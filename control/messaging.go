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

package control

import (
	"hash"
	"path/filepath"

	"github.com/scionproto/scion/pkg/private/serrors"
	"github.com/scionproto/scion/pkg/scrypto"
	"github.com/scionproto/scion/private/keyconf"
)

// MACGenFactory creates a MAC factory for the public/default membership
func MACGenFactory(configDir string) (func() hash.Hash, error) {
	mk, err := keyconf.LoadMaster(filepath.Join(configDir, "keys"))
	if err != nil {
		return nil, serrors.Wrap("loading master key", err)
	}
	hfMacFactory, err := scrypto.HFMacFactory(mk.Key0)
	if err != nil {
		return nil, err
	}
	return hfMacFactory, nil
}

// creates a key derivation instance that can generate per-membership MAC factories
// This is used to derive separate forwarding keys for each private ISD membership
func MembershipMACGenFactory(configDir string) (*scrypto.MembershipKeyDerivation, error) {
	mk, err := keyconf.LoadMaster(filepath.Join(configDir, "keys"))
	if err != nil {
		return nil, serrors.Wrap("loading master key", err)
	}
	keyDerivation, err := scrypto.NewMembershipKeyDerivation(mk.Key0)
	if err != nil {
		return nil, serrors.Wrap("creating membership key derivation", err)
	}
	return keyDerivation, nil
}
