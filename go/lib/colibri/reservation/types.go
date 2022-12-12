// Copyright 2019 ETH Zurich, Anapaya Systems
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

package reservation

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/serrors"
	"github.com/scionproto/scion/go/lib/util"
)

// ID identifies a COLIBRI segment or E2E reservation. The suffix differentiates
// reservations for the same AS.
// A segment ID has a 4 byte long suffix. The suffix is 12 byte long for an E2E reservation.
type ID struct {
	ASID   addr.AS
	Suffix []byte
}

const (
	IDSuffixSegLen = 4
	IDSuffixE2ELen = 12
	IDSegLen       = 6 + IDSuffixSegLen
	IDE2ELen       = 6 + IDSuffixE2ELen
)

var _ io.Reader = (*ID)(nil)

// NewID returns a new ID
func NewID(AS addr.AS, suffix []byte) (*ID, error) {
	if len(suffix) != IDSuffixSegLen && len(suffix) != IDSuffixE2ELen {
		return nil, serrors.New("wrong suffix length, should be 4 or 12", "actual_len", len(suffix))
	}
	id := ID{
		ASID:   AS,
		Suffix: append([]byte{}, suffix...),
	}
	return &id, nil
}

// IDFromRawBuffers constructs an ID from two separate buffers.
func IDFromRawBuffers(ASID, suffix []byte) (*ID, error) {
	if len(ASID) < 6 {
		return nil, serrors.New("buffers too small", "length_ASID", len(ASID),
			"length_suffix", len(suffix))
	}
	return NewID(addr.AS(binary.BigEndian.Uint64(append([]byte{0, 0}, ASID[:6]...))), suffix)
}

// IDFromRaw constructs a ID parsing a raw buffer.
func IDFromRaw(raw []byte) (*ID, error) {
	if len(raw) < 6 {
		return nil, serrors.New("buffer too small", "actual", len(raw))
	}
	return IDFromRawBuffers(raw[:6], raw[6:])
}

// IDFromString expects a string like "ff00:0:1-01234567" (suffix in hex).
// The function will not validate the ID to be legal. I.e. the suffix could be any length.
func IDFromString(str string) (*ID, error) {
	parts := strings.Split(str, "-")
	if len(parts) != 2 {
		return nil, serrors.New("bad colibri id", "string", str)
	}
	as, err := addr.ParseAS(parts[0])
	if err != nil {
		return nil, serrors.WrapStr("bad colibri id", err, "string", str)
	}
	suffix, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, serrors.WrapStr("bad colibri id", err, "string", str)
	}
	return &ID{
		ASID:   as,
		Suffix: suffix,
	}, nil
}

func (id *ID) SetSegmentSuffix(suffix int) {
	if id.Suffix == nil {
		id.Suffix = make([]byte, 4)
	}
	binary.BigEndian.PutUint32(id.Suffix, uint32(suffix))
}

// Len returns the length of this ID in bytes.
func (id *ID) Len() int {
	return 6 + len(id.Suffix)
}

func (id *ID) Equal(other *ID) bool {
	return id.ASID == other.ASID && bytes.Equal(id.Suffix, other.Suffix)
}

func (id *ID) Validate() error {
	if len(id.Suffix) != IDSuffixSegLen && len(id.Suffix) != IDSuffixE2ELen {
		return serrors.New("bad suffix", "suffix", hex.EncodeToString(id.Suffix))
	}
	return nil
}

func (id *ID) Copy() *ID {
	if id == nil {
		return nil
	}
	return &ID{
		ASID:   id.ASID,
		Suffix: append([]byte{}, id.Suffix...),
	}
}

func (id *ID) IsSegmentID() bool {
	return len(id.Suffix) == IDSuffixSegLen
}

func (id *ID) IsE2EID() bool {
	return len(id.Suffix) == IDSuffixE2ELen
}

// Read serializes this ID into the buffer.
func (id *ID) Read(raw []byte) (int, error) {
	if len(raw) < id.Len() {
		return 0, serrors.New("buffer too small", "actual", len(raw), "min", id.Len())
	}
	auxBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(auxBuff, uint64(id.ASID))
	copy(raw, auxBuff[2:8])
	copy(raw[6:], id.Suffix)
	return id.Len(), nil
}

// ToRaw calls Read and returns a new allocated buffer with the ID serialized.
func (id *ID) ToRaw() []byte {
	buf := make([]byte, id.Len())
	id.Read(buf) // safely ignore errors as they can only come from buffer size
	return buf
}

func (id ID) String() string {
	return fmt.Sprintf("%s-%x", id.ASID, id.Suffix)
}

