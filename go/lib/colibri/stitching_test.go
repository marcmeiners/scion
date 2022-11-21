// Copyright 2021 ETH Zurich
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

// Package colibri contains methods for the creation and verification of the colibri packet
// timestamp and validation fields.
package colibri_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	base "github.com/scionproto/scion/go/co/reservation"
	te "github.com/scionproto/scion/go/co/reservation/test"
	"github.com/scionproto/scion/go/lib/colibri"
	ct "github.com/scionproto/scion/go/lib/colibri/coltest"
	"github.com/scionproto/scion/go/lib/colibri/reservation"
)

func TestShortcutSteps(t *testing.T) {
	cases := map[string]struct {
		segments []base.PathSteps
		expected base.PathSteps
	}{
		"empty": {
			segments: []base.PathSteps{},
			expected: te.NewSteps(),
		},
		"longlegs1": {
			segments: []base.PathSteps{
				te.NewSteps(0, "1-ff00:0:113", 1, 2, "1-ff00:0:111", 41, 1, "1-ff00:0:110", 0),
				te.NewSteps(0, "1-ff00:0:110", 1, 41, "1-ff00:0:111", 0),
			},
			expected: te.NewSteps(0, "1-ff00:0:113", 1, 2, "1-ff00:0:111", 0),
		},
		"longlegs2": {
			segments: []base.PathSteps{
				te.NewSteps(0, "1-ff00:0:111", 41, 1, "1-ff00:0:110", 0),
				te.NewSteps(0, "1-ff00:0:110", 1, 41, "1-ff00:0:111", 1, 1, "1-ff00:0:113", 0),
			},
			expected: te.NewSteps(0, "1-ff00:0:111", 1, 1, "1-ff00:0:113", 0),
		},
		"abc_cbd": {
			segments: []base.PathSteps{
				te.NewSteps(0, "1-1", 1, 2, "1-2", 3, 4, "1-3", 0),
				te.NewSteps(0, "1-3", 4, 3, "1-2", 5, 6, "1-4", 0),
			},
			expected: te.NewSteps(0, "1-1", 1, 2, "1-2", 5, 6, "1-4", 0),
		},
		"one_segment": {
			segments: []base.PathSteps{
				te.NewSteps(0, "1-1", 1, 2, "1-2", 3, 4, "1-3", 0),
			},
			expected: te.NewSteps(0, "1-1", 1, 2, "1-2", 3, 4, "1-3", 0),
		},
		"short_segments": {
			segments: []base.PathSteps{
				te.NewSteps(0, "1-1", 1, 2, "1-2", 0),
				te.NewSteps(0, "1-2", 3, 4, "1-3", 0),
			},
			expected: te.NewSteps(0, "1-1", 1, 2, "1-2", 3, 4, "1-3", 0),
		},
		"no_duplicates": { // abc_cde_efg
			segments: []base.PathSteps{
				te.NewSteps(0, "1-1", 1, 2, "1-2", 3, 4, "1-3", 0),
				te.NewSteps(0, "1-3", 5, 6, "1-4", 7, 8, "1-5", 0),
				te.NewSteps(0, "1-5", 9, 10, "1-6", 11, 12, "1-7", 0),
			},
			// abcdefg
			expected: te.NewSteps(0, "1-1", 1, 2, "1-2", 3, 4, "1-3", 5, 6, "1-4", 7,
				8, "1-5", 9, 10, "1-6", 11, 12, "1-7", 0),
		},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ft := make(colibri.FullTrip, len(tc.segments))
			for i := range ft {
				ft[i] = &colibri.SegRDetails{
					Steps: tc.segments[i],
				}
			}
			got := ft.ShortcutSteps()
			require.Equal(t, tc.expected, got, "expected != got\n%s\n%s", tc.expected, got)
		})
	}
}

