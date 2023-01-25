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

package grpc

import (
	"context"

	"github.com/scionproto/scion/go/coligate/storage"
	libaddr "github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/log"
	libmetrics "github.com/scionproto/scion/go/lib/metrics"
	"github.com/scionproto/scion/go/lib/util"
	cgpb "github.com/scionproto/scion/go/pkg/proto/coligate"
)

type Coligate struct {
	LocalIA                      libaddr.IA
	ReservationChannels          []chan *storage.UpdateTask
	CleanupChannel               chan *storage.UpdateTask
	UpdateSigmasTotalPromCounter libmetrics.Counter
	FindWorker                   func([12]byte) uint32
}

var _ cgpb.ColibriGatewayServiceServer = (*Coligate)(nil)

func (s *Coligate) UpdateSigmas(ctx context.Context, msg *cgpb.UpdateSigmasRequest) (
	*cgpb.UpdateSigmasResponse, error) {

	log.Debug("Call to UpdateSigmas")
	newId := [12]byte{}
	copy(newId[:], msg.Suffix)

	s.UpdateSigmasTotalPromCounter.Add(1)
	task := &storage.UpdateTask{
		Reservation: &storage.Reservation{
			Id:  newId,
			Rlc: uint8(msg.Rlc),
			Indices: map[uint8]*storage.ReservationIndex{
				uint8(msg.Index): {
					Index:    uint8(msg.Index),
					Validity: util.SecsToTime(msg.ExpirationTime),
					BwCls:    uint8(msg.Bwcls),
					Sigmas:   msg.Sigmas,
				},
			},
			Hops: make([]storage.HopField, len(msg.HopInterfaces)),
		},
		HighestValidity: util.SecsToTime(msg.ExpirationTime),
	}

	for i, hop := range msg.HopInterfaces {
		task.Reservation.Hops[i].EgressId = uint16(hop.Egressid)
		task.Reservation.Hops[i].IngressId = uint16(hop.Ingressid)
	}
	s.CleanupChannel <- task

	s.ReservationChannels[s.FindWorker(newId)] <- task

	return &cgpb.UpdateSigmasResponse{}, nil
}
