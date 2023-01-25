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
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"

	"github.com/scionproto/scion/go/coligate/processing"
	"github.com/scionproto/scion/go/coligate/storage"
	"github.com/scionproto/scion/go/lib/addr"
	libcolibri "github.com/scionproto/scion/go/lib/colibri/dataplane"
	"github.com/scionproto/scion/go/lib/slayers"
	"github.com/scionproto/scion/go/lib/slayers/path/colibri"
	"github.com/scionproto/scion/go/pkg/coligate/config"
)

func getColigateConfiguration() *config.ColigateConfig {
	cfg := &config.ColigateConfig{
		NumBitsForGatewayId:        1,
		NumBitsForWorkerId:         8,
		NumBitsForPerWorkerCounter: 23,
		NumWorkers:                 8,
		MaxQueueSizePerWorker:      256,
		Salt:                       "",
		ColibriGatewayID:           1,
	}
	return cfg
}

func TestValidate(t *testing.T) {
	type entry struct {
		proc processing.DataPacket
		err  string
	}

	type test struct {
		name     string
		entries  []entry
		resStore map[[12]byte]*storage.Reservation
	}
	worker := processing.NewWorker(getColigateConfiguration(), 1, 1, 1, nil, coligateMetrics)

	var startTime = time.Unix(0, 0)

	tests := []test{
		{
			name: "TestValidateCFlagIsSet",
			entries: []entry{
				{
					proc: processing.DataPacket{
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								C: true,
							},
						},
					},
					err: "Invalid flags",
				},
			},
		},
		{
			name: "TestValidateRFlagIsSet",
			entries: []entry{
				{
					proc: processing.DataPacket{
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								R: true,
							},
						},
					},
					err: "Invalid flags",
				},
			},
		},
		{
			name: "TestValidateSFlagIsSet",
			entries: []entry{
				{
					proc: processing.DataPacket{
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								S: true,
							},
						},
					},
					err: "Invalid flags",
				},
			},
		},
		{
			name: "TestValidateReservationBelongsToOtherAS",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id:            reservationIdOne,
					Hops:          make([]storage.HopField, 1),
					ActiveIndexId: 0,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(1),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
							},
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 2),
						},
					},
					err: "Reservation does not belong to local AS",
				},
			},
		},
		{
			name: "TestValidateReservationDoesNotExist",
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
							},
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "E2E reservation is invalid",
				},
			},
		},
		{
			name: "TestValidateInvalidNumberOfHopfields",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id:            reservationIdOne,
					Hops:          make([]storage.HopField, 1),
					ActiveIndexId: 0,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(1),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
							},
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "Number of hopfields is invalid",
				},
			},
		},
		{
			name: "TestValidateCurrHFIsInvalid",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id:            reservationIdOne,
					Hops:          make([]storage.HopField, 1),
					ActiveIndexId: 0,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(1),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
								CurrHF:      1,
							},
							HopFields: make([]*colibri.HopField, 1),
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "CurrHF is invalid",
				},
			},
		},
		{
			name: "TestValidateBwClsIsInvalid",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id:            reservationIdOne,
					Hops:          make([]storage.HopField, 1),
					ActiveIndexId: 0,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(1),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
								CurrHF:      0,
								BwCls:       2,
							},
							HopFields: make([]*colibri.HopField, 1),
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "Bandwidth class is invalid",
				},
			},
		},
		{
			name: "TestValidateRlcIsInvalid",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id:            reservationIdOne,
					Hops:          make([]storage.HopField, 1),
					ActiveIndexId: 0,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(1),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
					Rlc: 1,
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
								CurrHF:      0,
								BwCls:       1,
								Rlc:         2,
							},
							HopFields: make([]*colibri.HopField, 1),
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "Latency class is invalid",
				},
			},
		},
		{
			name: "TestValidateExpTickIsInvalid",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id:            reservationIdOne,
					Hops:          make([]storage.HopField, 1),
					ActiveIndexId: 0,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(100 * time.Second),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
					Rlc: 1,
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
								CurrHF:      0,
								BwCls:       1,
								Rlc:         1,
								ExpTick:     1,
							},
							HopFields: make([]*colibri.HopField, 1),
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "ExpTick is invalid",
				},
			},
		},
		{
			name: "TestValidateInvalidIngressId",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id: reservationIdOne,
					Hops: []storage.HopField{
						{
							IngressId: 1,
							EgressId:  2,
						},
					},
					ActiveIndexId: 0,
					Rlc:           1,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(100 * time.Second),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
								Ver:         0,
								BwCls:       1,
								Rlc:         1,
								HFCount:     1,
								ExpTick:     uint32(startTime.Add(100*time.Second).Unix() / 4),
							},
							HopFields: []*colibri.HopField{
								{
									IngressId: 3,
									EgressId:  2,
								},
							},
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "IngressId is invalid",
				},
			},
		},
		{
			name: "TestValidateInvalidEgressId",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id: reservationIdOne,
					Hops: []storage.HopField{
						{
							IngressId: 1,
							EgressId:  2,
						},
					},
					ActiveIndexId: 0,
					Rlc:           1,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(100 * time.Second),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
								Ver:         0,
								BwCls:       1,
								Rlc:         1,
								HFCount:     1,
								ExpTick:     uint32(startTime.Add(100*time.Second).Unix() / 4),
							},
							HopFields: []*colibri.HopField{
								{
									IngressId: 1,
									EgressId:  3,
								},
							},
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "EgressId is invalid",
				},
			},
		},
		{
			name: "TestValidateAllValid",
			resStore: map[[12]byte]*storage.Reservation{
				reservationIdOne: {
					Id: reservationIdOne,
					Hops: []storage.HopField{
						{
							IngressId: 1,
							EgressId:  2,
						},
					},
					ActiveIndexId: 0,
					Rlc:           1,
					Indices: map[uint8]*storage.ReservationIndex{
						0: {
							Index:    0,
							Validity: startTime.Add(100 * time.Second),
							BwCls:    1,
							Sigmas:   make([][]byte, 1),
						},
					},
				},
			},
			entries: []entry{
				{
					proc: processing.DataPacket{
						Id:             reservationIdOne,
						PktArrivalTime: startTime,
						ColibriPath: &colibri.ColibriPath{
							InfoField: &colibri.InfoField{
								ResIdSuffix: reservationIdOne[:],
								Ver:         0,
								BwCls:       1,
								Rlc:         1,
								HFCount:     1,
								ExpTick:     uint32(startTime.Add(100*time.Second).Unix() / 4),
							},
							HopFields: []*colibri.HopField{
								{
									IngressId: 1,
									EgressId:  2,
								},
							},
						},
						ScionLayer: &slayers.SCION{
							SrcIA: addr.MustIAFrom(1, 1),
						},
					},
					err: "",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			worker.Storage.InitStorageWithData(tc.resStore)
			for _, en := range tc.entries {
				err := worker.Validate(en.proc.Convert())
				if en.err == "" {
					assert.NoError(t, err)
				} else {
					assert.True(t, strings.HasPrefix(err.Error(), en.err), err.Error())
				}
			}
		})
	}
}

