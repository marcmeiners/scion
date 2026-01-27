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

package testcrypto

import (
	"os"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/private/serrors"
)

// topo contains the relevant part of the topo file for the testcrypto command.
type topo struct {
	ASes map[addr.IA]struct {
		CA            addr.IA                `yaml:"cert_issuer"`
		Authoritative bool                   `yaml:"authoritative"`
		Core          bool                   `yaml:"core"`
		Issuing       bool                   `yaml:"issuing"`
		Voting        bool                   `yaml:"voting"`
		PrivateOnlyAS bool                   `yaml:"private_only_as"`
		PrivateISDs   []PrivateISDMembership `yaml:"private_isds"`
	} `yaml:"ASes"`
}

func (t topo) baseAttrs(ia addr.IA) PrivateISDMembershipAttrs {
	v := t.ASes[ia]
	return PrivateISDMembershipAttrs{
		Core:          v.Core,
		Issuing:       v.Issuing,
		Voting:        v.Voting,
		Authoritative: v.Authoritative,
	}
}

// PrivateISDMembership describes membership in a private ISD including role overrides.
type PrivateISDMembership struct {
	ISD           addr.ISD
	Core          bool
	Issuing       bool
	Voting        bool
	Authoritative bool
	CertIssuer    addr.IA
}

func (m *PrivateISDMembership) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var raw uint16
		if err := value.Decode(&raw); err != nil {
			return serrors.Wrap("decoding private ISD", err)
		}
		m.ISD = addr.ISD(raw)
		m.Core = false
		m.Issuing = false
		m.Voting = false
		m.Authoritative = false
	case yaml.MappingNode:
		var aux struct {
			ISD           addr.ISD `yaml:"isd"`
			Core          bool     `yaml:"core"`
			Issuing       bool     `yaml:"issuing"`
			Voting        bool     `yaml:"voting"`
			Authoritative bool     `yaml:"authoritative"`
			CertIssuer    string   `yaml:"cert_issuer"`
		}
		if err := value.Decode(&aux); err != nil {
			return serrors.Wrap("decoding private ISD mapping", err)
		}
		if aux.ISD == 0 {
			return serrors.New("missing ISD in private ISD entry")
		}
		m.ISD = aux.ISD
		m.Core = aux.Core
		m.Issuing = aux.Issuing
		m.Voting = aux.Voting
		m.Authoritative = aux.Authoritative
		if aux.CertIssuer != "" {
			ia, err := addr.ParseIA(aux.CertIssuer)
			if err != nil {
				return serrors.Wrap("parsing private cert issuer", err)
			}
			m.CertIssuer = ia
		}
	default:
		return serrors.New("invalid private ISD entry", "kind", value.Kind)
	}
	return nil
}

// loadTopo loads the topo from file.
func loadTopo(file string) (topo, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return topo{}, serrors.Wrap("failed to load topofile", err, "file", file)
	}
	var t topo
	if err := yaml.Unmarshal(raw, &t); err != nil {
		return topo{}, serrors.Wrap("failed to parse topofile", err, "file", file)
	}
	for ia, v := range t.ASes {
		if v.Issuing {
			// If an AS is issuer, it issues for itself.
			v.CA = ia
		}
		sanitized, err := sanitizePrivateISDs(v.PrivateISDs)
		if err != nil {
			return topo{}, err
		}
		v.PrivateISDs = sanitized
		t.ASes[ia] = v
	}
	return t, nil
}

func sanitizePrivateISDs(values []PrivateISDMembership) ([]PrivateISDMembership, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[addr.ISD]PrivateISDMembership, len(values))
	for _, v := range values {
		if v.ISD < 16 || v.ISD > 63 {
			return nil, serrors.New("private ISD outside permitted range", "isd", v.ISD)
		}
		existing, ok := seen[v.ISD]
		if ok {
			existing.Core = existing.Core || v.Core
			existing.Issuing = existing.Issuing || v.Issuing
			existing.Voting = existing.Voting || v.Voting
			existing.Authoritative = existing.Authoritative || v.Authoritative
			if existing.CertIssuer.IsZero() && !v.CertIssuer.IsZero() {
				existing.CertIssuer = v.CertIssuer
			}
			seen[v.ISD] = existing
			continue
		}
		seen[v.ISD] = v
	}
	result := make([]PrivateISDMembership, 0, len(seen))
	for _, v := range seen {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ISD < result[j].ISD })
	return result, nil
}
