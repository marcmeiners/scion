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

package storage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/scionproto/scion/go/coligate/storage"
)

var reservationIdOne [12]byte = [12]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}

func TestReservationNotFound(t *testing.T) {
	storage := &storage.Storage{}
	storage.InitStorageWithData(nil)
	res, found := storage.UseReservation(reservationIdOne,
		0, time.Now())
	assert.False(t, found)
	assert.Nil(t, res)
}

func TestReservationVersionNotFound(t *testing.T) {
	s := &storage.Storage{}
	resmap := make(map[[12]byte]*storage.Reservation)
	resvmap := make(map[uint8]*storage.ReservationIndex)
	resvmap[0] = &storage.ReservationIndex{
		Index:    0,
		Validity: time.Now().Add(1 * time.Minute),
	}

	resmap[reservationIdOne] = &storage.Reservation{
		Id:            reservationIdOne,
		Indices:       resvmap,
		ActiveIndexId: 0,
	}
	s.InitStorageWithData(resmap)

	res, found := s.UseReservation(reservationIdOne, 1, time.Now())
	assert.False(t, found)
	assert.Nil(t, res)
}

func TestActiveVersionIsProvidedVersion(t *testing.T) {
	s := &storage.Storage{}
	resmap := make(map[[12]byte]*storage.Reservation)
	resvmap := make(map[uint8]*storage.ReservationIndex)
	resvmap[0] = &storage.ReservationIndex{
		Index:    0,
		Validity: time.Now().Add(1 * time.Minute),
	}

	resmap[reservationIdOne] = &storage.Reservation{
		Id:            reservationIdOne,
		Indices:       resvmap,
		ActiveIndexId: 0,
	}
	s.InitStorageWithData(resmap)

	res, found := s.UseReservation(reservationIdOne, 0, time.Now())
	assert.True(t, found)
	assert.Equal(t, uint8(0), res.ActiveIndexId)
}

func TestActiveVersionOlder(t *testing.T) {
	s := &storage.Storage{}
	resmap := make(map[[12]byte]*storage.Reservation)
	resvmap := make(map[uint8]*storage.ReservationIndex)
	resvmap[0] = &storage.ReservationIndex{
		Index:    0,
		Validity: time.Now().Add(1 * time.Minute),
	}
	resvmap[1] = &storage.ReservationIndex{
		Index:    1,
		Validity: time.Now().Add(2 * time.Minute),
	}

	resmap[reservationIdOne] = &storage.Reservation{
		Id:            reservationIdOne,
		Indices:       resvmap,
		ActiveIndexId: 0,
	}
	s.InitStorageWithData(resmap)

	res, found := s.UseReservation(reservationIdOne, 1, time.Now())
	assert.True(t, found)
	assert.Equal(t, uint8(1), res.ActiveIndexId)
	_, found = res.Indices[0]
	assert.False(t, found)

}

func TestActiveVersionNewer(t *testing.T) {
	s := &storage.Storage{}
	resmap := make(map[[12]byte]*storage.Reservation)
	resvmap := make(map[uint8]*storage.ReservationIndex)
	resvmap[0] = &storage.ReservationIndex{
		Index:    0,
		Validity: time.Now().Add(2 * time.Minute),
	}
	resvmap[1] = &storage.ReservationIndex{
		Index:    1,
		Validity: time.Now().Add(1 * time.Minute),
	}

	resmap[reservationIdOne] = &storage.Reservation{
		Id:            reservationIdOne,
		Indices:       resvmap,
		ActiveIndexId: 0,
	}
	s.InitStorageWithData(resmap)
	_, found := s.UseReservation(reservationIdOne, 1, time.Now())
	assert.False(t, found)
	_, found = resvmap[1]
	assert.False(t, found)
}

func TestPacketValidityIsChecked(t *testing.T) {
	s := &storage.Storage{}
	resmap := make(map[[12]byte]*storage.Reservation)
	resvmap := make(map[uint8]*storage.ReservationIndex)
	now := time.Now()
	resvmap[0] = &storage.ReservationIndex{
		Index:    0,
		Validity: now,
	}

	resmap[reservationIdOne] = &storage.Reservation{
		Id:            reservationIdOne,
		Indices:       resvmap,
		ActiveIndexId: 0,
	}
	s.InitStorageWithData(resmap)

	res, found := s.UseReservation(reservationIdOne, 0, now)
	assert.True(t, found)
	assert.Equal(t, reservationIdOne, res.Id)
	res, found = s.UseReservation(reservationIdOne,
		0, now.Add(1*time.Second))
	assert.False(t, found)
	assert.Nil(t, res)
}
