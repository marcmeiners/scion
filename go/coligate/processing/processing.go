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

package processing

import (
	"crypto/cipher"
	"encoding/binary"
	"time"

	"github.com/scionproto/scion/go/coligate/storage"
	"github.com/scionproto/scion/go/coligate/tokenbucket"
	libaddr "github.com/scionproto/scion/go/lib/addr"
	libcolibri "github.com/scionproto/scion/go/lib/colibri/dataplane"
	libtypes "github.com/scionproto/scion/go/lib/colibri/reservation"
	"github.com/scionproto/scion/go/lib/serrors"
	"github.com/scionproto/scion/go/lib/slayers"
	"github.com/scionproto/scion/go/lib/slayers/path/colibri"
	"github.com/scionproto/scion/go/pkg/coligate/config"
)

type Worker struct {
	Id             uint32
	CoreIdCounter  uint32
	NumCounterBits int

	Storage         *storage.Storage
	forwardChannels map[uint16]*packetForwarderContainer
	LocalAS         libaddr.AS
	metrics         *ColigateMetrics
}

type dataPacket struct {
	pktArrivalTime time.Time
	scionLayer     *slayers.SCION
	colibriPath    *colibri.ColibriPath
	reservation    *storage.Reservation
	rawPacket      []byte
	id             [12]byte
}

// Parse parses only the bare minimum that is required to extract the reservation id
func Parse(rawPacket []byte) (*dataPacket, error) {
	if len(rawPacket) < 76 {
		return nil, serrors.New("raw packet length too small")
	}
	addrLenByte := rawPacket[9]
	dstAddrLen := addrLenByte >> 4 & 0x3
	srcAddrLen := addrLenByte & 0x3
	dataPacket := dataPacket{
		rawPacket: rawPacket,
	}
	offsetToColibriHeader := slayers.CmnHdrLen + 2*libaddr.IABytes +
		(int(dstAddrLen)+1)*4 + (int(srcAddrLen)+1)*4
	if len(rawPacket) < offsetToColibriHeader+40 {
		return nil, serrors.New("raw packet length too small")
	}
	copy(dataPacket.id[:12], rawPacket[offsetToColibriHeader+12:])
	return &dataPacket, nil
}

// NewWorker initializes the worker with its id, tokenbuckets and reservations
func NewWorker(config *config.ColigateConfig, workerId uint32, gatewayId uint32,
	localAS libaddr.AS, forwardChannels map[uint16]*packetForwarderContainer,
	metrics *ColigateMetrics) *Worker {
	w := &Worker{
		Id: workerId,
		CoreIdCounter: (gatewayId << (32 - config.NumBitsForGatewayId)) |
			(workerId << (32 - config.NumBitsForGatewayId - config.NumBitsForWorkerId)),
		NumCounterBits:  config.NumBitsForPerWorkerCounter,
		LocalAS:         localAS,
		Storage:         &storage.Storage{},
		forwardChannels: forwardChannels,
		metrics:         metrics,
	}
	w.Storage.InitStorageWithData(nil)

	return w
}

// Parses the some fields of the scion header that are needed by colibri gateway
// and the full colibri header
func (w *Worker) realParse(d *dataPacket) error {
	payloadLen := binary.BigEndian.Uint16(d.rawPacket[6:8])
	realPayloadLen := len(d.rawPacket) - int(d.rawPacket[5])*4
	if payloadLen != uint16(realPayloadLen) {
		return serrors.New("payload length field does not match actual packet payload length",
			"payloadLen", payloadLen, "realPayloadLen", realPayloadLen)
	}
	addrLenByte := d.rawPacket[9]
	dstAddrLen := addrLenByte >> 4 & 0x3
	srcAddrLen := addrLenByte & 0x3
	d.scionLayer = &slayers.SCION{
		DstAddrLen: slayers.AddrLen(dstAddrLen),
		SrcAddrLen: slayers.AddrLen(srcAddrLen),
		SrcIA: libaddr.IA(binary.BigEndian.Uint64(
			d.rawPacket[slayers.CmnHdrLen+libaddr.IABytes:])),
	}
	d.colibriPath = &colibri.ColibriPath{}
	offset := slayers.CmnHdrLen + 2*libaddr.IABytes + (int(dstAddrLen)+1)*4 + (int(srcAddrLen)+1)*4
	err := d.colibriPath.DecodeFromBytes(d.rawPacket[offset:])
	if err != nil {
		return err
	}
	d.pktArrivalTime = time.Now()
	return nil
}

