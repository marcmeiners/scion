// Copyright 2021 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package coliquic implements QUIC on top of COLIBRI.
// Inspired on squic.
// Test with go test ./go/lib/colibri/coliquic/ -count=1
package coliquic

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/lucas-clemente/quic-go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"

	"github.com/scionproto/scion/go/co/reservation/test"
	"github.com/scionproto/scion/go/lib/common"
	"github.com/scionproto/scion/go/lib/slayers/path/colibri"
	"github.com/scionproto/scion/go/lib/snet"
	"github.com/scionproto/scion/go/lib/snet/mock_snet"
	"github.com/scionproto/scion/go/lib/snet/path"
	"github.com/scionproto/scion/go/lib/snet/squic"
	"github.com/scionproto/scion/go/lib/topology"
	"github.com/scionproto/scion/go/lib/xtest"
	sgrpc "github.com/scionproto/scion/go/pkg/grpc"
	"github.com/scionproto/scion/go/pkg/grpc/mock_grpc"
	colpb "github.com/scionproto/scion/go/pkg/proto/colibri"
	mock_col "github.com/scionproto/scion/go/pkg/proto/colibri/mock_colibri"
)

// TestColibriQuic creates a server and a client, both with SCION-COLIBRI addresses and paths,
// and communicates both via a quic connection.
// The test also checks the ability of QUIC to receive packets destined to a different address
// that the one it is listening to (e.g. our BR forwards colibri packets with C=1 to the
// local colibri service instead of the destination). This part of the test is left here for
// future reference, although it doesn't test anything from our codebase.
func TestColibriQuic(t *testing.T) {
	// TODO(juagargi) remove this test as it's superseded by TestQUICMultipleConnections
	testCases := map[string]struct {
		// XXX(juagargi) port numbers must be different on each test
		clientAddr net.Addr // packets sent from here
		dstAddr    net.Addr // packets originally sent to here
		rcvAddr    net.Addr // packets end up here. If empty, dstAddr will be used
	}{
		"scion_no_routing": {
			clientAddr: mockScionAddress(t, "1-ff00:0:111", "127.0.0.1:12345"),
			dstAddr:    mockScionAddress(t, "1-ff00:0:112", "127.0.0.1:43211"),
		},
		"scion_one_transit": {
			clientAddr: mockScionAddress(t, "1-ff00:0:111", "127.0.0.1:12346"),
			dstAddr:    mockScionAddress(t, "1-ff00:0:112", "127.0.0.1:43212"),
			rcvAddr:    mockScionAddress(t, "1-ff00:0:110", "127.0.0.2:43212"),
		},
		"colibri_no_routing": {
			clientAddr: mockColibriAddress(t, "1-ff00:0:111", "127.0.0.1:12347"),
			dstAddr:    mockScionAddress(t, "1-ff00:0:112", "127.0.0.1:43213"),
		},
		"colibri_one_transit": {
			clientAddr: mockColibriAddress(t, "1-ff00:0:111", "127.0.0.1:12348"),
			dstAddr:    mockScionAddress(t, "1-ff00:0:112", "127.0.0.1:43214"),
			rcvAddr:    mockScionAddress(t, "1-ff00:0:110", "127.0.0.2:43214"),
		},
	}
	for name, tc := range testCases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel() // we are not really using sockets -> no bind clashes
			if tc.rcvAddr == nil {
				tc.rcvAddr = tc.dstAddr
			}
			thisNet := newMockNetwork(t, tc.dstAddr, tc.rcvAddr)
			// server:
			serverTlsConfig := &tls.Config{
				Certificates: []tls.Certificate{*createTestCertificate(t)},
				NextProtos:   []string{"coliquictest"},
			}
			serverQuicConfig := &quic.Config{KeepAlive: true}
			listener, err := quic.Listen(newConnMock(t, tc.rcvAddr, thisNet),
				serverTlsConfig, serverQuicConfig)
			require.NoError(t, err)
			defer func() {
				err := listener.Close()
				require.NoError(t, err)
			}()

			done := make(chan struct{})
			ctx, cancelF := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelF()
			go func(ctx context.Context, listener quic.Listener) {
				session, err := listener.Accept(ctx)
				require.NoError(t, err)

				// check the local address
				require.Equal(t, tc.rcvAddr.String(), session.LocalAddr().String())

				// check if the path used is colibri
				colPath, err := GetColibriPath(session)
				require.NoError(t, err)
				clientPath := tc.clientAddr.(*snet.UDPAddr).Path
				if _, ok := clientPath.(path.Colibri); ok {
					// if it is colibri, check it is the same as the one used originally
					// at the source by comparing their bytes
					buff := make([]byte, colPath.Len())
					err = colPath.SerializeTo(buff)
					require.NoError(t, err)
					require.Equal(t, clientPath.(path.Colibri).Raw, buff)
				} else {
					require.Nil(t, colPath)
				}

				stream, err := session.AcceptStream(ctx)
				require.NoError(t, err)
				buff := make([]byte, 16384)
				n, err := stream.Read(buff)
				require.NoError(t, err)
				require.Equal(t, "hello world", string(buff[:n]))
				err = stream.Close()
				require.NoError(t, err)
				err = session.CloseWithError(0, "")
				require.NoError(t, err)
				done <- struct{}{}
			}(ctx, listener)

			// client:
			clientTlsConfig := &tls.Config{
				InsecureSkipVerify: true,
				NextProtos:         []string{"coliquictest"},
			}
			clientQuicConfig := &quic.Config{KeepAlive: true}

			ctx2, cancelF2 := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelF2()
			session, err := quic.DialContext(ctx2, newConnMock(t, tc.clientAddr, thisNet),
				tc.dstAddr, "serverName", clientTlsConfig, clientQuicConfig)
			require.NoError(t, err)
			stream, err := session.OpenStream()
			require.NoError(t, err)
			n, err := stream.Write([]byte("hello world"))
			require.NoError(t, err)
			require.Equal(t, len("hello wold")+1, n)

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				require.FailNow(t, "timed out")
			}
			err = stream.Close()
			require.NoError(t, err)
			err = session.CloseWithError(0, "")
			require.NoError(t, err)
		})
	}
}