func TestPerformTrafficMonitoring(t *testing.T) {
	type entry struct {
		proc    processing.DataPacket
		success bool
	}

	type test struct {
		name    string
		entries []entry
	}
	worker := processing.NewWorker(getColigateConfiguration(), 1, 1, 1, nil, coligateMetrics)

	var startTime = time.Unix(0, 0)

	tests := []test{
		{
			name: "TestPerformTrafficMonitoringOnePacketFullBandwidth",
			entries: []entry{
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 2000),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 1),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: false,
				},
			},
		},
		{
			name: "TestPerformTrafficMonitoringSeveralPacketsTotalFullBandwidth",
			entries: []entry{
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 500),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 500),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 500),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 500),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 1),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: false,
				},
			},
		},
		{
			name: "TestPerformTrafficMonitoringDifferentReservationsHaveNoImpact",
			entries: []entry{
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 2000),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 2000),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            [12]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2},
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
			},
		},
		{
			name: "TestPerformTrafficMonitoringSeveralReservationIndicesMapToSameBucket",
			entries: []entry{
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 2000),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 1),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 1,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
								1: {
									Index:    1,
									Validity: startTime.Add(2),
									BwCls:    1,
								},
							},
						},
					},
					success: false,
				},
			},
		},
		{
			name: "TestPerformTrafficMonitoringIncreasedBandwidth",
			entries: []entry{
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 2000),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 2750),
						PktArrivalTime: startTime.Add(1 * time.Second),
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 1,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    1,
								},
								1: {
									Index:    1,
									Validity: startTime.Add(2 * time.Second),
									BwCls:    2,
								},
							},
						},
					},
					success: true,
				},
			},
		},
		{
			name: "TestPerformTrafficMonitoringDecreasedBandwidth",
			entries: []entry{
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 2750),
						PktArrivalTime: startTime,
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 0,
							Indices: map[uint8]*storage.ReservationIndex{
								0: {
									Index:    0,
									Validity: startTime.Add(1),
									BwCls:    2,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 2000),
						PktArrivalTime: startTime.Add(1 * time.Second),
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 1,
							Indices: map[uint8]*storage.ReservationIndex{
								1: {
									Index:    1,
									Validity: startTime.Add(2 * time.Second),
									BwCls:    1,
								},
							},
						},
					},
					success: true,
				},
				{
					proc: processing.DataPacket{
						RawPacket:      make([]byte, 1),
						PktArrivalTime: startTime.Add(1 * time.Second),
						Reservation: &storage.Reservation{
							Id:            reservationIdOne,
							ActiveIndexId: 1,
							Indices: map[uint8]*storage.ReservationIndex{
								1: {
									Index:    1,
									Validity: startTime.Add(2 * time.Second),
									BwCls:    1,
								},
							},
						},
					},
					success: false,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := make(map[[12]byte]*storage.TrafficMonitor)
			for _, en := range tc.entries {
				monitor := m[en.proc.Reservation.Id]
				en.proc.Reservation.TrafficMonitor = monitor
				err := worker.PerformTrafficMonitoring(en.proc.Convert())
				m[en.proc.Reservation.Id] = en.proc.Reservation.TrafficMonitor
				assert.True(t, (err == nil && en.success) || (err != nil && !en.success))
			}
		})
	}
}

