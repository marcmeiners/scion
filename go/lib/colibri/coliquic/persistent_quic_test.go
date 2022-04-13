// Copyright 2022 ETH Zurich
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

package coliquic

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func TestInvariantColibriRepresentation(t *testing.T) {
	colPath := newTestColibriPath()
	copy(colPath.PacketTimestamp[:], []byte{0, 1, 2, 3, 4, 5, 6, 7})
	colPath.InfoField.OrigPayLen = 12345
	buff := make([]byte, colPath.Len())
	err := colPath.SerializeTo(buff)
	require.NoError(t, err)

	// representation of the original
	repr1 := invariantColibri(buff)

	// modify timestamp and orig payload length and obtain the representation again
	copy(colPath.PacketTimestamp[:], []byte{10, 11, 12, 13, 14, 15, 16, 17})
	colPath.InfoField.OrigPayLen = 11111
	err = colPath.SerializeTo(buff)
	require.NoError(t, err)
	repr2 := invariantColibri(buff)

	require.Equal(t, repr1, repr2)
}

// TestListenerManySessions is a multi part test that checks the listener for proper behavior.
// - part 1 tests listening without any sessions yet.
// - part 2 reuses the previous session
// - part 3 opens a new session
// - part 4 closes first session and opens a new stream w/ second session
// - part 5 closes second and only remaining session, opens a new one
// - part 6: closes listener and accept should return an error
func TestListenerManySessions(t *testing.T) {
	wgServer := sync.WaitGroup{}
	thisNet := newMockNetwork(t)
	serverAddr := mockScionAddress(t, "1-ff00:0:110", "127.0.0.1:10001")
	wgServer.Add(1)
	messagesReceivedAtServer := make(chan string)
	go func() {
		defer wgServer.Done()
		pconn := newConnMock(t, serverAddr, thisNet)
		serverTlsConfig := &tls.Config{
			Certificates: []tls.Certificate{*createTestCertificate(t)},
			NextProtos:   []string{"coliquictest"},
		}
		listener := NewListener(pconn, serverTlsConfig, nil)
		// keep track of sessions and streams
		sessions := make(map[quic.Session]struct{})
		streams := make(map[quic.Stream]struct{})
		expectedSessionsPerPart := []int{1, 1, 2, 2, 3}
		// parts 1-5
		for i, expectedSessions := range expectedSessionsPerPart {
			conn, err := listener.Accept()
			require.NoError(t, err)
			buff2, err := io.ReadAll(conn)
			require.NoError(t, err)
			msg := string(buff2)
			c := conn.(*streamAsConn)
			sessions[c.session] = struct{}{}
			streams[c.stream] = struct{}{}
			require.Len(t, sessions, expectedSessions, "wrong number of sessions")
			require.Len(t, streams, i+1)
			messagesReceivedAtServer <- msg
		}
		// part 6
		err := listener.Close()
		require.NoError(t, err)
		conn, err := listener.Accept()
		require.Nil(t, conn)
		require.Error(t, err)
	}()
	// client
	ctx, cancelF := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelF()
	clientAddr := mockScionAddress(t, "1-ff00:0:111", "127.0.0.1:1234")
	pconn := newConnMock(t, clientAddr, thisNet)
	clientTlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"coliquictest"},
	}
	// useful func to read or timeout
	messageFromServer := func() string {
		select {
		case <-ctx.Done():
			return "context deadline exceeded: no message from server"
		case msg := <-messagesReceivedAtServer:
			return msg
		}
	}
	// part 1
	t.Log("-------------------- part 1")
	sess1, err := quic.DialContext(ctx, pconn, serverAddr, "serverName", clientTlsConfig, nil)
	require.NoError(t, err)
	stream, err := sess1.OpenStream()
	require.NoError(t, err)
	msg := "hello server 1"
	_, err = stream.Write([]byte(msg))
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)
	require.Equal(t, msg, messageFromServer())
	// part 2
	t.Log("-------------------- part 2")
	stream, err = sess1.OpenStream()
	require.NoError(t, err)
	msg = "hello server 2"
	_, err = stream.Write([]byte(msg))
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)
	require.Equal(t, msg, messageFromServer())
	// part 3
	t.Log("-------------------- part 3")
	sess2, err := quic.DialContext(ctx, pconn, serverAddr, "serverName", clientTlsConfig, nil)
	require.NoError(t, err)
	stream, err = sess2.OpenStream()
	require.NoError(t, err)
	msg = "hello server 3"
	_, err = stream.Write([]byte(msg))
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)
	require.Equal(t, msg, messageFromServer())
	// part 4
	t.Log("-------------------- part 4")
	err = sess1.CloseWithError(quic.ApplicationErrorCode(0), "")
	require.NoError(t, err)
	stream, err = sess2.OpenStream()
	require.NoError(t, err)
	msg = "hello server 4"
	_, err = stream.Write([]byte(msg))
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)
	require.Equal(t, msg, messageFromServer())
	// part 5
	t.Log("-------------------- part 5")
	err = sess2.CloseWithError(quic.ApplicationErrorCode(0), "")
	require.NoError(t, err)
	sess3, err := quic.DialContext(ctx, pconn, serverAddr, "serverName", clientTlsConfig, nil)
	require.NoError(t, err)
	stream, err = sess3.OpenStream()
	require.NoError(t, err)
	msg = "hello server 5"
	_, err = stream.Write([]byte(msg))
	require.NoError(t, err)
	err = stream.Close()
	require.NoError(t, err)
	require.Equal(t, msg, messageFromServer())
	// part 6 is server only
	// wait for server to shutdown
	require.NoError(t, waitWithContext(ctx, &wgServer))
}