func (id *ID) IsEmptySuffix() bool {
	return bytes.Equal(id.Suffix, []byte{0, 0, 0, 0})
}

func (id *ID) IsEmpty() bool {
	return id.ASID == 0 && id.IsEmptySuffix()
}

type IDs []ID

func (ids IDs) String() string {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	return strings.Join(strs, ",")
}

const SecsPerTick = 4
const DurationPerTick = SecsPerTick * time.Second

const TicksInSegmentRsv = 80
const TicksInE2ERsv = 4

const SegRsvDuration = TicksInSegmentRsv * DurationPerTick // ~ 5m
const E2ERsvDuration = TicksInE2ERsv * DurationPerTick     // 16s

// Tick represents a slice of time of 4 seconds.
type Tick uint32

// TickFromTime returns the tick for a given time.
func TickFromTime(t time.Time) Tick {
	return Tick(util.TimeToSecs(t) / SecsPerTick)
}

// TicksFromDuration returns duration as ticks.
func TicksFromDuration(dur time.Duration) Tick {
	secsDur := (dur + DurationPerTick - 1).Truncate(DurationPerTick)
	seconds := secsDur.Milliseconds() / 1e3
	return Tick(seconds / SecsPerTick)
}

func (t Tick) ToTime() time.Time {
	return util.SecsToTime(uint32(t) * SecsPerTick)
}

func (t Tick) ToDuration() time.Duration {
	return time.Duration(t) * DurationPerTick
}

// BWCls is the bandwidth class. bandwidth = 16 * sqrt(2^(BWCls - 1)). 0 <= bwcls <= 63 kbps.
type BWCls uint8

// BWClsFromBW constructs a BWCls from the bandwidth. Given that
// bandwidth = 16 * sqrt(2^(BWCls - 1))
// where bandwidth is kbps. We then have
// BWCls = 2 * log2( bandwidth/16 ) + 1
// The value of BWCls will be the ceiling of the previous expression.
func BWClsFromBW(bwKbps uint64) BWCls {
	cls := math.Max(0, 2*math.Log2(float64(bwKbps)/16)+1)
	cls = math.Min(cls, 63)
	return BWCls(math.Floor(cls))
}

// Validate will return an error for invalid values.
func (b BWCls) Validate() error {
	if b > 63 {
		return serrors.New("invalid BWClass value", "bw_cls", b)
	}
	return nil
}

// ToKbps returns the kilobits per second this BWCls represents.
func (b BWCls) ToKbps() uint64 {
	if b == 0 {
		return 0
	}
	return uint64(16 * math.Sqrt(math.Pow(2, float64(b)-1)))
}

// MaxBWCls returns the maximum of two BWCls.
func MaxBWCls(a, b BWCls) BWCls {
	if a > b {
		return a
	}
	return b
}

// MinBWCls returns the minimum of two BWCls.
func MinBWCls(a, b BWCls) BWCls {
	if a < b {
		return a
	}
	return b
}

// SplitCls is the traffic split parameter. split = sqrt(2^c). The split divides the bandwidth
// in control traffic (BW * split) and end to end traffic (BW * (1-s)). 0 <= splitCls <= 256 .
type SplitCls uint8

func (s SplitCls) SplitForControl() float64 {
	return math.Sqrt(1. / math.Pow(2., float64(s)))
}

func (s SplitCls) SplitForData() float64 {
	return 1. - s.SplitForControl()
}

// RLC Request Latency Class. latency = 2^rlc miliseconds. 0 <= rlc <= 63
type RLC uint8

// Validate will return an error for invalid values.
func (c RLC) Validate() error {
	if c > 63 {
		return serrors.New("invalid BWClass", "bw_cls", c)
	}
	return nil
}

// IndexNumber is a 4 bit index for a reservation.
type IndexNumber uint8

func NewIndexNumber(value int) IndexNumber {
	return IndexNumber(value % 16)
}

// Validate will return an error for invalid values.
func (i IndexNumber) Validate() error {
	if i >= 1<<4 {
		return serrors.New("invalid IndexNumber", "value", i)
	}
	return nil
}

func (i IndexNumber) Add(other IndexNumber) IndexNumber {
	return (i + other) % 16
}

func (i IndexNumber) Sub(other IndexNumber) IndexNumber {
	return (i - other) % 16
}

// PathType specifies which type of COLIBRI path this segment reservation or request refers to.
type PathType uint8

// the different COLIBRI path types.
const (
	UnknownPath PathType = iota
	CorePath
	DownPath
	UpPath
	PeeringDownPath
	PeeringUpPath
	E2EPath
	_lastvaluePath
)