// TestQUICMultipleConnections tests that a single QUIC listener is able to receive packets
// destined to it, as well as destined to another host but captured by the network.
// This test is useful to asses that our colibri service will be able to serve requests
// when it is the destination AS, as well as when it is a transit AS.
// The listener will receive messages via COLIBRI paths and regular SCION.
func TestQUICMultipleConnections(t *testing.T) {
	// XXX(juagargi) use different addresses for each test case
	testCases := map[string]struct {
		clientAddr net.Addr
		serverAddr net.Addr   // where the service is listening
		messages   []net.Addr // destination addresses of each request/message
	}{
		"dest_two_msgs": {
			clientAddr: mockColibriAddress(t, "1-ff00:0:111", "127.0.0.1:11111"),
			serverAddr: mockScionAddress(t, "1-ff00:0:110", "127.0.0.1:31010"),
			messages: []net.Addr{
				// 1: same as server
				mockScionAddress(t, "1-ff00:0:110", "127.0.0.1:31010"),
				// 2: same as server
				mockScionAddress(t, "1-ff00:0:110", "127.0.0.1:31010"),
			},
		},
		"transit_two_msgs": {
			clientAddr: mockColibriAddress(t, "1-ff00:0:111", "127.0.0.1:11112"),
			serverAddr: mockScionAddress(t, "1-ff00:0:110", "127.0.0.1:31011"),
			messages: []net.Addr{
				// 1: destination COL SRV at 112
				mockScionAddress(t, "1-ff00:0:112", "127.0.0.2:31011"),
				// 2: destination COL SRV at 112
				mockScionAddress(t, "1-ff00:0:112", "127.0.0.2:31011"),
			},
		},
		"mix_three_msgs": {
			clientAddr: mockColibriAddress(t, "1-ff00:0:111", "127.0.0.1:11113"),
			serverAddr: mockScionAddress(t, "1-ff00:0:110", "127.0.0.1:31012"),
			messages: []net.Addr{
				// 1: destination COL SRV at 112
				mockScionAddress(t, "1-ff00:0:112", "127.0.0.2:31012"),
				// 2: same as server
				mockScionAddress(t, "1-ff00:0:110", "127.0.0.1:31012"),
				// 3: destination COL SRV at 112
				mockScionAddress(t, "1-ff00:0:112", "127.0.0.2:31012"),
			},
		},
	}
	for name, tc := range testCases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// prepare the routing for the mock network: all intended destinataries go to server
			fwdEntries := make([]net.Addr, 0)
			for _, dstAddr := range tc.messages {
				fwdEntries = append(fwdEntries, dstAddr, tc.serverAddr)
			}
			thisNet := newMockNetwork(t, fwdEntries...)
			// server:
			serverTlsConfig := &tls.Config{
				Certificates: []tls.Certificate{*createTestCertificate(t)},
				NextProtos:   []string{"coliquictest"},
			}
			serverQuicConfig := &quic.Config{KeepAlive: true}
			listener, err := quic.Listen(newConnMock(t, tc.serverAddr, thisNet),
				serverTlsConfig, serverQuicConfig)
			require.NoError(t, err)

			receivedMsgs := make(chan string, 1024)
			go func(listener quic.Listener) {
				ctx, cancelF := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancelF()
				for i := 0; i < len(tc.messages); i++ {
					session, err := listener.Accept(ctx)
					require.NoError(t, err)
					stream, err := session.AcceptStream(ctx)
					require.NoError(t, err)
					var buff [16384]byte
					n, err := stream.Read(buff[:])
					require.NoError(t, err)
					msg := string(buff[:n])
					err = stream.Close()
					require.NoError(t, err)
					receivedMsgs <- msg
				}
			}(listener)

			// client:
			conn := newConnMock(t, tc.clientAddr, thisNet)
			clientTlsConfig := &tls.Config{
				InsecureSkipVerify: true,
				NextProtos:         []string{"coliquictest"},
			}
			clientQuicConfig := &quic.Config{KeepAlive: true}
			ctx, cancelF := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelF()
			for i, dstAddr := range tc.messages {
				session, err := quic.DialContext(ctx, conn, dstAddr, "serverName", clientTlsConfig, clientQuicConfig)
				require.NoError(t, err)
				stream, err := session.OpenStream()
				require.NoError(t, err)
				msg := fmt.Sprintf("hello world %d", i)
				_, err = stream.Write([]byte(msg))
				require.NoError(t, err)
				select {
				case msgAtServer := <-receivedMsgs:
					require.Equal(t, msg, msgAtServer)
				case <-time.After(time.Second):
					require.Fail(t, "time out waiting for message received at server")
				}
				err = stream.Close()
				require.NoError(t, err)
			}
		})
	}
}

