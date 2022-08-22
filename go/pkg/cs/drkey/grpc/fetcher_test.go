// Copyright 2020 ETH Zurich
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

package grpc_test

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/test/bufconn"

	"github.com/scionproto/scion/go/lib/drkey"
	"github.com/scionproto/scion/go/lib/scrypto/cppki"
	"github.com/scionproto/scion/go/lib/snet"
	"github.com/scionproto/scion/go/lib/snet/mock_snet"
	"github.com/scionproto/scion/go/lib/xtest"
	dk_grpc "github.com/scionproto/scion/go/pkg/cs/drkey/grpc"
	"github.com/scionproto/scion/go/pkg/cs/drkey/mock_drkey"
	"github.com/scionproto/scion/go/pkg/grpc/mock_grpc"
	cppb "github.com/scionproto/scion/go/pkg/proto/control_plane"
	"github.com/scionproto/scion/go/pkg/trust"
	"github.com/scionproto/scion/go/pkg/trust/mock_trust"
)

func dialer(creds credentials.TransportCredentials,
	drkeyServer cppb.DRKeyInterServiceServer) func(context.Context, string) (net.Conn, error) {
	bufsize := 1024 * 1024
	listener := bufconn.Listen(bufsize)

	server := grpc.NewServer(grpc.Creds(creds))

	cppb.RegisterDRKeyInterServiceServer(server, drkeyServer)

	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()

	return func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
}

func TestLvl1KeyFetching(t *testing.T) {

	if *updateNonDeterministic {
		t.Skip("test crypto is being updated")
	}

	trc := xtest.LoadTRC(t, "testdata/common/trcs/ISD1-B1-S1.trc")
	crt111File := "testdata/common/ISD1/ASff00_0_111/crypto/as/ISD1-ASff00_0_111.pem"
	key111File := "testdata/common/ISD1/ASff00_0_111/crypto/as/cp-as.key"
	tlsCert, err := tls.LoadX509KeyPair(crt111File, key111File)
	require.NoError(t, err)
	chain, err := cppki.ReadPEMCerts(crt111File)
	_ = chain
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lvl1db := mock_drkey.NewMockServiceEngine(ctrl)
	lvl1db.EXPECT().DeriveLvl1(gomock.Any()).Return(drkey.Lvl1Key{}, nil)

	mgrdb := mock_trust.NewMockDB(ctrl)
	mgrdb.EXPECT().SignedTRC(gomock.Any(), gomock.Any()).AnyTimes().Return(trc, nil)
	loader := mock_trust.NewMockX509KeyPairLoader(ctrl)
	loader.EXPECT().LoadX509KeyPair(gomock.Any(), gomock.Any()).AnyTimes().Return(&tlsCert, nil)
	mgr := trust.NewTLSCryptoManager(loader, mgrdb)

	drkeyServ := &dk_grpc.Server{
		Engine: lvl1db,
	}

	serverConf := &tls.Config{
		InsecureSkipVerify:    true,
		GetCertificate:        mgr.GetCertificate,
		VerifyPeerCertificate: mgr.VerifyClientCertificate,
		ClientAuth:            tls.RequireAnyClientCert,
	}
	serverCreds := credentials.NewTLS(serverConf)

	clientCreds := credentials.NewTLS(&tls.Config{
		InsecureSkipVerify:    true,
		GetClientCertificate:  mgr.GetClientCertificate,
		VerifyPeerCertificate: mgr.VerifyServerCertificate,
		VerifyConnection:      mgr.VerifyConnection,
	})

	conn, err := grpc.DialContext(context.Background(),
		"1-ff00:0:111,127.0.0.1:10000",
		grpc.WithTransportCredentials(clientCreds),
		grpc.WithContextDialer(dialer(serverCreds, drkeyServ)),
	)
	require.NoError(t, err)
	defer conn.Close()

	dialer := mock_grpc.NewMockDialer(ctrl)
	dialer.EXPECT().Dial(gomock.Any(), gomock.Any()).Return(conn, nil)

	path := mock_snet.NewMockPath(ctrl)
	path.EXPECT().Dataplane().Return(nil)
	path.EXPECT().UnderlayNextHop().Return(&net.UDPAddr{})
	router := mock_snet.NewMockRouter(ctrl)
	router.EXPECT().AllRoutes(gomock.Any(), gomock.Any()).Return([]snet.Path{path}, nil)

	fetcher := dk_grpc.Fetcher{
		Dialer:     dialer,
		Router:     router,
		MaxRetries: 10,
	}

	meta := drkey.Lvl1Meta{
		ProtoId:  drkey.Generic,
		Validity: time.Now(),
		SrcIA:    xtest.MustParseIA("1-ff00:0:111"),
	}
	_, err = fetcher.Lvl1(context.Background(), meta)
	require.NoError(t, err)
}
