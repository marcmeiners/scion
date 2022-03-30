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

package coliquic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvariantColibriRepresentation(t *testing.T) {

	colPath := newTestColibriPath()
	copy(colPath.PacketTimestamp[:], []byte{0, 1, 2, 3, 4, 5, 6, 7})
	colPath.InfoField.OrigPayLen = 12345
	buff := make([]byte, colPath.Len())
	err := colPath.SerializeTo(buff)
	require.NoError(t, err)

	// representation of the original
	repr1 := invariantColibri(buff)

	// modify timestamp and orig payload length and obtain the representation again
	copy(colPath.PacketTimestamp[:], []byte{10, 11, 12, 13, 14, 15, 16, 17})
	colPath.InfoField.OrigPayLen = 11111
	err = colPath.SerializeTo(buff)
	require.NoError(t, err)
	repr2 := invariantColibri(buff)

	require.Equal(t, repr1, repr2)
}