// Validate will return an error for invalid values.
func (pt PathType) Validate() error {
	if pt == UnknownPath || pt >= _lastvaluePath {
		return serrors.New("invalid path type", "path_type", pt)
	}
	return nil
}

func (pt PathType) String() string {
	switch pt {
	case CorePath:
		return "core"
	case DownPath:
		return "down"
	case UpPath:
		return "up"
	case PeeringDownPath:
		return "peer_down"
	case PeeringUpPath:
		return "peer_up"
	case E2EPath:
		return "e2e"
	default:
		return fmt.Sprintf("unknown path_type %d", pt)
	}
}

func (pt PathType) MarshalJSON() ([]byte, error) {
	return json.Marshal(pt.String())
}

func (pt *PathType) UnmarshalJSON(b []byte) error {
	var text string
	err := json.Unmarshal(b, &text)
	if err != nil {
		return err
	}
	switch text {
	case "core":
		*pt = CorePath
	case "down":
		*pt = DownPath
	case "up":
		*pt = UpPath
	case "peer_down":
		*pt = PeeringDownPath
	case "peer_up":
		*pt = PeeringUpPath
	case "e2e":
		*pt = E2EPath
	default:
		return serrors.New("unknown path_type description", "text", text,
			"bytes", hex.EncodeToString(b))
	}
	return nil
}

// InfoField is used in the reservation token and segment request data.
// 0B       1        2        3        4        5        6        7
// +--------+--------+--------+--------+--------+--------+--------+--------+
// | Expiration time (4B)              |  BwCls | RTT Cls|Idx|Type| padding|
// +--------+--------+--------+--------+--------+--------+--------+--------+
//
// The bandwidth class (BwCls) indicates the reserved bandwidth in an active
// reservation. In a steady request, it indicates the minimal bandwidth class
// reserved so far. In a ephemeral request, it indicates the bandwidth class
// that the source end host is seeking to reserve.
//
// The round trip class (RTT Cls) allows for more granular control in the
// pending request garbage collection.
//
// The reservation index (Idx) is used to allow for multiple overlapping
// reservations within a single path, which enables renewal and changing the
// bandwidth requested.
//
// Type indicates which path type of the reservation.
type InfoField struct {
	ExpirationTick Tick
	Idx            IndexNumber
	BWCls          BWCls
	PathType       PathType
	RLC            RLC
}

// InfoFieldLen is the length in bytes of the InfoField.
const InfoFieldLen = 8

var _ io.Reader = (*InfoField)(nil)

// Validate will return an error for invalid values.
func (f *InfoField) Validate() error {
	if err := f.BWCls.Validate(); err != nil {
		return err
	}
	if err := f.RLC.Validate(); err != nil {
		return err
	}
	if err := f.Idx.Validate(); err != nil {
		return err
	}
	if err := f.PathType.Validate(); err != nil {
		return err
	}

	return nil
}

func (f *InfoField) String() string {
	return fmt.Sprintf("exp.tick: %v, idx: %d, bwcls: %d, pathtype: %v, rlc: %d",
		f.ExpirationTick, f.Idx, f.BWCls, f.PathType, f.RLC)
}

// InfoFieldFromRaw builds an InfoField from the InfoFieldLen bytes buffer.
func InfoFieldFromRaw(raw []byte) (*InfoField, error) {
	if len(raw) < InfoFieldLen {
		return nil, serrors.New("buffer too small", "min_size", InfoFieldLen,
			"current_size", len(raw))
	}
	info := InfoField{
		ExpirationTick: Tick(binary.BigEndian.Uint32(raw[:4])),
		BWCls:          BWCls(raw[4]),
		RLC:            RLC(raw[5]),
		Idx:            IndexNumber(raw[6]) >> 4,
		PathType:       PathType(raw[6]) & 0x7,
	}
	if err := info.Validate(); err != nil {
		return nil, err
	}
	return &info, nil
}

// Read serializes this InfoField into a sequence of InfoFieldLen bytes.
func (f *InfoField) Read(b []byte) (int, error) {
	if len(b) < InfoFieldLen {
		return 0, serrors.New("buffer too small", "min_size", InfoFieldLen,
			"current_size", len(b))
	}
	binary.BigEndian.PutUint32(b[:4], uint32(f.ExpirationTick))
	b[4] = byte(f.BWCls)
	b[5] = byte(f.RLC)
	b[6] = byte(f.Idx<<4) | uint8(f.PathType)
	b[7] = 0 // b[7] is padding
	return InfoFieldLen, nil
}