func TestUpdateCounter(t *testing.T) {
	w := processing.NewWorker(&config.ColigateConfig{
		NumBitsForGatewayId:        8,
		NumBitsForWorkerId:         8,
		NumBitsForPerWorkerCounter: 16,
	}, 13, 3, 1, nil, coligateMetrics)
	// Check that it starts with the correct value
	assert.Equal(t, uint32(0x30d0000), w.CoreIdCounter)

	// Check that increments in the first few bits works
	for i := 1; i <= 16; i++ {
		w.UpdateCounter()
		assert.Equal(t, uint32(0x30d0000+i), w.CoreIdCounter)
	}

	// Check that increments in the last few bits works
	w.CoreIdCounter = 0x30dfff0
	for i := 0xfff1; i <= 0xffff; i++ {
		w.UpdateCounter()
		assert.Equal(t, uint32(0x30d0000+i), w.CoreIdCounter)
	}
	// Check that the wraparound works
	w.UpdateCounter()
	assert.Equal(t, uint32(0x30d0000), w.CoreIdCounter)
}

// TestUpdateMacs tests that for a valid sigma, UpdateFields computes the correct mac values that
// can be verified with the help of the private key.
func TestUpdateMacs(t *testing.T) {
	privateKey := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	privateKeyCipher, err := libcolibri.InitColibriKey(privateKey)
	assert.NoError(t, err)

	w := processing.NewWorker(getColigateConfiguration(), 1, 1, 1, nil, coligateMetrics)

	d := &processing.DataPacket{
		PktArrivalTime: time.Unix(0, 0),
		RawPacket:      make([]byte, 200),
		ColibriPath: &colibri.ColibriPath{
			InfoField: &colibri.InfoField{
				ExpTick:     1,
				OrigPayLen:  0,
				HFCount:     1,
				ResIdSuffix: make([]byte, 12),
			},
			HopFields: []*colibri.HopField{
				{
					IngressId: 1,
					EgressId:  2,
					Mac:       make([]byte, 4),
				},
			},
		},
		ScionLayer: &slayers.SCION{
			SrcAddrType: slayers.T4Ip,
			DstAddrType: slayers.T4Ip,
			SrcAddrLen:  slayers.AddrLen4,
			DstAddrLen:  slayers.AddrLen4,
			RawSrcAddr:  []byte("1234"),
			RawDstAddr:  []byte("5678"),
			SrcIA:       addr.MustIAFrom(1, 1),
			DstIA:       addr.MustIAFrom(2, 2),
			PathType:    colibri.PathType,
		},
		Reservation: &storage.Reservation{
			Id:            reservationIdOne,
			ActiveIndexId: 0,
			Hops: []storage.HopField{
				{
					IngressId: 1,
					EgressId:  2,
				},
			},
			Indices: map[uint8]*storage.ReservationIndex{
				0: {
					Index:  0,
					Sigmas: make([][]byte, 1),
				},
			},
		},
	}
	// Create the sigma that colibri service would later send to colibri gateway
	sigmaBuffer := make([]byte, 16)
	err = libcolibri.MACSigma(sigmaBuffer, privateKeyCipher, d.ColibriPath.InfoField,
		d.ColibriPath.HopFields[0], d.ScionLayer)
	assert.NoError(t, err)

	// Update mac fields of EE data packet with wrong sigma
	d.Reservation.Indices[0].Sigmas[0] = []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	err = w.Stamp(d.Convert())
	assert.NoError(t, err)
	// Validate the computed mac
	err = libcolibri.VerifyMAC(privateKeyCipher, d.ColibriPath.PacketTimestamp,
		d.ColibriPath.InfoField, d.ColibriPath.HopFields[0], d.ScionLayer)
	assert.Error(t, err)

	// Update mac fields of EE data packet with correct sigma
	d.Reservation.Indices[0].Sigmas = [][]byte{
		sigmaBuffer,
	}
	d.Reservation.Indices[0].Ciphers = nil
	err = w.Stamp(d.Convert())
	assert.NoError(t, err)
	// Validate the computed mac
	err = libcolibri.VerifyMAC(privateKeyCipher, d.ColibriPath.PacketTimestamp,
		d.ColibriPath.InfoField, d.ColibriPath.HopFields[0], d.ScionLayer)
	assert.NoError(t, err)
}