// TestSingleSession checks that only one session is created per path.
// Mimic the tiny topology, and attempt to connect from 111 to 110 and 112, using
// both regular SCION and COLIBRI paths.
// This test is a multipart one:
// - part 1:
func TestReuseSession(t *testing.T) {
	ctx, cancelF := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelF()
	thisNet := newMockNetwork(t)
	clientTlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"coliquictest"},
	}

	dialer := NewPersistentQUIC(
		newConnMock(t, mockScionAddress(t, "1-ff00:0:111", "127.0.0.1:22345"), thisNet),
		clientTlsConfig, nil)
	require.Len(t, dialer.sessions, 0)

	messages := make(chan string)
	runPersistentServer := func(serverAddr net.Addr, msg string) {
		// health of the test itself: no previous path should be pressent in the dialer
		repr, err := addrToString(serverAddr)
		require.NoError(t, err, "problem within test, could not represent addr/path")
		_, ok := dialer.sessions[repr]
		require.False(t, ok, "problem within test, addr/path already present (should not call"+
			"twice runPersistentServer for the same addr/path)")
		go runListener(t, thisNet, serverAddr, messages, msg)
	}

	clientWg := sync.WaitGroup{}
	runClient := func(serverAddr net.Addr, sessions int, msg string) {
		clientWg.Add(1)
		// run the client
		go func() {
			defer clientWg.Done()
			conn, err := dialer.Dial(ctx, serverAddr)
			require.NoError(t, err, "failed for: %s", msg)
			require.Len(t, dialer.sessions, sessions, "failed for: %s", msg)
			n, err := io.WriteString(conn, msg)
			require.NoError(t, err, "failed for: %s", msg)
			require.Greater(t, n, 0, "failed for: %s", msg)
			err = conn.Close()
			require.NoError(t, err, "failed for: %s", msg)
			select {
			case <-ctx.Done():
				require.FailNow(t, "timeout", "for msg %s", msg)
			case <-messages:
			}
		}()
	}

	// to 110 with scion
	t.Log("to 110 with scion")
	dst := mockScionAddressWithPath(t, "1-ff00:0:110", "127.0.0.1:20001",
		"1-ff00:0:111", 41, 1, "1-ff00:0:110")
	runPersistentServer(dst, "server 110")
	runClient(dst, 1, "hello 110")
	require.NoError(t, waitWithContext(ctx, &clientWg))

	// to 112 with scion
	t.Log("to 112 with scion")
	dst = mockScionAddressWithPath(t, "1-ff00:0:112", "127.0.0.1:20002",
		"1-ff00:0:111", 41, 1, "1-ff00:0:110", 2, 1, "1-ff00:0:112")
	runPersistentServer(dst, "server 112")
	runClient(dst, 2, "hello 112")
	require.NoError(t, waitWithContext(ctx, &clientWg))

	// to 110 again with several connections
	t.Log("to 110 again with several connections")
	dst = mockScionAddressWithPath(t, "1-ff00:0:110", "127.0.0.1:20003",
		"1-ff00:0:111", 41, 1, "1-ff00:0:110")
	for i := 0; i < 50; i++ {
		runClient(dst, 2, fmt.Sprintf("hello 110 again %d", i))
	}
	require.NoError(t, waitWithContext(ctx, &clientWg))
}