// ToRaw returns the serial representation of the InfoField.
func (f *InfoField) ToRaw() []byte {
	var buff []byte = nil
	if f != nil {
		buff = make([]byte, InfoFieldLen)
		f.Read(buff) // safely ignore errors as they can only come from buffer size
	}
	return buff
}

// PathEndProps represent the zero or more properties a COLIBRI path can have at both ends.
type PathEndProps uint8

// The only current properties are "Local" (can be used to create e2e rsvs) and "Transfer" (can be
// stiched together with another segment reservation). The first 4 bits encode the properties
// of the "Start" AS, and the last 4 bits encode those of the "End" AS.
const (
	StartLocal    PathEndProps = 0x10
	StartTransfer PathEndProps = 0x20
	EndLocal      PathEndProps = 0x01
	EndTransfer   PathEndProps = 0x02
)

// Validate will return an error for invalid values.
func (pep PathEndProps) Validate() error {
	if pep&0x0F > 0x03 {
		return serrors.New("invalid path end properties (@End)", "path_end_props", pep)
	}
	if pep>>4 > 0x03 {
		return serrors.New("invalid path end properties (@Start)", "path_end_props", pep)
	}
	return nil
}

// ValidateWithPathType checks the validity of the properties when in a path of the specified type.
func (pep PathEndProps) ValidateWithPathType(pt PathType) error {
	if err := pep.Validate(); err != nil {
		return err
	}
	var invalid bool
	s := pep & 0xF0
	e := pep & 0x0F
	switch pt {
	case CorePath:
		if s == 0 {
			invalid = true
		}
	case UpPath:
		if s != StartLocal {
			invalid = true
		}
	case DownPath:
		if e != EndLocal {
			invalid = true
		}
	case PeeringUpPath:
		if s != StartLocal || e == 0 {
			invalid = true
		}
	case PeeringDownPath:
		if e != EndLocal || s == 0 {
			invalid = true
		}
	}
	if invalid {
		return serrors.New("invalid combination of path end properties and path type",
			"end_properties", hex.EncodeToString([]byte{byte(pep)}), "path_type", pt)
	}
	return nil
}

func (pep PathEndProps) StartLocal() bool {
	return pep&StartLocal != 0
}

func (pep PathEndProps) StartTransfer() bool {
	return pep&StartTransfer != 0
}

func (pep PathEndProps) EndLocal() bool {
	return pep&EndLocal != 0
}

func (pep PathEndProps) EndTransfer() bool {
	return pep&EndTransfer != 0
}

func NewPathEndProps(startLocal, startTransfer, endLocal, endTransfer bool) PathEndProps {
	var props PathEndProps
	if startLocal {
		props |= StartLocal
	}
	if startTransfer {
		props |= StartTransfer
	}
	if endLocal {
		props |= EndLocal
	}
	if endTransfer {
		props |= EndTransfer
	}
	return props
}

// AllocationBead represents an allocation resolved in an AS for a given reservation.
// It is used in an array to represent the allocation trail that happened for a reservation.
type AllocationBead struct {
	AllocBW BWCls
	MaxBW   BWCls
}

type AllocationBeads []AllocationBead

// MinMax returns the minimum of all the max BW in the AllocationBeads.
func (bs AllocationBeads) MinMax() BWCls {
	if len(bs) == 0 {
		return 0
	}
	var min BWCls = math.MaxUint8
	for _, b := range bs {
		if b.MaxBW < min {
			min = b.MaxBW
		}
	}
	return min
}

// HopField is a COLIBRI HopField.
// TODO(juagargi) move to slayers
type HopField struct {
	Ingress uint16
	Egress  uint16
	Mac     [4]byte
}

const HopFieldLen = 8

var _ io.Reader = (*HopField)(nil)

// HopFieldFromRaw builds a HopField from a raw buffer.
func HopFieldFromRaw(raw []byte) (*HopField, error) {
	if len(raw) < HopFieldLen {
		return nil, serrors.New("buffer too small for HopField", "min_size", HopFieldLen,
			"current_size", len(raw))
	}
	hf := HopField{
		Ingress: binary.BigEndian.Uint16(raw[:2]),
		Egress:  binary.BigEndian.Uint16(raw[2:4]),
	}
	copy(hf.Mac[:], raw[4:8])
	return &hf, nil
}

func (hf *HopField) String() string {
	return fmt.Sprintf("%d>%d [%x]", hf.Ingress, hf.Egress, hf.Mac)
}

