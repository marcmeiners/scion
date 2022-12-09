// Copyright 2020 Anapaya Systems
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

package slayers

import (
	"encoding/binary"

	"github.com/google/gopacket"

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/common"
	"github.com/scionproto/scion/go/lib/serrors"
	"github.com/scionproto/scion/go/lib/slayers/path"
	"github.com/scionproto/scion/go/lib/slayers/path/colibri"
	"github.com/scionproto/scion/go/lib/slayers/path/empty"
	"github.com/scionproto/scion/go/lib/slayers/path/epic"
	"github.com/scionproto/scion/go/lib/slayers/path/onehop"
	"github.com/scionproto/scion/go/lib/slayers/path/scion"
	sheader "github.com/scionproto/scion/go/lib/slayers/scion"
)

const (
	// LineLen is the length of a SCION header line in bytes.
	LineLen = 4
	// CmnHdrLen is the length of the SCION common header in bytes.
	CmnHdrLen = 12
	// SCIONVersion is the currently supported version of the SCION header format. Different
	// versions are not guaranteed to be compatible to each other.
	SCIONVersion = 0
)

func init() {
	empty.RegisterPath()
	scion.RegisterPath()
	onehop.RegisterPath()
	epic.RegisterPath()
	colibri.RegisterPath()
}

// BaseLayer is a convenience struct which implements the LayerData and
// LayerPayload functions of the Layer interface.
// Copy-pasted from gopacket/layers (we avoid importing this due its massive size)
type BaseLayer struct {
	// Contents is the set of bytes that make up this layer.  IE: for an
	// Ethernet packet, this would be the set of bytes making up the
	// Ethernet frame.
	Contents []byte
	// Payload is the set of bytes contained by (but not part of) this
	// Layer.  Again, to take Ethernet as an example, this would be the
	// set of bytes encapsulated by the Ethernet protocol.
	Payload []byte
}

// LayerContents returns the bytes of the packet layer.
func (b *BaseLayer) LayerContents() []byte { return b.Contents }

// LayerPayload returns the bytes contained within the packet layer.
func (b *BaseLayer) LayerPayload() []byte { return b.Payload }

type SCION struct {
	BaseLayer
	sheader.Header
	// PathType specifies the type of path in this SCION header.
	PathType path.Type
	// Path is the path contained in the SCION header. It depends on the PathType field.
	Path path.Path
}

func (s *SCION) LayerType() gopacket.LayerType {
	return LayerTypeSCION
}

func (s *SCION) CanDecode() gopacket.LayerClass {
	return LayerClassSCION
}

func (s *SCION) NextLayerType() gopacket.LayerType {
	return scionNextLayerType(s.Header.NextHdr)
}

func (s *SCION) LayerPayload() []byte {
	return s.Payload
}

func (s *SCION) NetworkFlow() gopacket.Flow {
	// TODO(shitz): Investigate how we can use gopacket.Flow.
	return gopacket.Flow{}
}

func (s *SCION) SerializeTo(b gopacket.SerializeBuffer, opts gopacket.SerializeOptions) error {
	scnLen := CmnHdrLen + s.AddrHdrLen() + s.Path.Len()
	buf, err := b.PrependBytes(scnLen)
	if err != nil {
		return err
	}
	if opts.FixLengths {
		s.HdrLen = uint8(scnLen / LineLen)
		s.PayloadLen = uint16(len(b.Bytes()) - scnLen)
	}

	// allow to modify values in the SCION header or in the path:
	// log.Debug("deleteme pre-sync", "path", s.Path)
	if err := s.Path.SyncWithScionHeader(&s.Header); err != nil {
		return err
	}
	// log.Debug("deleteme post-sync", "path", s.Path)

	// Serialize common header.
	firstLine := uint32(s.Version&0xF)<<28 | uint32(s.TrafficClass)<<20 | s.FlowID&0xFFFFF
	binary.BigEndian.PutUint32(buf[:4], firstLine)
	buf[4] = uint8(s.NextHdr)
	buf[5] = s.HdrLen
	binary.BigEndian.PutUint16(buf[6:8], s.PayloadLen)
	buf[8] = uint8(s.PathType)
	buf[9] = uint8(s.DstAddrType&0x3)<<6 | uint8(s.DstAddrLen&0x3)<<4 |
		uint8(s.SrcAddrType&0x3)<<2 | uint8(s.SrcAddrLen&0x3)
	binary.BigEndian.PutUint16(buf[10:12], 0)

	// Serialize address header.
	if err := s.SerializeAddrHdr(buf[CmnHdrLen:]); err != nil {
		return err
	}
	offset := CmnHdrLen + s.AddrHdrLen()

	// Serialize path header.
	return s.Path.SerializeTo(buf[offset:])
}

