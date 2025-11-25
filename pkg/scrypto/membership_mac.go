// Copyright 2025 ETH Zurich
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

package scrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"hash"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/private/serrors"
)

// MembershipKeyDerivation provides per-ISD-membership forwarding key derivation.
//
// Private ISD memberships require separate forwarding keys to prevent segment
// substitution attacks. An attacker with segments from one ISD membership
// should not be able to use them in another membership's paths.
//
// We derive per-membership keys using AES-based key derivation:
//   derivedKey = AES-Encrypt(masterKey, ISD_Number (2 bytes) || Padding (14 bytes))
//
// This provides:
// 1. Key Separation: Each ISD gets a cryptographically independent key
// 2. One-way Derivation: Cannot recover master key from derived keys
// 3. Deterministic: Same ISD always gets the same derived key
// 4. Attack Resistance: Segments signed with different keys cannot be mixed

type MembershipKeyDerivation struct {
	// base forwarding key loaded from keys/master0.key
	masterKey []byte
	// AES cipher for key derivation
	cipher cipher.Block
}

// creates a key derivation instance from a master key.
func NewMembershipKeyDerivation(masterKey []byte) (*MembershipKeyDerivation, error) {
	if len(masterKey) != 16 {
		return nil, serrors.New("master key must be 16 bytes", "len", len(masterKey))
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, serrors.Wrap("creating AES cipher for key derivation", err)
	}

	return &MembershipKeyDerivation{
		masterKey: masterKey,
		cipher:    block,
	}, nil
}

// derives a per-membership forwarding key for the given ISD.
func (m *MembershipKeyDerivation) DeriveKey(isd addr.ISD) ([]byte, error) {
	input := make([]byte, 16)
	binary.BigEndian.PutUint16(input[0:2], uint16(isd))
	derivedKey := make([]byte, 16)
	m.cipher.Encrypt(derivedKey, input)
	return derivedKey, nil
}

// creates a MAC factory function for a specific ISD membership
func (m *MembershipKeyDerivation) MACFactory(isd addr.ISD) (func() hash.Hash, error) {
	derivedKey, err := m.DeriveKey(isd)
	if err != nil {
		return nil, serrors.Wrap("deriving key for ISD", err, "isd", isd)
	}
	return HFMacFactory(derivedKey)
}