// Read serializes this HopField into the buffer.
func (hf *HopField) Read(b []byte) (int, error) {
	if len(b) < HopFieldLen {
		return 0, serrors.New("buffer too small for HopField", "min_size", HopFieldLen,
			"current_size", len(b))
	}
	binary.BigEndian.PutUint16(b[:2], hf.Ingress)
	binary.BigEndian.PutUint16(b[2:4], hf.Egress)
	copy(b[4:], hf.Mac[:])
	return HopFieldLen, nil
}

// ToRaw returns the serial representation of the HopField.
func (hf *HopField) ToRaw() []byte {
	buff := make([]byte, HopFieldLen)
	hf.Read(buff) // discard returned values
	return buff
}

// Token is used in the data plane to forward COLIBRI packets.
type Token struct {
	InfoField
	HopFields []HopField
}

var _ io.Reader = (*Token)(nil)

// Validate will return an error for invalid values. It will not check the hop fields' validity.
func (t *Token) Validate() error {
	if len(t.HopFields) == 0 {
		return serrors.New("token without hop fields")
	}
	return t.InfoField.Validate()
}

func (t *Token) String() string {
	hfs := make([]string, len(t.HopFields))
	for i, hf := range t.HopFields {
		hfs[i] = hf.String()
	}
	return t.InfoField.String() + ", Hops: " + strings.Join(hfs, " , ")
}

// TokenFromRaw builds a Token from the passed bytes buffer.
func TokenFromRaw(raw []byte) (*Token, error) {
	if raw == nil {
		return nil, nil
	}
	rawHFs := len(raw) - InfoFieldLen
	if rawHFs < 0 || rawHFs%HopFieldLen != 0 {
		return nil, serrors.New("buffer too small for Token", "min_size", InfoFieldLen,
			"current_size", len(raw))
	}
	numHFs := rawHFs / HopFieldLen
	inf, err := InfoFieldFromRaw(raw[:InfoFieldLen])
	if err != nil {
		return nil, err
	}
	t := Token{
		InfoField: *inf,
	}
	if numHFs > 0 {
		t.HopFields = make([]HopField, numHFs)
	}
	for i := 0; i < numHFs; i++ {
		offset := InfoFieldLen + i*HopFieldLen
		hf, err := HopFieldFromRaw(raw[offset : offset+HopFieldLen])
		if err != nil {
			return nil, err
		}
		t.HopFields[i] = *hf
	}
	return &t, nil
}

// Len returns the number of bytes of this token if serialized.
func (t *Token) Len() int {
	if t == nil {
		return 0
	}
	return InfoFieldLen + len(t.HopFields)*HopFieldLen
}

// Read serializes this Token to the passed buffer.
func (t *Token) Read(b []byte) (int, error) {
	length := t.Len()
	if len(b) < length {
		return 0, serrors.New("buffer too small", "min_size", length, "current_size", len(b))
	}
	offset, err := t.InfoField.Read(b[:InfoFieldLen])
	if err != nil {
		return 0, err
	}
	for i := 0; i < len(t.HopFields); i++ {
		t.HopFields[i].Read(b[offset : offset+HopFieldLen])
		offset += HopFieldLen
	}
	return offset, nil
}

// ToRaw returns the serial representation of the Token.
func (t *Token) ToRaw() []byte {
	var buff []byte = nil
	if t != nil {
		buff = make([]byte, t.Len())
		t.Read(buff) // safely ignore errors as they can only come from buffer size
	}
	return buff
}

// AddNewHopField adds a new hopfield to the token. The added token is prepended to the list
// of existing tokens, so that it is at the beginning of the list.
// The function returns a pointer to the new (copied) hop field inside the token.
func (t *Token) AddNewHopField(hf *HopField) *HopField {
	t.HopFields = append([]HopField{*hf}, t.HopFields...)
	hf = &t.HopFields[0]
	return hf
}

// GetFirstNHopFields returns the n first (*in order of addition*) hop fields of the token.
// Depending on the path type (up, down, etc) thoese will be located at the beginning, or at the
// end of the hop field list in the token.
// If the existing hop fields are less than n, only those are returned.
func (t *Token) GetFirstNHopFields(n int) []HopField {
	if t == nil || len(t.HopFields) == 0 || n <= 0 {
		return nil
	}
	var begin, end int
	n = min(n, len(t.HopFields))
	switch t.InfoField.PathType {
	case DownPath:
		begin, end = 0, n
	case UpPath, CorePath, E2EPath:
		begin, end = len(t.HopFields)-n, len(t.HopFields)
	default:
		panic(fmt.Sprintf("unknown path type %v", t.InfoField.PathType))
	}
	return t.HopFields[begin:end]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
