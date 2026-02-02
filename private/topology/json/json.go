// Copyright 2019 ETH Zurich
// Copyright 2020 ETH Zurich, Anapaya Systems
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

// Package json encodes AS topology information via JSON. All types exposed by this package are
// designed to be directly marshaled to / unmarshaled from JSON.
package json

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/scionproto/scion/pkg/log"
	"github.com/scionproto/scion/pkg/private/serrors"
	"github.com/scionproto/scion/pkg/private/util"
	"github.com/scionproto/scion/pkg/segment/iface"
)

// Attribute indicates the capability of a primary AS.
type Attribute string

const (
	// AttrCore indicates a core AS.
	AttrCore Attribute = "core"
)

// UnmarshalText checks that the attribute is valid. It can only be "core";
// Deprecated values "authoritative", "issuing", or "voting" are ignored.
func (t *Attribute) UnmarshalText(b []byte) error {
	switch Attribute(b) {
	case AttrCore:
		*t = AttrCore
	case "authoritative", "issuing", "voting":
		// ignore
		log.Info("topology.json; ignoring deprecated attribute value", "value", string(b))
	default:
		return serrors.New("invalid attribute", "input", string(b))
	}
	return nil
}

type Attributes []Attribute

func (as *Attributes) UnmarshalJSON(b []byte) error {
	var attrs []Attribute
	if err := json.Unmarshal(b, &attrs); err != nil {
		return err
	}
	var filtered Attributes
	for _, a := range attrs {
		if a != "" { // drop ignored deprecated values
			filtered = append(filtered, a)
		}
	}
	*as = filtered
	return nil
}

// PrivateISD describes the membership of an AS in a private ISD, including optional role overrides.
type PrivateISD struct {
	ISD           uint16 `json:"isd"`
	Core          bool   `json:"core,omitempty"`
	Issuing       bool   `json:"issuing,omitempty"`
	Voting        bool   `json:"voting,omitempty"`
	Authoritative bool   `json:"authoritative,omitempty"`
	CertIssuer    string `json:"cert_issuer,omitempty"`
}

func (p *PrivateISD) UnmarshalJSON(b []byte) error {
	var number uint16
	if err := json.Unmarshal(b, &number); err == nil {
		if number == 0 {
			return serrors.New("private ISD must be non-zero")
		}
		p.ISD = number
		p.Core = false
		p.Issuing = false
		p.Voting = false
		p.Authoritative = false
		p.CertIssuer = ""
		return nil
	}
	var aux struct {
		ISD           uint16 `json:"isd"`
		Core          bool   `json:"core"`
		Issuing       bool   `json:"issuing"`
		Voting        bool   `json:"voting"`
		Authoritative bool   `json:"authoritative"`
		CertIssuer    string `json:"cert_issuer"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	if aux.ISD == 0 {
		return serrors.New("private ISD entry missing ISD")
	}
	p.ISD = aux.ISD
	p.Core = aux.Core
	p.Issuing = aux.Issuing
	p.Voting = aux.Voting
	p.Authoritative = aux.Authoritative
	p.CertIssuer = aux.CertIssuer
	return nil
}

// Topology is the JSON type for the entire AS topology file.
type Topology struct {
	Timestamp          int64             `json:"timestamp,omitempty"`
	TimestampHuman     string            `json:"timestamp_human,omitempty"`
	IA                 string            `json:"isd_as"`
	MTU                int               `json:"mtu"`
	EndhostPortRange   string            `json:"dispatched_ports"`
	PrivateOnlyAS      bool              `json:"private_only_as,omitempty"`
	LocalIAs           []string          `json:"local_ias,omitempty"`
	MembershipBaseDirs map[string]string `json:"membership_base_dirs,omitempty"`
	// Attributes specify whether this is a core AS or not.
	Attributes Attributes `json:"attributes"`
	// PrivateISDs lists the private ISDs the AS participates in with optional role overrides.
	PrivateISDs         []PrivateISD            `json:"private_isds,omitempty"`
	BorderRouters       map[string]*BRInfo      `json:"border_routers,omitempty"`
	ControlService      map[string]*ServerInfo  `json:"control_service,omitempty"`
	DiscoveryService    map[string]*ServerInfo  `json:"discovery_service,omitempty"`
	HiddenSegmentLookup map[string]*ServerInfo  `json:"hidden_segment_lookup_service,omitempty"`
	HiddenSegmentReg    map[string]*ServerInfo  `json:"hidden_segment_registration_service,omitempty"`
	SIG                 map[string]*GatewayInfo `json:"sigs,omitempty"`
}

// ServerInfo contains the information for a SCION application running in the local AS.
type ServerInfo struct {
	Addr string `json:"addr"`
}

// BRInfo contains Border Router specific information.
type BRInfo struct {
	InternalAddr string                    `json:"internal_addr"`
	Interfaces   map[iface.ID]*BRInterface `json:"interfaces"`
}

// GatewayInfo contains SCION gateway information.
type GatewayInfo struct {
	CtrlAddr   string   `json:"ctrl_addr"`
	DataAddr   string   `json:"data_addr"`
	ProbeAddr  string   `json:"probe_addr,omitempty"`
	Interfaces []uint64 `json:"allow_interfaces,omitempty"`
}

// BRInterface contains the information for an data-plane BR socket that is external (i.e., facing
// the neighboring AS).
type BRInterface struct {
	Underlay   Underlay `json:"underlay,omitempty"`
	IA         string   `json:"isd_as"`
	LinkTo     string   `json:"link_to"`
	MTU        int      `json:"mtu"`
	BFD        *BFD     `json:"bfd,omitempty"`
	RemoteIfID iface.ID `json:"remote_interface_id,omitempty"`
	// PrivateISDs lists the private ISDs shared with the remote interface.
	PrivateISDs []uint16 `json:"private_isds,omitempty"`
	// PrivateOnly restricts this link to private memberships only; public traffic is not allowed.
	PrivateOnly bool `json:"private_only,omitempty"`
	// AllowedPrivate restricts which private ISDs may traverse this link. Empty means any private.
	AllowedPrivate []uint16 `json:"allowed_private,omitempty"`
}

// Underlay is the underlay information for a BR interface.
type Underlay struct {
	Provider string `json:"provider,omitempty"`
	Local    string `json:"local,omitempty"`
	Remote   string `json:"remote,omitempty"`
}

// BFD configuration.
type BFD struct {
	Disable               *bool        `json:"disable,omitempty"`
	DetectMult            uint8        `json:"detect_mult,omitempty"`
	DesiredMinTxInterval  util.DurWrap `json:"desired_min_tx_interval,omitempty"`
	RequiredMinRxInterval util.DurWrap `json:"required_min_rx_interval,omitempty"`
}

func (i ServerInfo) String() string {
	return fmt.Sprintf("Addr: %s", i.Addr)
}

func (i BRInfo) String() string {
	var s []string
	s = append(s, fmt.Sprintf("Loc addrs:\n  %s\nInterfaces:", i.InternalAddr))
	for ifID, intf := range i.Interfaces {
		s = append(s, fmt.Sprintf("%d: %+v", ifID, intf))
	}
	return strings.Join(s, "\n")
}

// Load parses a topology from its raw byte representation.
func Load(b []byte) (*Topology, error) {
	rt := &Topology{}
	if err := json.Unmarshal(b, rt); err != nil {
		return nil, serrors.Wrap("unable to parse topology from JSON", err)
	}
	return rt, nil
}

// LoadFromFile parses a topology from a file.
func LoadFromFile(path string) (*Topology, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, serrors.Wrap("unable to open topology", err, "path", path)
	}
	return Load(b)
}