func TestColibriGRPC(t *testing.T) {
	thisNet := newMockNetwork(t)

	// server: (don't reuse addresses on any test, as quic caches the connections)
	serverAddr := mockScionAddress(t, "1-ff00:0:111", "127.0.0.1:23211")
	serverTlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*createTestCertificate(t)},
		NextProtos:   []string{"coliquicgrpc"},
	}
	serverQuicConfig := &quic.Config{KeepAlive: true}

	quicLis, err := quic.Listen(newConnMock(t, serverAddr, thisNet),
		serverTlsConfig, serverQuicConfig)
	require.NoError(t, err)

	listener := NewConnListener(quicLis)
	require.NoError(t, err)

	// mock a method (see net_test) and check we recover the colibri path correctly
	mctrl := gomock.NewController(t)
	defer mctrl.Finish()
	handler := mock_col.NewMockColibriServiceServer(mctrl)
	// use SetupSegment to check that the client talks to the server as expected,
	// and that the server is able to extract the address and path to the client.
	handler.EXPECT().SegmentSetup(gomock.Any(), gomock.Any()).Times(1).DoAndReturn(
		func(ctx context.Context, _ *colpb.SegmentSetupRequest) (
			*colpb.SegmentSetupResponse, error) {

			p, ok := peer.FromContext(ctx)
			require.True(t, ok)
			require.NotNil(t, p)
			require.IsType(t, &snet.UDPAddr{}, p.Addr)
			require.IsType(t, path.Colibri{}, p.Addr.(*snet.UDPAddr).Path)
			ok, usage, err := UsageFromContext(ctx)
			require.NoError(t, err)
			require.True(t, ok)
			require.Greater(t, usage, uint64(0))
			return &colpb.SegmentSetupResponse{SuccessFailure: &colpb.SegmentSetupResponse_Token{
				Token: p.Addr.(*snet.UDPAddr).Path.(path.Colibri).Raw,
			}}, nil
		})

	var testInterceptorCalled bool
	testInterceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {
		res, err := handler(ctx, req)
		testInterceptorCalled = true
		return res, err
	}

	gRPCServer := NewGrpcServer(grpc.UnaryInterceptor(testInterceptor),
		sgrpc.UnaryServerInterceptor())
	colpb.RegisterColibriServiceServer(gRPCServer, handler)

	done := make(chan struct{})
	go func() {
		err = gRPCServer.Serve(listener)
		require.NoError(t, err)
		done <- struct{}{}
	}()

	// client:
	clientAddr := mockColibriAddress(t, "1-ff00:0:112", "127.0.0.1:2346")
	clientTlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"coliquicgrpc"},
	}
	clientQuicConfig := &quic.Config{KeepAlive: true}

	ctx, cancelF := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelF()

	connDial := squic.ConnDialer{
		Conn:       newConnMock(t, clientAddr, thisNet),
		TLSConfig:  clientTlsConfig,
		QUICConfig: clientQuicConfig,
	}
	quicConn, err := connDial.Dial(ctx, serverAddr)
	require.NoError(t, err)
	dialer := func(context.Context, string) (net.Conn, error) {
		return quicConn, nil
	}
	conn, err := grpc.DialContext(ctx, serverAddr.String(), grpc.WithInsecure(),
		grpc.WithContextDialer(dialer))
	require.NoError(t, err)
	gRPCClient := colpb.NewColibriServiceClient(conn)
	res, err := gRPCClient.SegmentSetup(ctx, &colpb.SegmentSetupRequest{})
	require.NoError(t, err)
	require.Equal(t, clientAddr.(*snet.UDPAddr).Path.(path.Colibri).Raw, res.GetToken())
	require.True(t, testInterceptorCalled)

	gRPCServer.GracefulStop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out")
	}
}

