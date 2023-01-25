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

package storage

import (
	"crypto/cipher"
	"time"

	"github.com/scionproto/scion/go/coligate/tokenbucket"
	"github.com/scionproto/scion/go/lib/log"
)

type Storage struct {
	reservations map[[12]byte]*Reservation
}
type Reservation struct {
	Id             [12]byte
	Hops           []HopField
	Rlc            uint8
	ActiveIndexId  uint8
	Indices        map[uint8]*ReservationIndex
	TrafficMonitor *TrafficMonitor
}
type HopField struct {
	IngressId uint16
	EgressId  uint16
}
type ReservationIndex struct {
	Index    uint8
	Validity time.Time
	Sigmas   [][]byte
	Ciphers  []cipher.Block
	BwCls    uint8
}

type TrafficMonitor struct {
	Bucket    *tokenbucket.TokenBucket
	LastBwcls uint8
}

type Task interface {
	Execute(*Storage)
}

type UpdateTask struct {
	Reservation *Reservation

	// The validity of the longest (but possible not active) reservation index.
	HighestValidity time.Time
}

type DeletionTask struct {
	ResId [12]byte
}

// Execute merges (overwrites if exists in both) all provided reservation indices
// with the currently stored indices. Creates a new entry if no reservation exists.
func (task *UpdateTask) Execute(store *Storage) {
	res, found := store.get(task.Reservation.Id)
	if found {
		defer res.deleteOlderIndices()
		for indexNumber, reservationIndex := range task.Reservation.Indices {
			res.Indices[indexNumber] = reservationIndex
		}
	} else {
		store.store(task.Reservation.Id, task.Reservation)
	}
}

func (task *DeletionTask) Execute(store *Storage) {
	store.remove(task.ResId)
}

// Current returns the active reservation index. If the active reservation
// index does not exist (anymore) nil is returned.
func (res *Reservation) Current() *ReservationIndex {
	ver, found := res.Indices[res.ActiveIndexId]
	if !found {
		return nil
	}
	return ver
}

// InitStorageWithData initializes the reservation storage
func (store *Storage) InitStorageWithData(data map[[12]byte]*Reservation) {
	if data == nil {
		store.reservations = make(map[[12]byte]*Reservation)
	} else {
		store.reservations = data
	}
}

func (store *Storage) get(resId [12]byte) (*Reservation, bool) {
	res, found := store.reservations[resId]
	return res, found
}

func (store *Storage) store(resId [12]byte, reservation *Reservation) {
	store.reservations[resId] = reservation
}

func (store *Storage) remove(resId [12]byte) {
	delete(store.reservations, resId)
}

func NewReservation(resId [12]byte, hops []HopField) *Reservation {
	return &Reservation{
		Id:      resId,
		Rlc:     0, //TODO(rohrerj)
		Indices: make(map[uint8]*ReservationIndex),
		Hops:    hops,
	}
}
func NewIndex(index uint8, validity time.Time, bwCls uint8, sigmas [][]byte) *ReservationIndex {
	return &ReservationIndex{
		Index:    index,
		Validity: validity,
		BwCls:    bwCls,
		Sigmas:   sigmas,
	}
}
func NewUpdateTask(res *Reservation, highestValidity time.Time) *UpdateTask {
	return &UpdateTask{
		Reservation:     res,
		HighestValidity: highestValidity,
	}
}
func NewDeletionTask(resId [12]byte) *DeletionTask {
	return &DeletionTask{
		ResId: resId,
	}
}

// UseReservation checks whether the reservation index exists and is valid. If the
// reservation exists but contains no longer valid indices, they will be removed.
// If the reservation index exists and is valid, the reservation is returned.
func (store *Storage) UseReservation(resId [12]byte, providedIndex uint8,
	pktTime time.Time) (*Reservation, bool) {

	log.Debug("use resId", "resId", resId)
	res, found := store.get(resId)
	if !found {
		log.Debug("reservation not found")
		return nil, false
	}
	index, found := res.Indices[providedIndex]
	if !found {
		log.Debug("reservation index not found")
		return nil, false
	}
	defer res.deleteOlderIndices()

	if res.ActiveIndexId != providedIndex {
		activeVer, found := res.Indices[res.ActiveIndexId]
		if found && activeVer.Validity.After(index.Validity) {
			// Active index exists but has longer validity than provided index
			return nil, false
		} else {
			res.ActiveIndexId = providedIndex
		}
	}

	if !pktTime.After(index.Validity) {
		return res, true
	}

	return nil, false
}

// Compares the validity of all indices with the active index and deletes all
// indices whose validity is behind the active index. If the active index
// is not valid, it will do the comparison with the current time.
func (res *Reservation) deleteOlderIndices() {
	v, found := res.Indices[res.ActiveIndexId]
	var t time.Time
	if found {
		t = v.Validity
	} else {
		t = time.Now()
	}
	for _, ver := range res.Indices {
		if ver.Validity.Before(t) {
			delete(res.Indices, uint8(ver.Index))
		}
	}
}