func BenchmarkParsing(b *testing.B) {
	payloadLen := uint16(500)
	s := &slayers.SCION{
		SrcIA:      addr.MustIAFrom(1, 1),
		DstIA:      addr.MustIAFrom(2, 2),
		PayloadLen: payloadLen,
		HdrLen:     27,
		BaseLayer: slayers.BaseLayer{
			Payload: make([]byte, payloadLen),
		},
		PathType: colibri.PathType,
		Path: &colibri.ColibriPath{
			InfoField: &colibri.InfoField{
				ResIdSuffix: make([]byte, 12),
				HFCount:     5,
				OrigPayLen:  payloadLen,
			},
			HopFields: []*colibri.HopField{
				{
					IngressId: 1,
					EgressId:  2,
					Mac:       []byte{1, 2, 3, 4},
				},
				{
					IngressId: 1,
					EgressId:  2,
					Mac:       []byte{1, 2, 3, 4},
				},
				{
					IngressId: 1,
					EgressId:  2,
					Mac:       []byte{1, 2, 3, 4},
				},
				{
					IngressId: 1,
					EgressId:  2,
					Mac:       []byte{1, 2, 3, 4},
				},
				{
					IngressId: 1,
					EgressId:  2,
					Mac:       []byte{1, 2, 3, 4},
				},
			},
		},
	}
	buffer := gopacket.NewSerializeBuffer()
	err := s.SerializeTo(buffer, gopacket.SerializeOptions{})
	assert.NoError(b, err)
	payload := make([]byte, payloadLen)
	bytes := []byte{}
	bytes = append(bytes, buffer.Bytes()...)
	bytes = append(bytes, payload...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := processing.Parse(bytes)
		assert.NoError(b, err)
	}
}