// DecodeFromBytes decodes the SCION layer. DecodeFromBytes resets the internal state of this layer
// to the state defined by the passed-in bytes. Slices in the SCION layer reference the passed-in
// data, so care should be taken to copy it first should later modification of data be required
// before the SCION layer is discarded.
func (s *SCION) DecodeFromBytes(data []byte, df gopacket.DecodeFeedback) error {
	// Decode common header.
	if len(data) < CmnHdrLen {
		df.SetTruncated()
		return serrors.New("packet is shorter than the common header length",
			"min", CmnHdrLen, "actual", len(data))
	}
	firstLine := binary.BigEndian.Uint32(data[:4])
	s.Version = uint8(firstLine >> 28)
	s.TrafficClass = uint8((firstLine >> 20) & 0xFF)
	s.FlowID = firstLine & 0xFFFFF
	s.NextHdr = common.L4ProtocolType(data[4])
	s.HdrLen = data[5]
	s.PayloadLen = binary.BigEndian.Uint16(data[6:8])
	s.PathType = path.Type(data[8])
	s.DstAddrType = sheader.AddrType(data[9] >> 6)
	s.DstAddrLen = sheader.AddrLen(data[9] >> 4 & 0x3)
	s.SrcAddrType = sheader.AddrType(data[9] >> 2 & 0x3)
	s.SrcAddrLen = sheader.AddrLen(data[9] & 0x3)

	// Decode address header.
	if err := s.DecodeAddrHdr(data[CmnHdrLen:]); err != nil {
		df.SetTruncated()
		return err
	}
	addrHdrLen := s.AddrHdrLen()
	offset := CmnHdrLen + addrHdrLen

	// Decode path header.
	var err error
	hdrBytes := int(s.HdrLen) * LineLen
	pathLen := hdrBytes - CmnHdrLen - addrHdrLen
	if pathLen < 0 {
		return serrors.New("invalid header, negative pathLen",
			"hdrBytes", hdrBytes, "addrHdrLen", addrHdrLen, "CmdHdrLen", CmnHdrLen)
	}
	if minLen := offset + pathLen; len(data) < minLen {
		df.SetTruncated()
		return serrors.New("provided buffer is too small", "expected", minLen, "actual", len(data))
	}

	s.Path, err = path.NewPath(s.PathType)
	if err != nil {
		return err
	}

	err = s.Path.BuildFromHeader(data[offset:offset+pathLen], &s.Header)
	if err != nil {
		return err
	}
	s.Contents = data[:hdrBytes]
	s.Payload = data[hdrBytes:]

	return nil
}

func decodeSCION(data []byte, pb gopacket.PacketBuilder) error {
	scn := &SCION{}
	err := scn.DecodeFromBytes(data, pb)
	if err != nil {
		return err
	}
	pb.AddLayer(scn)
	pb.SetNetworkLayer(scn)
	return pb.NextDecoder(scionNextLayerType(scn.NextHdr))
}

// scionNextLayerType returns the layer type for the given protocol identifier
// in a SCION base header.
func scionNextLayerType(t common.L4ProtocolType) gopacket.LayerType {
	switch t {
	case common.HopByHopClass:
		return LayerTypeHopByHopExtn
	case common.End2EndClass:
		return LayerTypeEndToEndExtn
	default:
		return scionNextLayerTypeL4(t)
	}
}

// scionNextLayerTypeAfterHBH returns the layer type for the given protocol
// identifier in a SCION hop-by-hop extension, excluding (repeated) hop-by-hop
// extensions.
func scionNextLayerTypeAfterHBH(t common.L4ProtocolType) gopacket.LayerType {
	switch t {
	case common.HopByHopClass:
		return gopacket.LayerTypeDecodeFailure
	case common.End2EndClass:
		return LayerTypeEndToEndExtn
	default:
		return scionNextLayerTypeL4(t)
	}
}

// scionNextLayerTypeAfterE2E returns the layer type for the given protocol
// identifier, in a SCION end-to-end extension, excluding (repeated or
// misordered) hop-by-hop extensions or (repeated) end-to-end extensions.
func scionNextLayerTypeAfterE2E(t common.L4ProtocolType) gopacket.LayerType {
	switch t {
	case common.HopByHopClass:
		return gopacket.LayerTypeDecodeFailure
	case common.End2EndClass:
		return gopacket.LayerTypeDecodeFailure
	default:
		return scionNextLayerTypeL4(t)
	}
}

// scionNextLayerTypeL4 returns the layer type for the given layer-4 protocol identifier.
// Does not handle extension header classes.
func scionNextLayerTypeL4(t common.L4ProtocolType) gopacket.LayerType {
	switch t {
	case common.L4UDP:
		return LayerTypeSCIONUDP
	case common.L4SCMP:
		return LayerTypeSCMP
	case common.L4BFD:
		return layerTypeBFD
	default:
		return gopacket.LayerTypePayload
	}
}

// AddrHdrLen returns the length of the address header (destination and source ISD-AS-Host triples)
// in bytes.
func (s *SCION) AddrHdrLen() int {
	return 2*addr.IABytes + addrBytes(s.DstAddrLen) + addrBytes(s.SrcAddrLen)
}

