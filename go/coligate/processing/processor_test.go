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

package processing_test

import (
	"encoding/binary"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/ipv4"
	"golang.org/x/sync/errgroup"

	proc "github.com/scionproto/scion/go/coligate/processing"
	"github.com/scionproto/scion/go/coligate/storage"
	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/slayers"
	colipath "github.com/scionproto/scion/go/lib/slayers/path/colibri"
)

func ErrGroupWait(e *errgroup.Group, duration time.Duration) error {
	c := make(chan struct{})
	var err error
	go func() {
		defer close(c)
		err = e.Wait()
	}()
	select {
	case <-c:
		return err
	case <-time.After(duration):
		return errors.New("Wait group did not finish in time")
	}
}

var coligateMetrics *proc.ColigateMetrics = proc.InitializeMetrics()

var reservationIdOne [12]byte = [12]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
var reservationIdLeftOne [12]byte = [12]byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

// Tests sequentially that write-read-repeat returns all reservations
// and that the channel is empty after the exit.
func TestCleanupRoutineSingleTaskSequentially(t *testing.T) {
	L := 10
	errGroup := &errgroup.Group{}
	c := &proc.Processor{}
	c.SetMetrics(coligateMetrics)
	cleanupChannel := c.CreateCleanupChannel(L)
	reservationDeletionChannels := c.CreateControlDeletionChannels(1, L)
	c.SetNumWorkers(1)
	now := time.Now()

	c.SetHasher([]byte("salt"))
	errGroup.Go(func() error {
		c.InitCleanupRoutine()
		return nil
	})

	for i := 0; i < L; i++ {
		newId := reservationIdLeftOne
		binary.BigEndian.PutUint32(newId[8:12], uint32(i))
		cleanupChannel <- &storage.UpdateTask{
			Reservation: &storage.Reservation{
				Id: newId,
			},
			HighestValidity: now.Add(1 * time.Millisecond),
		}
		select {
		case task := <-reservationDeletionChannels[0]:
			assert.NotNil(t, task)
			assert.IsType(t, &storage.DeletionTask{}, task)
			assert.Equal(t, newId, task.ResId)
		case <-time.After(1100 * time.Millisecond):
			assert.Fail(t, "reservation not deleted")
		}
	}

	c.Shutdown()
	assert.NoError(t, ErrGroupWait(errGroup, 1*time.Second))

	select {
	case task := <-reservationDeletionChannels[0]:
		assert.Nil(t, task)
	default:
	}
}

// Tests that the cleanup routine returns all reservations (in any order) once
// they are expired.
func TestCleanupRoutineBatchOfTasksSequentially(t *testing.T) {
	L := 10
	errGroup := &errgroup.Group{}
	c := &proc.Processor{}
	c.SetMetrics(coligateMetrics)
	cleanupChannel := c.CreateCleanupChannel(L)
	reservationDeletionChannels := c.CreateControlDeletionChannels(1, L)
	c.SetNumWorkers(1)

	c.SetHasher([]byte("salt"))
	errGroup.Go(func() error {
		c.InitCleanupRoutine()
		return nil
	})

	now := time.Now()
	for i := 0; i < L; i++ {
		newId := reservationIdLeftOne
		binary.BigEndian.PutUint32(newId[8:12], uint32(i))
		cleanupChannel <- &storage.UpdateTask{
			Reservation: &storage.Reservation{
				Id: newId,
			},
			HighestValidity: now.Add(1 * time.Millisecond),
		}
	}
	exit := false
	reportedReservations := 0
	for !exit {
		select {
		case <-reservationDeletionChannels[0]:
			reportedReservations++
		case <-time.After(1100 * time.Millisecond):
			exit = true
		}
	}
	assert.Equal(t, L, reportedReservations)
	c.Shutdown()
	assert.NoError(t, ErrGroupWait(errGroup, 1*time.Second))
}

// Tests that the validity of a currently stored reservation is
// extended if a new index arrives with longer validity
func TestCleanupRoutineSupersedeOld(t *testing.T) {
	L := 100
	errGroup := &errgroup.Group{}
	c := &proc.Processor{}
	c.SetMetrics(coligateMetrics)
	cleanupChannel := c.CreateCleanupChannel(L)
	reservationDeletionChannels := c.CreateControlDeletionChannels(1, L)
	c.SetNumWorkers(1)

	c.SetHasher([]byte("salt"))
	errGroup.Go(func() error {
		c.InitCleanupRoutine()
		return nil
	})
	now := time.Now()

	for i := 0; i < L; i++ {
		newId := reservationIdLeftOne
		binary.BigEndian.PutUint32(newId[8:12], uint32(i))
		cleanupChannel <- &storage.UpdateTask{
			Reservation: &storage.Reservation{
				Id: newId,
			},
			HighestValidity: now.Add(10 * time.Millisecond),
		}
	}

	for i := 0; i < L; i++ {
		newId := reservationIdLeftOne
		binary.BigEndian.PutUint32(newId[8:12], uint32(i))
		cleanupChannel <- &storage.UpdateTask{
			Reservation: &storage.Reservation{
				Id: newId,
			},
			HighestValidity: now.Add(20 * time.Millisecond),
		}
	}

	reportedReservations := 0
	exit := false
	for !exit {
		select {
		case task := <-reservationDeletionChannels[0]:
			assert.IsType(t, &storage.DeletionTask{}, task)
			reportedReservations++
		case <-time.After(1100 * time.Millisecond):
			exit = true
		}
	}
	assert.Equal(t, L, reportedReservations)
	c.Shutdown()

	assert.NoError(t, ErrGroupWait(errGroup, 1*time.Second))
}