func BenchmarkProcess(b *testing.B) {
	errGroup := &errgroup.Group{}
	c := &processing.Processor{}
	borderRouterAddr, err := net.ResolveUDPAddr("udp", "localhost:30000")
	if err != nil {
		b.Error(err)
	}
	c.SetupPacketForwarder(errGroup, map[uint16]*net.UDPAddr{
		2: borderRouterAddr,
	}, coligateMetrics)

	w := processing.NewWorker(getColigateConfiguration(), 1, 1, addr.MustIAFrom(1, 1).AS(),
		c.GetPacketForwarderContainers(), coligateMetrics)
	now := time.Now()
	resStore := map[[12]byte]*storage.Reservation{
		reservationIdOne: {
			Id: reservationIdOne,
			Indices: map[uint8]*storage.ReservationIndex{
				1: {
					Index:    0,
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
	w.Storage.InitStorageWithData(resStore)
	defaultPkt := &processing.DataPacket{
		Id:             reservationIdOne,
		PktArrivalTime: time.Now(),
		ScionLayer: &slayers.SCION{
			PathType: 4,
			SrcIA:    addr.MustIAFrom(1, 1),
		},
		ColibriPath: &colibri.ColibriPath{
			InfoField: &colibri.InfoField{
				Ver:         1,
				ResIdSuffix: reservationIdOne[:],
				BwCls:       60,
				ExpTick:     uint32(now.Add(12*time.Second).Unix() / 4),
			},
			HopFields: []*colibri.HopField{
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
	}
	server, err := net.ListenUDP("udp", borderRouterAddr)
	if err != nil {
		b.Log(err)
		b.FailNow()
	}
	defer server.Close()
	time.Sleep(1 * time.Millisecond)
	pkt := defaultPkt.Convert()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.NoError(b, w.Process(pkt))
	}
	c.StopPacketForwarder()
	errGroup.Wait()
}

func BenchmarkValidate(b *testing.B) {
	w := processing.NewWorker(getColigateConfiguration(), 1, 1, 1, nil, coligateMetrics)
	now := time.Now()

	resStore := map[[12]byte]*storage.Reservation{
		reservationIdOne: {
			Id: reservationIdOne,
			Hops: []storage.HopField{
				{
					IngressId: 1,
					EgressId:  2,
				},
			},
			ActiveIndexId: 0,
			Rlc:           1,
			Indices: map[uint8]*storage.ReservationIndex{
				0: {
					Index:    0,
					Validity: now.Add(16 * time.Second),
					BwCls:    1,
					Sigmas:   make([][]byte, 1),
				},
			},
		},
	}
	w.Storage.InitStorageWithData(resStore)
	d := &processing.DataPacket{
		Id:             reservationIdOne,
		PktArrivalTime: now,
		ColibriPath: &colibri.ColibriPath{
			InfoField: &colibri.InfoField{
				ResIdSuffix: reservationIdOne[:],
				Ver:         0,
				BwCls:       1,
				Rlc:         1,
				HFCount:     1,
				ExpTick:     uint32(now.Add(16*time.Second).Unix() / 4),
			},
			HopFields: []*colibri.HopField{
				{
					IngressId: 1,
					EgressId:  2,
				},
			},
		},
		ScionLayer: &slayers.SCION{
			SrcIA: addr.MustIAFrom(1, 1),
		},
	}
	pkt := d.Convert()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.NoError(b, w.Validate(pkt))
	}
}

func BenchmarkTrafficMonitoring(b *testing.B) {
	w := processing.NewWorker(getColigateConfiguration(), 1, 1, 1, nil, coligateMetrics)
	now := time.Now()
	d := &processing.DataPacket{
		Id:             reservationIdOne,
		PktArrivalTime: now,
		Reservation: &storage.Reservation{
			Id:            reservationIdOne,
			ActiveIndexId: 0,
			Indices: map[uint8]*storage.ReservationIndex{
				0: {
					Index:    0,
					Validity: now.Add(12 * time.Second),
					BwCls:    40,
				},
			},
		},
	}
	pkt := d.Convert()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.NoError(b, w.PerformTrafficMonitoring(pkt))
	}
}

func BenchmarkStamp(b *testing.B) {
	w := processing.NewWorker(getColigateConfiguration(), 1, 1, 1, nil, coligateMetrics)
	now := time.Now()
	defaultPkt := &processing.DataPacket{
		Id:             reservationIdOne,
		PktArrivalTime: now,
		ScionLayer: &slayers.SCION{
			PathType: colibri.PathType,
			SrcIA:    addr.MustIAFrom(1, 1),
		},
		ColibriPath: &colibri.ColibriPath{
			InfoField: &colibri.InfoField{
				Ver:         1,
				ResIdSuffix: reservationIdOne[:],
				BwCls:       1,
				ExpTick:     uint32(now.Add(12*time.Second).Unix() / 4),
				HFCount:     5,
			},
			HopFields: []*colibri.HopField{
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
		Reservation: &storage.Reservation{
			Id: reservationIdOne,
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
			ActiveIndexId: 0,
			Rlc:           1,
			Indices: map[uint8]*storage.ReservationIndex{
				0: {
					Index:    0,
					Validity: now.Add(12 * time.Second),
					BwCls:    1,
					Sigmas: [][]byte{
						{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
						{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1},
						{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2},
						{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2, 3},
						{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2, 3, 4},
						{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2, 3, 4, 5},
					},
				},
			},
		},
	}
	pkt := defaultPkt.Convert()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Stamp(pkt)
	}
}

func BenchmarkForwardPacket(b *testing.B) {
	errGroup := &errgroup.Group{}
	c := &processing.Processor{}
	borderRouterAddr, err := net.ResolveUDPAddr("udp", "localhost:30000")
	if err != nil {
		b.Error(err)
	}
	c.SetupPacketForwarder(errGroup, map[uint16]*net.UDPAddr{
		2: borderRouterAddr,
	}, coligateMetrics)
	w := processing.NewWorker(getColigateConfiguration(), 1, 1, 1,
		c.GetPacketForwarderContainers(), coligateMetrics)
	now := time.Now()
	defaultPkt := &processing.DataPacket{
		Id:             reservationIdOne,
		PktArrivalTime: time.Now(),
		ScionLayer: &slayers.SCION{
			PathType: 4,
			SrcIA:    addr.MustIAFrom(1, 1),
		},
		ColibriPath: &colibri.ColibriPath{
			InfoField: &colibri.InfoField{
				Ver:         1,
				ResIdSuffix: reservationIdOne[:],
				BwCls:       60,
				ExpTick:     uint32(now.Add(12*time.Second).Unix() / 4),
			},
			HopFields: []*colibri.HopField{
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
	}

	server, err := net.ListenUDP("udp", borderRouterAddr)
	if err != nil {
		b.Log(err)
		b.FailNow()
	}
	defer server.Close()
	pkt := defaultPkt.Convert()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.ForwardPacket(pkt)
	}
	c.StopPacketForwarder()
	errGroup.Wait()
}