// TestTooManyStreams checks that the persistent quic can connect to the destination even
// in the case when too many streams have been created for a stream.
func TestTooManyStreams(t *testing.T) {
	thisNet := newMockNetwork(t)
	serverAddr := mockScionAddressWithPath(t, "1-ff00:0:110", "127.0.0.1:30001",
		"1-ff00:0:111", 41, 1, "1-ff00:0:110")
	messages := make(chan string)
	go runListener(t, thisNet, serverAddr, messages, "theserver")

	clientTlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"coliquictest"},
	}
	dialer := NewPersistentQUIC(
		newConnMock(t, mockScionAddress(t, "1-ff00:0:111", "127.0.0.1:32345"), thisNet),
		clientTlsConfig, nil)

	ctx, cancelF := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelF()

	conns := make([]net.Conn, 0)
	connsM := sync.Mutex{}
	wgClients := sync.WaitGroup{}
	runClient := func(id int) {
		wgClients.Add(1)
		go func() {
			defer wgClients.Done()
			conn, err := dialer.Dial(ctx, serverAddr)
			require.NoError(t, err)
			_, err = io.WriteString(conn, "message from client")
			require.NoError(t, err)

			connsM.Lock()
			defer connsM.Unlock()
			conns = append(conns, conn)
			// do not close the stream
		}()
	}
	N := 5000 // 5000 simultaneous streams to the same destination
	for i := 0; i < N; i++ {
		runClient(i)
	}
	require.NoError(t, waitWithContext(ctx, &wgClients))
	require.Len(t, dialer.sessions, 1)
	require.Len(t, conns, N)
	// close all connections
	for i, c := range conns {
		err := c.Close()
		require.NoError(t, err, "closing connection number %d", i)
	}
}

func TestCloseSession(t *testing.T) {

}

func waitWithContext(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()
	select {
	case <-ctx.Done():
		return fmt.Errorf("context deadline exceeded, server not done")
	case <-done:
	}
	return nil
}

func runListener(t *testing.T, theNet *mockNetwork, serverAddr net.Addr,
	messages chan string, serverId string) {

	serverTlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*createTestCertificate(t)},
		NextProtos:   []string{"coliquictest"},
	}
	serverQuicConfig := &quic.Config{KeepAlive: true}
	listener := NewListener(newConnMock(t, serverAddr, theNet),
		serverTlsConfig, serverQuicConfig)
	for {
		conn, err := listener.Accept()
		require.NoError(t, err, "failed for: %s", serverId)
		buff, err := io.ReadAll(conn)
		require.NoError(t, err, "failed for: %s", serverId)
		msg := string(buff)
		messages <- msg
		err = conn.Close()
		require.NoError(t, err)
	}

}