// SerializeAddrHdr serializes destination and source ISD-AS-Host address triples into the provided
// buffer. The caller must ensure that the correct address types and lengths are set in the SCION
// layer, otherwise the results of this method are undefined.
func (s *SCION) SerializeAddrHdr(buf []byte) error {
	if len(buf) < s.AddrHdrLen() {
		return serrors.New("provided buffer is too small", "expected", s.AddrHdrLen(),
			"actual", len(buf))
	}
	dstAddrBytes := addrBytes(s.DstAddrLen)
	srcAddrBytes := addrBytes(s.SrcAddrLen)
	offset := 0

	// log.Debug("deleteme address header", "dstIA", s.DstIA)

	binary.BigEndian.PutUint64(buf[offset:], uint64(s.DstIA))
	offset += addr.IABytes
	binary.BigEndian.PutUint64(buf[offset:], uint64(s.SrcIA))
	offset += addr.IABytes
	copy(buf[offset:offset+dstAddrBytes], s.RawDstAddr)
	offset += dstAddrBytes
	copy(buf[offset:offset+srcAddrBytes], s.RawSrcAddr)

	return nil
}

// DecodeAddrHdr decodes the destination and source ISD-AS-Host address triples from the provided
// buffer. The caller must ensure that the correct address types and lengths are set in the SCION
// layer, otherwise the results of this method are undefined.
func (s *SCION) DecodeAddrHdr(data []byte) error {
	if len(data) < s.AddrHdrLen() {
		return serrors.New("provided buffer is too small", "expected", s.AddrHdrLen(),
			"actual", len(data))
	}
	offset := 0
	s.DstIA = addr.IA(binary.BigEndian.Uint64(data[offset:]))
	offset += addr.IABytes
	s.SrcIA = addr.IA(binary.BigEndian.Uint64(data[offset:]))
	offset += addr.IABytes
	dstAddrBytes := addrBytes(s.DstAddrLen)
	srcAddrBytes := addrBytes(s.SrcAddrLen)
	s.RawDstAddr = data[offset : offset+dstAddrBytes]
	offset += dstAddrBytes
	s.RawSrcAddr = data[offset : offset+srcAddrBytes]

	return nil
}

func addrBytes(addrLen sheader.AddrLen) int {
	return (int(addrLen) + 1) * LineLen
}

// computeChecksum computes the checksum with the SCION pseudo header.
func (s *SCION) computeChecksum(upperLayer []byte, protocol uint8) (uint16, error) {
	if s == nil {
		return 0, serrors.New("SCION header missing")
	}
	csum, err := s.pseudoHeaderChecksum(len(upperLayer), protocol)
	if err != nil {
		return 0, err
	}
	csum = s.upperLayerChecksum(upperLayer, csum)
	folded := s.foldChecksum(csum)
	return folded, nil
}

func (s *SCION) pseudoHeaderChecksum(length int, protocol uint8) (uint32, error) {
	if len(s.RawDstAddr) == 0 {
		return 0, serrors.New("destination address missing")
	}
	if len(s.RawSrcAddr) == 0 {
		return 0, serrors.New("source address missing")
	}
	var csum uint32
	var srcIA, dstIA [8]byte
	binary.BigEndian.PutUint64(srcIA[:], uint64(s.SrcIA))
	binary.BigEndian.PutUint64(dstIA[:], uint64(s.DstIA))
	for i := 0; i < 8; i += 2 {
		csum += uint32(srcIA[i]) << 8
		csum += uint32(srcIA[i+1])
		csum += uint32(dstIA[i]) << 8
		csum += uint32(dstIA[i+1])
	}
	// Address length is guaranteed to be a multiple of 2 by the protocol.
	for i := 0; i < len(s.RawSrcAddr); i += 2 {
		csum += uint32(s.RawSrcAddr[i]) << 8
		csum += uint32(s.RawSrcAddr[i+1])
	}
	for i := 0; i < len(s.RawDstAddr); i += 2 {
		csum += uint32(s.RawDstAddr[i]) << 8
		csum += uint32(s.RawDstAddr[i+1])
	}
	l := uint32(length)
	csum += (l >> 16) + (l & 0xffff)
	csum += uint32(protocol)
	return csum, nil
}

func (s *SCION) upperLayerChecksum(upperLayer []byte, csum uint32) uint32 {
	// Compute safe boundary to ensure we do not access out of bounds.
	// Odd lengths are handled at the end.
	safeBoundary := len(upperLayer) - 1
	for i := 0; i < safeBoundary; i += 2 {
		csum += uint32(upperLayer[i]) << 8
		csum += uint32(upperLayer[i+1])
	}
	if len(upperLayer)%2 == 1 {
		csum += uint32(upperLayer[safeBoundary]) << 8
	}
	return csum
}

func (s *SCION) foldChecksum(csum uint32) uint16 {
	for csum > 0xffff {
		csum = (csum >> 16) + (csum & 0xffff)
	}
	return ^uint16(csum)
}