// TestClient ensures that the client works as expected.
// Namely, it tests that there is only one quic session per neighbor opened at all times,
// even when dialing a destination that is not a direct neighbor.
// e.g. with the tiny topology, when located at ff00:0:111, and
// dialing ff00:0:112 there should only be one session opened to ff00:0:110 .
func TestClient(t *testing.T) {
	t.Skip("deleteme enable this test")
	ctx, cancelF := context.WithTimeout(context.Background(), time.Second)
	defer cancelF()
	// the path to use for all tests:
	path111to112 := test.NewPath(0, "1-ff00:0:111", 10, 1, "1-ff00:0:110", 2, 20, "1-ff00:0:112", 0)
	// mock topology loader and router
	topo := MockTopoLoader{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	router := mock_snet.NewMockRouter(ctrl)
	router.EXPECT().Route(gomock.Any(), gomock.Any()).AnyTimes().
		Return(test.NewSnetPath("1-ff00:0:111", 10, 1, "1-ff00:0:110"), nil)
	// because we will prefill the neighbor entries, the connection is only used
	// for GRPC to actually perform the RPC via NewColibriServiceClient
	serverAddr := &snet.UDPAddr{
		IA:      xtest.MustParseIA("1-ff00:0:110"),
		Host:    xtest.MustParseUDPAddr(t, "127.0.0.1:12345"),
		NextHop: xtest.MustParseUDPAddr(t, "127.0.0.1:30200"), // address of BR, never used
	}
	thisNet := newMockNetwork(t)
	pconn := newConnMock(t, serverAddr, thisNet)
	conn := mock_grpc.NewMockDialer(ctrl)
	conn.EXPECT().Dial(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, addr net.Addr) (*grpc.ClientConn, error) {
			require.IsType(t, &snet.UDPAddr{}, addr)
			require.Equal(t, serverAddr, addr)

			// client:
			clientAddr := mockColibriAddress(t, "1-ff00:0:111", "127.0.0.1:12346")
			clientTlsConfig := &tls.Config{
				InsecureSkipVerify: true,
				NextProtos:         []string{"coliquicgrpc"},
			}
			clientQuicConfig := &quic.Config{KeepAlive: true}
			connDial := squic.ConnDialer{
				Conn:       newConnMock(t, clientAddr, thisNet),
				TLSConfig:  clientTlsConfig,
				QUICConfig: clientQuicConfig,
			}
			quicConn, err := connDial.Dial(ctx, serverAddr)
			require.NoError(t, err)

			dialer := func(context.Context, string) (net.Conn, error) {
				return quicConn, nil
			}
			// var deleteme *grpc.ClientConn
			// deleteme.
			return grpc.DialContext(ctx, addr.String(), grpc.WithInsecure(),
				grpc.WithContextDialer(dialer))
		})
	_ = conn

	oprtr, err := NewServiceClientOperator(topo, pconn, router, nil)
	require.NoError(t, err)
	// time.Sleep(3 * time.Second)
	oprtr.neighborsMutex.Lock()
	oprtr.neighbors[10] = serverAddr
	oprtr.neighborsMutex.Unlock()

	// RPC to direct neighbor 110
	path111to112.CurrentStep = 0
	client1, err := oprtr.ColibriClient(ctx, path111to112)
	require.NoError(t, err)
	// for {
	// 	select {

	// 	}
	// }
	_, err = client1.SegmentSetup(ctx, &colpb.SegmentSetupRequest{})
	require.NoError(t, err)

	// RPC to indirect neighbor 112
	path111to112.CurrentStep = 1
	client2, err := oprtr.ColibriClient(ctx, path111to112)
	require.NoError(t, err)
	_, err = client2.SegmentSetup(ctx, &colpb.SegmentSetupRequest{})
	require.NoError(t, err)

	// check again to direct neighbor
	_, err = client1.SegmentSetup(ctx, &colpb.SegmentSetupRequest{})
	require.NoError(t, err)
}