func TestCombineAll(t *testing.T) {
	cases := map[string]struct {
		stitchable *colibri.StitchableSegments
		expected   []*colibri.FullTrip
	}{
		"empty": {
			stitchable: nil,
			expected:   nil,
		},
		"tiny_topo": {
			stitchable: ct.NewStitchableSegments("1-ff00:0:111", "1-ff00:0:112",
				ct.WithCoreASes("1-ff00:0:110"),
				ct.WithUpSegs(2),   // src to core1
				ct.WithDownSegs(2), // from core1 to dst
			),
			expected: ct.NewFullTrips("1-ff00:0:111", "1-ff00:0:112",
				ct.WithCoresInTrip("1-ff00:0:110"),
				ct.WithTrips(ct.T(ct.U(0, 2), ct.D(2, 1))),
			),
		},
		"from_core": {
			stitchable: ct.NewStitchableSegments("1-ff00:0:110", "1-ff00:0:112",
				ct.WithDownSegs(0), // core1 to src
			),
			expected: ct.NewFullTrips("1-ff00:0:110", "1-ff00:0:112",
				ct.WithTrips(ct.T(ct.D(0, 1))),
			),
		},
		"core_to_core_then_down": {
			stitchable: ct.NewStitchableSegments("1-ff00:0:110", "1-ff00:0:211",
				ct.WithCoreASes("1-ff00:0:210"),
				ct.WithCoreSegs(ct.P(0, 2)), // src -> core2
				ct.WithDownSegs(2),          // core2 -> dst
			),
			expected: ct.NewFullTrips("1-ff00:0:110", "1-ff00:0:211",
				ct.WithCoresInTrip("1-ff00:0:210"),
				ct.WithTrips(ct.T(ct.C(0, 2), ct.D(2, 1))),
			),
		},
		"core_to_core": {
			stitchable: ct.NewStitchableSegments("1-ff00:0:110", "1-ff00:0:210",
				ct.WithCoreSegs(ct.P(0, 1)), // src -> dst
			),
			expected: ct.NewFullTrips("1-ff00:0:110", "1-ff00:0:210",
				ct.WithTrips(ct.T(ct.C(0, 1))),
			),
		},
		"core_to_far_core": {
			stitchable: ct.NewStitchableSegments("1-ff00:0:110", "2-ff00:0:210",
				ct.WithCoreSegs(ct.P(0, 1)), // src -> dst
			),
			expected: ct.NewFullTrips("1-ff00:0:110", "2-ff00:0:210",
				ct.WithTrips(ct.T(ct.C(0, 1))),
			),
		},
		"core_to_far_core_then_down": {
			stitchable: ct.NewStitchableSegments("1-ff00:0:110", "2-ff00:0:211",
				ct.WithCoreASes("2-ff00:0:210"),
				ct.WithCoreSegs(ct.P(0, 2)), // src -> core2
				ct.WithDownSegs(2),          // core2 -> dst
			),
			expected: ct.NewFullTrips("1-ff00:0:110", "2-ff00:0:211",
				ct.WithCoresInTrip("2-ff00:0:210"),
				ct.WithTrips(ct.T(ct.C(0, 2), ct.D(2, 1))),
			),
		},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			t.Log("---------------------------- stitchable:")
			t.Log(tc.stitchable)
			t.Log("---------------------------- expected:")
			t.Log(tc.expected)
			t.Log("---------------------------- actual:")
			actual := colibri.CombineAll(tc.stitchable)
			t.Log(actual)
			t.Log("---------------------------------------------")
			require.Equal(t, tc.expected, actual)
		})
	}
	require.NoError(t, nil)
}

func TestBW(t *testing.T) {
	// XXX(juagargi): test expects to find only one full trip after the full combination
	cases := map[string]struct {
		stitchables *colibri.StitchableSegments
		expectedBW  uint64
	}{
		"all_the_same": {
			stitchables: ct.NewStitchableSegments("1-ff00:0:111", "1-ff00:0:112",
				ct.WithUpSegs(1),
				ct.WithBW(ct.Up, 0, ct.Allocbw, 13),
				ct.WithSplit(ct.Up, 0, 7),
			),
			expectedBW: bw(13, 7),
		},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			trips := colibri.CombineAll(tc.stitchables)
			require.Len(t, trips, 1) // expect to always find one and only one trip in this test

			require.Equal(t, tc.expectedBW, trips[0].BW())
		})
	}
}

func bw(bwCls reservation.BWCls, splitCls reservation.SplitCls) uint64 {
	return uint64(float64(bwCls.ToKbps()) * splitCls.SplitForData())
}