// Processes the current packet based on the current dataPacket
func (w *Worker) process(d *dataPacket) error {
	if w == nil {
		return serrors.New("worker must not be nil")
	}
	if d == nil {
		return serrors.New("datapacket must not be nil")
	}
	if err := w.validate(d); err != nil {
		return err
	}
	if err := w.performTrafficMonitoring(d); err != nil {
		return err
	}
	if err := w.stamp(d); err != nil {
		return err
	}
	return w.forwardPacket(d)
}

// Validates the fields in the colibri header and checks that a valid reservation exists
func (w *Worker) validate(d *dataPacket) error {
	infoField := d.colibriPath.InfoField
	C := infoField.C
	R := infoField.R
	S := infoField.S
	if C || R || S {
		return serrors.New("Invalid flags", "S", S, "R", R, "C", C)
	}
	if d.scionLayer.SrcIA.AS() != w.LocalAS {
		return serrors.New("Reservation does not belong to local AS", "expected",
			w.LocalAS, "actual", d.scionLayer.SrcIA.AS())
	}

	reservation, isValid := w.Storage.UseReservation(d.id, infoField.Ver, d.pktArrivalTime)
	if !isValid {
		return serrors.New("E2E reservation is invalid")
	}
	d.reservation = reservation

	return w.validateFields(d)
}

// Validates the colibri header fields
// TODO(rohrerj) Requires further discussions. Checks might be removed. Make sure that
// coligate cannot crash if those checks are removed.
func (w *Worker) validateFields(d *dataPacket) error {
	infoField := d.colibriPath.InfoField
	currentIndex := d.reservation.Current()
	lenHopFields := len(d.colibriPath.HopFields)
	if currentIndex.Ciphers != nil {
		if lenHopFields != len(currentIndex.Ciphers) {
			return serrors.New("Number of hopfields is invalid", "expected",
				len(currentIndex.Ciphers), "actual", lenHopFields)
		}
	} else {
		if lenHopFields != len(currentIndex.Sigmas) {
			return serrors.New("Number of hopfields is invalid", "expected",
				len(currentIndex.Sigmas), "actual", lenHopFields)
		}
	}

	if infoField.CurrHF != 0 {
		return serrors.New("CurrHF is invalid", "expected", 0, "actual", infoField.CurrHF)
	}
	if infoField.BwCls != currentIndex.BwCls {
		return serrors.New("Bandwidth class is invalid", "expected",
			currentIndex.BwCls, "actual", infoField.BwCls)
	}
	if infoField.Rlc != d.reservation.Rlc {
		return serrors.New("Latency class is invalid", "expected",
			d.reservation.Rlc, "actual", infoField.Rlc)
	}
	if infoField.ExpTick != uint32(currentIndex.Validity.Unix()/4) {
		return serrors.New("ExpTick is invalid", "expected",
			currentIndex.Validity.Unix()/4, "actual", infoField.ExpTick)
	}
	for i, hop := range d.reservation.Hops {
		if d.colibriPath.HopFields[i].EgressId != hop.EgressId {
			return serrors.New("EgressId is invalid", "expected", hop.EgressId,
				"actual", d.colibriPath.HopFields[i].EgressId)
		}
		if d.colibriPath.HopFields[i].IngressId != hop.IngressId {
			return serrors.New("IngressId is invalid", "expected", hop.IngressId,
				"actual", d.colibriPath.HopFields[i].IngressId)
		}
	}
	return nil
}