type MockTopoLoader struct{}

func (MockTopoLoader) InterfaceIDs() []uint16 {
	// return []uint16{10}
	return nil
}

func (topo MockTopoLoader) InterfaceInfoMap() map[common.IFIDType]topology.IFInfo {
	return nil
	// return map[common.IFIDType]topology.IFInfo{
	// 	1: {
	// 		IA: xtest.MustParseIA("1-ff00:0:110"),
	// 	},
	// }
}

// mockNetwork is used to simulate a network, where packets are sent and read.
// The routing field is used to determine to which address will be the packet sent,
// if an enty is present. E.g. if routing["a"]=="b", a packet with destination "a" will be
// sent to "b". If no entry is present, the destination address of the packet is used.
// The channels field organizes packets per receiver address (as string).
type mockNetwork struct {
	debugMessagesEnabled bool
	routing              map[string]string
	channels             map[string]chan packet
	m                    sync.Mutex
}

// packet is a packet received by a mockNetwork.
type packet struct {
	sender net.Addr
	data   []byte
}

func newMockNetwork(t *testing.T, redirPairs ...net.Addr) *mockNetwork {
	if len(redirPairs)%2 != 0 {
		require.Fail(t, "redir pairs should have an even number of elements")
	}
	n := &mockNetwork{
		routing:  make(map[string]string, len(redirPairs)/2),
		channels: make(map[string]chan packet),
	}
	for i := 0; i < len(redirPairs); i += 2 {
		orig := redirPairs[i].String()
		dst := redirPairs[i+1].String()
		n.routing[orig] = dst
	}
	return n
}

func (n *mockNetwork) EnableDebugMessages(enable bool) *mockNetwork {
	n.debugMessagesEnabled = enable
	return n
}

// ReadFrom returns the data from the first packet for receiver, and its sender.
func (n *mockNetwork) ReadFrom(receiver net.Addr) ([]byte, net.Addr) {
	key := receiver.String()
	n.ensureChannel(key)
	pac := <-n.channels[key]
	if n.debugMessagesEnabled {
		fmt.Printf("[mocknet] ReadFrom (%s -> %s) = %d bytes\n", pac.sender, receiver, len(pac.data))
	}
	return pac.data, pac.sender
}

// WriteTo writes a packet from sender to receiver, with data.
func (n *mockNetwork) WriteTo(sender, receiver net.Addr, data []byte) {
	pac := packet{sender: sender, data: data}
	orig := receiver.String()
	n.ensureChannel(orig)
	dst := n.routing[orig]
	n.channels[dst] <- pac
	if n.debugMessagesEnabled {
		fmt.Printf("[mocknet] WriteTo  (%s -> %s) = %d bytes\n", sender, receiver, len(pac.data))
	}
}