func BenchmarkWorker(b *testing.B) {
	errGroup := &errgroup.Group{}
	c := &proc.Processor{}
	c.SetMetrics(coligateMetrics)
	updateChannels := c.CreateControlUpdateChannels(1, 1000)
	c.CreateControlDeletionChannels(1, 1)
	dataChannels := c.CreateDataChannels(1, 1000)

	borderRouterAddr, err := net.ResolveUDPAddr("udp", "localhost:30000")
	if err != nil {
		b.Error(err)
	}
	c.SetupPacketForwarder(errGroup, map[uint16]*net.UDPAddr{
		2: borderRouterAddr,
	}, coligateMetrics)

	errGroup.Go(func() error {
		return c.WorkerReceiveEntry(getColigateConfiguration(), 0, 1, addr.MustIAFrom(1, 1).AS())
	})

	now := time.Now()
	updateChannels[0] <- &storage.UpdateTask{
		Reservation: &storage.Reservation{
			Id: reservationIdOne,
			Indices: map[uint8]*storage.ReservationIndex{
				1: {
					Index:    1,
					Validity: now.Add(12 * time.Second),
					Sigmas: [][]byte{
						{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
						{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1},
						{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2},
						{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2, 3},
						{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2, 3, 4},
						{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2, 3, 4, 5},
					},
					BwCls: 60,
				},
			},
			Hops: []storage.HopField{
				{
					IngressId: 1,
					EgressId:  2,
				},
				{
					IngressId: 3,
					EgressId:  4,
				},
				{
					IngressId: 5,
					EgressId:  6,
				},
				{
					IngressId: 7,
					EgressId:  8,
				},
				{
					IngressId: 9,
					EgressId:  10,
				},
				{
					IngressId: 11,
					EgressId:  12,
				},
			},
		},
	}
	defaultPkt := &proc.DataPacket{
		PktArrivalTime: time.Now(),
		ScionLayer: &slayers.SCION{
			PathType:   4,
			SrcIA:      addr.MustIAFrom(1, 1),
			PayloadLen: 400,
		},
		ColibriPath: &colipath.ColibriPath{
			InfoField: &colipath.InfoField{
				Ver:         1,
				ResIdSuffix: reservationIdOne[:],
				BwCls:       60,
				ExpTick:     uint32(now.Add(12*time.Second).Unix() / 4),
				HFCount:     6,
			},
			HopFields: []*colipath.HopField{
				{
					IngressId: 1,
					EgressId:  2,
					Mac:       make([]byte, 4),
				},
				{
					IngressId: 3,
					EgressId:  4,
					Mac:       make([]byte, 4),
				},
				{
					IngressId: 5,
					EgressId:  6,
					Mac:       make([]byte, 4),
				},
				{
					IngressId: 7,
					EgressId:  8,
					Mac:       make([]byte, 4),
				},
				{
					IngressId: 9,
					EgressId:  10,
					Mac:       make([]byte, 4),
				},
				{
					IngressId: 11,
					EgressId:  12,
					Mac:       make([]byte, 4),
				},
			},
		},
		RawPacket: make([]byte, 400),
		Id:        reservationIdOne,
	}
	defaultPkt.ScionLayer.Path = defaultPkt.ColibriPath
	serializeBuffer := gopacket.NewSerializeBuffer()
	defaultPkt.ScionLayer.SerializeTo(serializeBuffer, gopacket.SerializeOptions{})
	copy(defaultPkt.RawPacket, serializeBuffer.Bytes())

	server, err := net.ListenUDP("udp", borderRouterAddr)
	if err != nil {
		b.Log(err)
		b.FailNow()
	}
	var counter int
	errGroup.Go(func() error {
		msgs := make([]ipv4.Message, 10)
		for i := 0; i < 10; i++ {
			msgs[i].Buffers = [][]byte{make([]byte, 500)}
		}

		packetConn := ipv4.NewPacketConn(server)

		for {
			n, err := packetConn.ReadBatch(msgs, syscall.MSG_WAITFORONE)
			if err != nil {
				return err
			}
			counter += n
		}
	})
	time.Sleep(10 * time.Millisecond)
	pkt := defaultPkt.Convert()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dataChannels[0] <- pkt
	}
	dataChannels[0] <- nil
	for len(dataChannels[0]) != 0 {
	}
	b.StopTimer()
	c.StopPacketForwarder()
	time.Sleep(100 * time.Millisecond)
	server.Close()
	errGroup.Wait()

	// check that the majority of the packets arrived
	assert.GreaterOrEqual(b, float64(counter), float64(b.N)*0.99-128)
}