// Checks that the reservation is not overused
func (w *Worker) performTrafficMonitoring(d *dataPacket) error {
	monitor := d.reservation.TrafficMonitor
	currentBwCls := d.reservation.Current().BwCls
	if monitor != nil {
		if monitor.LastBwcls != currentBwCls {
			realBandwidth := 125 * libtypes.BWCls(currentBwCls).ToKbps()
			monitor.Bucket.SetRate(float64(realBandwidth))
			monitor.Bucket.SetBurstSize(float64(realBandwidth))
			monitor.LastBwcls = currentBwCls
		}
	} else {
		realBandwidth := 125 * libtypes.BWCls(currentBwCls).ToKbps()
		// TODO(rohrerj) set correct value for burst size
		monitor = &storage.TrafficMonitor{
			Bucket: tokenbucket.NewTokenBucket(d.pktArrivalTime,
				float64(realBandwidth), float64(realBandwidth)),
			LastBwcls: currentBwCls,
		}
		d.reservation.TrafficMonitor = monitor
	}

	if !monitor.Bucket.Apply(len(d.rawPacket), d.pktArrivalTime) {
		return serrors.New("data packet exceeded bandwidth")
	}
	return nil
}

func (w *Worker) updateCounter() {
	a := uint32(1<<(w.NumCounterBits) - 1)
	b := a << (32 - w.NumCounterBits)
	w.CoreIdCounter = w.CoreIdCounter&b + a&(w.CoreIdCounter+1)
}

// initializeCiphers initializes all the ciphers, stores them in the reservation and
// deletes the sigmas
func (w *Worker) initializeCiphers(d *dataPacket, currentIndex *storage.ReservationIndex) error {
	currentIndex.Ciphers = make([]cipher.Block, len(currentIndex.Sigmas))
	for i := 0; i < len(currentIndex.Ciphers); i++ {
		cipher, err := libcolibri.InitColibriKey(currentIndex.Sigmas[i])
		if err != nil {
			currentIndex.Ciphers = nil
			return err
		}
		currentIndex.Ciphers[i] = cipher
	}
	currentIndex.Sigmas = nil
	return nil
}

// Updates the timestamp and HFVs
func (w *Worker) stamp(d *dataPacket) error {
	currentIndex := d.reservation.Current()
	tsRel, err := libcolibri.CreateTsRel(d.colibriPath.InfoField.ExpTick, d.pktArrivalTime)
	if err != nil {
		return err
	}
	w.updateCounter()

	d.colibriPath.PacketTimestamp = libcolibri.CreateColibriTimestampCustom(tsRel, w.CoreIdCounter)
	// Pre-initialize and store all ciphers if they are not initialized already
	if currentIndex.Ciphers == nil {
		if err := w.initializeCiphers(d, currentIndex); err != nil {
			return err
		}
	}
	// Set HVF values
	for i, cipher := range currentIndex.Ciphers {
		if err = libcolibri.MACE2EFromSigma(d.colibriPath.HopFields[i].Mac, cipher,
			d.colibriPath.InfoField, d.colibriPath.PacketTimestamp, d.scionLayer); err != nil {
			return err
		}
	}
	return d.colibriPath.SerializeTo(d.rawPacket[slayers.CmnHdrLen+d.scionLayer.AddrHdrLen():])
}

func (w *Worker) forwardPacket(d *dataPacket) error {
	egressId := d.colibriPath.GetCurrentHopField().EgressId
	forwarderContainer, found := w.forwardChannels[egressId]
	if !found {
		return serrors.New("Forward Channel for egress id not found", "egressId", egressId)
	}
	index := w.Id % uint32(forwarderContainer.ForwarderCount)
	forwarderContainer.Forwarders[index].ForwardChannel <- d.rawPacket
	return nil
}