func (n *mockNetwork) ensureChannel(key string) {
	n.m.Lock()
	defer n.m.Unlock()
	if _, found := n.channels[key]; !found {
		n.channels[key] = make(chan packet, 1024) // buffer size big enough to never block writers
		if _, found = n.routing[key]; !found {
			n.routing[key] = key
		}
	}
}

// mockScionAddress returns a SCION address with a SCION type path.
func mockScionAddress(t *testing.T, ia, host string) net.Addr {
	t.Helper()
	return &snet.UDPAddr{
		IA:   xtest.MustParseIA(ia),
		Host: xtest.MustParseUDPAddr(t, host),
		Path: path.SCION{
			Raw: xtest.MustParseHexString("0000208000000111000001000100022200000100003f0001" +
				"0000010203040506003f00030002010203040506003f00000002010203040506003f000100000" +
				"10203040506"),
		},
	}
}

// mockColibriAddress returns a SCION address with a Colibri path.
func mockColibriAddress(t *testing.T, ia, host string) net.Addr {
	t.Helper()
	p := newTestColibriPath()
	buffLen := 8 + 24 + (len(p.HopFields) * 8) // timestamp + infofield + 3*hops
	buff := make([]byte, buffLen)
	err := p.SerializeTo(buff)
	require.NoError(t, err)

	return &snet.UDPAddr{
		IA:   xtest.MustParseIA(ia),
		Host: xtest.MustParseUDPAddr(t, host),
		Path: path.Colibri{
			Raw: buff,
		},
	}
}

func mockScionAddressWithPath(t *testing.T, ia, host string, path ...interface{}) net.Addr {
	scionPath := test.NewSnetPath(path...)
	addr := mockScionAddress(t, ia, host)
	addr.(*snet.UDPAddr).Path = scionPath.Dataplane()
	return addr
}

func newTestColibriPath() *colibri.ColibriPath {
	return &colibri.ColibriPath{
		PacketTimestamp: colibri.Timestamp{1},
		InfoField: &colibri.InfoField{
			C:           true,
			R:           false,
			S:           true,
			Ver:         1,
			CurrHF:      0,
			HFCount:     3,
			ResIdSuffix: xtest.MustParseHexString("beefcafe0000000000000000"),
			ExpTick:     1893452400, // valid until 1.1.2030
			BwCls:       7,
			Rlc:         7,
			OrigPayLen:  1208,
		},
		HopFields: []*colibri.HopField{
			{
				IngressId: 0,
				EgressId:  41,
				Mac:       []byte{140, 95, 102, 190}, // MAC is 4 bytes
			},
			{
				IngressId: 1,
				EgressId:  2,
				Mac:       []byte{0, 61, 66, 164},
			},
			{
				IngressId: 1,
				EgressId:  0,
				Mac:       xtest.MustParseHexString("00000000"),
			},
		},
	}
}

// connMock uses a mockNetwork to simulate a proper net.PacketConn.
type connMock struct {
	localAddr net.Addr
	net       *mockNetwork
}

var _ net.PacketConn = (*connMock)(nil)

func newConnMock(t *testing.T, localAddr net.Addr, network *mockNetwork) *connMock {
	t.Helper()
	require.NotNil(t, network)
	return &connMock{
		localAddr: localAddr,
		net:       network,
	}
}

func (c *connMock) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *connMock) Close() error {
	return nil
}

func (c *connMock) ReadFrom(p []byte) (int, net.Addr, error) {
	b, sender := c.net.ReadFrom(c.localAddr)
	n := copy(p, b)
	return n, sender, nil
}

func (c *connMock) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.net.WriteTo(c.localAddr, addr, p)
	return len(p), nil
}

func (c *connMock) SetDeadline(t time.Time) error {
	return nil
}

func (c *connMock) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *connMock) SetWriteDeadline(t time.Time) error {
	return nil
}

// createTestCertificate is based on https://github.com/lucas-clemente/quic-go/blob/
// e098ccd2b3bf560d3d8056dccc1a35b229a2a47a/example/echo/echo.go#L92
func createTestCertificate(t *testing.T) *tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return &tlsCert
}
