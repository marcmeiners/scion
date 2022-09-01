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

package reservationstore

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/scionproto/scion/go/co/reservation/segment"
	seg "github.com/scionproto/scion/go/co/reservation/segment"
	st "github.com/scionproto/scion/go/co/reservation/segmenttest"
	mockmanager "github.com/scionproto/scion/go/co/reservationstore/mock_reservationstore"
	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/colibri/reservation"
	"github.com/scionproto/scion/go/lib/pathpol"
	"github.com/scionproto/scion/go/lib/util"
	"github.com/scionproto/scion/go/lib/xtest"
)

// func TestKeepOneShotDeleteme(t *testing.T) {
// 	now := util.SecsToTime(10)
// 	tomorrow := now.AddDate(0, 0, 1)
// 	endProps1 := reservation.StartLocal | reservation.EndLocal | reservation.EndTransfer
// 	cases := map[string]struct {
// 		configured            []requirements
// 		reservations          []*segment.Reservation
// 		paths                 map[addr.IA][]snet.Path
// 		expectedRequestsCalls int
// 		expectedWakeupTime    time.Time
// 	}{
// 		"regular": {
// 			configured: []requirements{
// 				{
// 					dst:           xtest.MustParseIA("1-ff00:0:2"),
// 					predicate:     newSequence(t, "1-ff00:0:1 1-ff00:0:2"), // direct
// 					minBW:         10,
// 					maxBW:         42,
// 					splitCls:      2,
// 					endProps:      endProps1,
// 					minActiveRsvs: 1,
// 				},
// 				{
// 					dst:           xtest.MustParseIA("1-ff00:0:2"),
// 					predicate:     newSequence(t, "1-ff00:0:1 0+ 1-ff00:0:2"), // not direct
// 					minBW:         10,
// 					maxBW:         42,
// 					splitCls:      2,
// 					endProps:      endProps1,
// 					minActiveRsvs: 1,
// 				},
// 				{
// 					dst:           xtest.MustParseIA("1-ff00:0:2"),
// 					predicate:     newSequence(t, "1-ff00:0:1 1-ff00:0:3"), // direct
// 					minBW:         10,
// 					maxBW:         42,
// 					splitCls:      2,
// 					endProps:      endProps1,
// 					minActiveRsvs: 1,
// 				},
// 				{
// 					dst:           xtest.MustParseIA("1-ff00:0:2"),
// 					predicate:     newSequence(t, "1-ff00:0:1 0+ 1-ff00:0:3"), // not direct
// 					minBW:         10,
// 					maxBW:         42,
// 					splitCls:      2,
// 					endProps:      endProps1,
// 					minActiveRsvs: 1,
// 				},
// 			},
// 			paths: map[addr.IA][]snet.Path{
// 				xtest.MustParseIA("1-ff00:0:2"): {
// 					te.NewSnetPath("1-ff00:0:1", 1, 2, "1-ff00:0:2"), // direct
// 					te.NewSnetPath("1-ff00:0:1", 2, 3, "1-ff00:0:2"), // direct
// 					te.NewSnetPath("1-ff00:0:1", 3, 88, "1-ff00:0:88", 99, 4, "1-ff00:0:2"),
// 				},
// 				xtest.MustParseIA("1-ff00:0:3"): {
// 					te.NewSnetPath("1-ff00:0:1", 1, 2, "1-ff00:0:3"), // direct
// 					te.NewSnetPath("1-ff00:0:1", 2, 3, "1-ff00:0:3"), // direct
// 					te.NewSnetPath("1-ff00:0:1", 3, 88, "1-ff00:0:88", 99, 4, "1-ff00:0:3"),
// 				},
// 			},
// 			// reservations: map[addr.IA][]*segment.Reservation{
// 			// 	xtest.MustParseIA("1-ff00:0:2"): modOneRsv(
// 			// 		st.NewRsvs(2, st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
// 			// 			st.AddIndex(0, st.WithBW(12, 42, 0),
// 			// 				st.WithExpiration(tomorrow)),
// 			// 			st.AddIndex(1, st.WithBW(12, 24, 0),
// 			// 				st.WithExpiration(tomorrow.Add(24*time.Hour))),
// 			// 			st.ConfirmAllIndices(),
// 			// 			st.WithActiveIndex(0),
// 			// 			st.WithTrafficSplit(2),
// 			// 			st.WithEndProps(endProps1)),
// 			// 		0, st.ModIndex(0, st.WithBW(3, 0, 0))), // change rsv 0 to could_be_compliant
// 			// 	xtest.MustParseIA("1-ff00:0:3"): modOneRsv(
// 			// 		st.NewRsvs(2, st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:3"),
// 			// 			st.AddIndex(0, st.WithBW(12, 42, 0),
// 			// 				st.WithExpiration(tomorrow)),
// 			// 			st.AddIndex(1, st.WithBW(12, 24, 0),
// 			// 				st.WithExpiration(tomorrow.Add(24*time.Hour))),
// 			// 			st.ConfirmAllIndices(),
// 			// 			st.WithActiveIndex(0),
// 			// 			st.WithTrafficSplit(2),
// 			// 			st.WithEndProps(endProps1)),
// 			// 		0, st.ModIndex(0, st.WithBW(3, 0, 0))), // change rsv 0 to could_be_compliant
// 			// },
// 			reservations: append(
// 				modOneRsv(
// 					st.NewRsvs(2, st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
// 						st.AddIndex(0, st.WithBW(12, 42, 0),
// 							st.WithExpiration(tomorrow)),
// 						st.AddIndex(1, st.WithBW(12, 24, 0),
// 							st.WithExpiration(tomorrow.Add(24*time.Hour))),
// 						st.ConfirmAllIndices(),
// 						st.WithActiveIndex(0),
// 						st.WithTrafficSplit(2),
// 						st.WithEndProps(endProps1)),
// 					0, st.ModIndex(0, st.WithBW(3, 0, 0))), // change rsv 0 to could_be_compliant
// 				modOneRsv(
// 					st.NewRsvs(2, st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:3"),
// 						st.AddIndex(0, st.WithBW(12, 42, 0),
// 							st.WithExpiration(tomorrow)),
// 						st.AddIndex(1, st.WithBW(12, 24, 0),
// 							st.WithExpiration(tomorrow.Add(24*time.Hour))),
// 						st.ConfirmAllIndices(),
// 						st.WithActiveIndex(0),
// 						st.WithTrafficSplit(2),
// 						st.WithEndProps(endProps1)),
// 					0, st.ModIndex(0, st.WithBW(3, 0, 0)))..., // change rsv 0 to could_be_compliant
// 			),
// 			expectedRequestsCalls: 2,
// 			expectedWakeupTime:    now.Add(sleepAtMost),
// 		},
// 		"all compliant expiring tomorrow": {
// 			configured: map[addr.IA][]requirementsDeleteme{
// 				xtest.MustParseIA("1-ff00:0:2"): {{
// 					predicate:     newSequence(t, "1-ff00:0:1 1-ff00:0:2"), // direct
// 					minBW:         10,
// 					maxBW:         42,
// 					splitCls:      2,
// 					endProps:      endProps1,
// 					minActiveRsvs: 1,
// 				}},
// 			},
// 			paths: map[addr.IA][]snet.Path{
// 				xtest.MustParseIA("1-ff00:0:2"): {
// 					te.NewSnetPath("1-ff00:0:1", 1, 2, "1-ff00:0:2"), // direct
// 				},
// 			},
// 			reservations: map[addr.IA][]*segment.Reservation{
// 				xtest.MustParseIA("1-ff00:0:2"): st.NewRsvs(1,
// 					st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
// 					st.AddIndex(0, st.WithBW(12, 42, 0), st.WithExpiration(tomorrow)),
// 					st.AddIndex(1, st.WithBW(12, 24, 0),
// 						st.WithExpiration(tomorrow.Add(24*time.Hour))),
// 					st.WithActiveIndex(0),
// 					st.WithTrafficSplit(2),
// 					st.WithEndProps(endProps1)),
// 			},
// 			expectedRequestsCalls: 0,
// 			expectedWakeupTime:    now.Add(sleepAtMost),
// 		},
// 	}
// 	for name, tc := range cases {
// 		name, tc := name, tc
// 		t.Run(name, func(t *testing.T) {
// 			t.Parallel()
// 			ctx := context.Background()
// 			ctrl := gomock.NewController(t)
// 			defer ctrl.Finish()

// 			localIA := xtest.MustParseIA("1-ff00:0:1")

// 			manager := mockManager(ctrl, now, localIA)
// 			entries := matchRsvsWithConfiguration(tc.reservations, tc.configured)
// 			keeper := keeper{
// 				manager:   manager,
// 				deletemes: tc.configured,
// 			}
// 			manager.EXPECT().GetReservationsAtSource(gomock.Any(), gomock.Any()).
// 				Times(len(tc.configured)).DoAndReturn(
// 				func(_ context.Context, dstIA addr.IA) (
// 					[]*segment.Reservation, error) {

// 					return tc.reservations[dstIA], nil
// 				})
// 			manager.EXPECT().PathsTo(gomock.Any(),
// 				gomock.Any()).Times(len(tc.configured)).DoAndReturn(
// 				func(_ context.Context, dstIA addr.IA) ([]snet.Path, error) {
// 					return tc.paths[dstIA], nil
// 				})
// 			manager.EXPECT().SetupManyRequest(gomock.Any(), gomock.Any()).
// 				Times(tc.expectedRequestsCalls).DoAndReturn(
// 				func(_ context.Context, reqs []*segment.SetupReq) []error {
// 					return make([]error, len(reqs))
// 				})
// 			manager.EXPECT().ActivateManyRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
// 				AnyTimes().DoAndReturn(
// 				func(_ context.Context, reqs []*base.Request, steps []base.PathSteps, paths []slayerspath.Path) []error {
// 					return make([]error, len(reqs), len(paths))
// 				})

// 			wakeupTime, err := keeper.OneShot(ctx)
// 			require.NoError(t, err)
// 			require.Equal(t, tc.expectedWakeupTime, wakeupTime)
// 		})
// 	}
// }

func TestRequirementsCompliance(t *testing.T) {
	now := util.SecsToTime(0)
	tomorrow := now.Add(3600 * 24 * time.Second)
	reqs := &configuration{
		pathType:  reservation.UpPath,
		predicate: newSequence(t, "1-ff00:0:1 1-ff00:0:2"), // direct
		minBW:     10,
		maxBW:     42,
		splitCls:  2,
		endProps:  reservation.StartLocal | reservation.EndLocal | reservation.EndTransfer,
	}
	cases := map[string]struct {
		conf               *configuration
		rsv                *segment.Reservation
		atLeastUntil       time.Time
		expectedCompliance Compliance
	}{
		"compliant, one index": {
			conf: reqs,
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.AddIndex(0, st.WithBW(12, 24, 0), st.WithExpiration(tomorrow)),
				st.WithPathType(reservation.UpPath),
				st.WithActiveIndex(0),
				st.WithTrafficSplit(2),
				st.WithEndProps(reqs.endProps)),
			atLeastUntil:       now,
			expectedCompliance: Compliant,
		},
		"one non compliant index, minbw": {
			conf: reqs,
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.AddIndex(0, st.WithBW(1, 24, 0), st.WithExpiration(tomorrow)),
				st.WithActiveIndex(0),
				st.WithTrafficSplit(2),
				st.WithEndProps(reqs.endProps)),
			atLeastUntil:       now,
			expectedCompliance: NeedsIndices,
		},
		"one non compliant index, maxbw": {
			conf: reqs,
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.AddIndex(0, st.WithBW(12, 44, 0), st.WithExpiration(tomorrow)),
				st.WithActiveIndex(0),
				st.WithTrafficSplit(2),
				st.WithEndProps(reqs.endProps)),
			atLeastUntil:       now,
			expectedCompliance: NeedsIndices,
		},
		"one non compliant index, expired": {
			conf: reqs,
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.AddIndex(0, st.WithBW(12, 24, 0), st.WithExpiration(now)),
				st.WithActiveIndex(0),
				st.WithTrafficSplit(2),
				st.WithEndProps(reqs.endProps)),
			atLeastUntil:       now,
			expectedCompliance: NeedsIndices,
		},
		"no active indices": {
			conf: reqs,
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.AddIndex(0, st.WithBW(12, 24, 0), st.WithExpiration(tomorrow)),
				st.ConfirmAllIndices(),
				st.WithTrafficSplit(2),
				st.WithEndProps(reqs.endProps)),
			atLeastUntil:       now,
			expectedCompliance: NeedsActivation,
		},
		"no indices": {
			conf: reqs,
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.WithTrafficSplit(2),
				st.WithEndProps(reqs.endProps)),
			atLeastUntil:       now,
			expectedCompliance: NeedsIndices,
		},
		"compliant in the past, not now": {
			conf: reqs,
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.AddIndex(0, st.WithBW(12, 24, 0), st.WithExpiration(tomorrow)),
				st.AddIndex(1, st.WithBW(1, 24, 0), st.WithExpiration(tomorrow)),
				st.WithActiveIndex(1), // will destroy index 0
				st.WithTrafficSplit(2),
				st.WithEndProps(reqs.endProps)),
			atLeastUntil:       now,
			expectedCompliance: NeedsIndices,
		},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			entry := &entry{
				conf: tc.conf,
				rsv:  tc.rsv,
			}
			c := compliance(entry, tc.atLeastUntil)
			require.Equal(t, tc.expectedCompliance, c,
				"expected %s got %s", tc.expectedCompliance, c)
		})
	}
}

func TestMatchRsvsWithConfiguration(t *testing.T) {
	r1 := st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
		st.WithPathType(reservation.UpPath),
		st.WithTrafficSplit(1), // 1
		st.WithEndProps(reservation.StartLocal))
	r2 := st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
		st.WithPathType(reservation.UpPath),
		st.WithTrafficSplit(2), // 2
		st.WithEndProps(reservation.StartLocal))
	c1 := &configuration{
		dst:       xtest.MustParseIA("1-ff00:0:2"),
		pathType:  reservation.UpPath,
		predicate: newSequence(t, "1-ff00:0:1 1-ff00:0:2"),
		splitCls:  1,
		endProps:  reservation.StartLocal,
	}
	c2 := &configuration{
		dst:       xtest.MustParseIA("1-ff00:0:2"),
		pathType:  reservation.UpPath,
		predicate: newSequence(t, "1-ff00:0:1 1-ff00:0:2"),
		splitCls:  2,
		endProps:  reservation.StartLocal,
	}
	c1_copy := func() *configuration { a := *c1; return &a }()
	cases := map[string]struct {
		rsvs              []*seg.Reservation
		confs             []*configuration
		expectedConfToRsv []int // configuration i paired with rsvs[ expected[i] ]
	}{
		"both": {
			rsvs:              []*seg.Reservation{r1, r2},
			confs:             []*configuration{c1, c2},
			expectedConfToRsv: []int{0, 1},
		},
		"unordered": {
			rsvs:              []*seg.Reservation{r1, r2},
			confs:             []*configuration{c2, c1},
			expectedConfToRsv: []int{1, 0},
		},
		"only_one": {
			rsvs:              []*seg.Reservation{r2},
			confs:             []*configuration{c1, c2},
			expectedConfToRsv: []int{-1, 0},
		},
		"none": {
			rsvs:              []*seg.Reservation{},
			confs:             []*configuration{c2, c1},
			expectedConfToRsv: []int{-1, -1},
		},
		"same_config": {
			rsvs:              []*seg.Reservation{r1, r2},
			confs:             []*configuration{c1, c1_copy},
			expectedConfToRsv: []int{0, -1},
		},
		"no_config": {
			rsvs:              []*seg.Reservation{r1, r2},
			confs:             []*configuration{},
			expectedConfToRsv: []int{},
		},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			// check that the test case is well formed
			require.Equal(t, len(tc.confs), len(tc.expectedConfToRsv),
				"wrong use case: expected matches must have the same length as confs")

			entries := matchRsvsWithConfiguration(tc.rsvs, tc.confs)
			require.Len(t, entries, len(tc.confs))

			confToReservation := make(map[*configuration]*seg.Reservation)
			for i, e := range tc.expectedConfToRsv {
				var r *seg.Reservation
				if e >= 0 {
					r = tc.rsvs[e]
				}
				_, ok := confToReservation[tc.confs[i]]
				require.False(t, ok)
				confToReservation[tc.confs[i]] = r
			}
			for i, e := range entries {
				require.Contains(t, confToReservation, e.conf)
				require.Same(t, confToReservation[e.conf], e.rsv,
					"entry %d has unexpected reservation", i)
				delete(confToReservation, e.conf)
			}
		})
	}
}

func TestFindCompatibleConfiguration(t *testing.T) {
	cases := map[string]struct {
		rsv      *seg.Reservation
		confs    []*configuration
		expected int // index of match on `confs`, or -1
	}{
		"ok": {
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.WithTrafficSplit(2),
				st.WithEndProps(reservation.StartLocal)),
			confs: []*configuration{
				{
					dst:       xtest.MustParseIA("1-ff00:0:2"),
					pathType:  reservation.UpPath,
					predicate: newSequence(t, "1-ff00:0:1 1-ff00:0:2"), // direct
					minBW:     10,
					maxBW:     42,
					splitCls:  2,
					endProps:  reservation.StartLocal,
				},
			},
			expected: 0,
		},
		"bad_path_type": {
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.DownPath),
				st.WithTrafficSplit(2),
				st.WithEndProps(reservation.StartLocal)),
			confs: []*configuration{
				{
					dst:       xtest.MustParseIA("1-ff00:0:2"),
					pathType:  reservation.UpPath,
					predicate: newSequence(t, "1-ff00:0:1 1-ff00:0:2"), // direct
					minBW:     10,
					maxBW:     42,
					splitCls:  2,
					endProps:  reservation.StartLocal,
				},
			},
			expected: -1,
		},
		"bad_traffic_split": {
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.WithTrafficSplit(1),
				st.WithEndProps(reservation.StartLocal)),
			confs: []*configuration{
				{
					dst:       xtest.MustParseIA("1-ff00:0:2"),
					pathType:  reservation.UpPath,
					predicate: newSequence(t, "1-ff00:0:1 1-ff00:0:2"), // direct
					minBW:     10,
					maxBW:     42,
					splitCls:  2,
					endProps:  reservation.StartLocal,
				},
			},
			expected: -1,
		},
		"bad_end_props": {
			rsv: st.NewRsv(st.WithPath("1-ff00:0:1", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.WithTrafficSplit(2),
				st.WithEndProps(reservation.StartLocal)),
			confs: []*configuration{
				{
					dst:       xtest.MustParseIA("1-ff00:0:2"),
					pathType:  reservation.UpPath,
					predicate: newSequence(t, "1-ff00:0:1 1-ff00:0:2"), // direct
					minBW:     10,
					maxBW:     42,
					splitCls:  2,
					endProps:  reservation.StartLocal | reservation.EndLocal,
				},
			},
			expected: -1,
		},
		"bad_path": {
			rsv: st.NewRsv(st.WithPath("1-ff00:0:11", 1, 1, "1-ff00:0:2"),
				st.WithPathType(reservation.UpPath),
				st.WithTrafficSplit(2),
				st.WithEndProps(reservation.StartLocal)),
			confs: []*configuration{
				{
					dst:       xtest.MustParseIA("1-ff00:0:2"),
					pathType:  reservation.UpPath,
					predicate: newSequence(t, "1-ff00:0:1 1-ff00:0:2"), // direct
					minBW:     10,
					maxBW:     42,
					splitCls:  2,
					endProps:  reservation.StartLocal,
				},
			},
			expected: -1,
		},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			i := findCompatibleConfiguration(tc.rsv, tc.confs)
			require.Equal(t, tc.expected, i)
		})
	}
}

func fakeReqs(ids ...int) []*segment.SetupReq {
	reqs := make([]*segment.SetupReq, len(ids))
	for i, id := range ids {
		reqs[i] = fakeReq(id)
	}
	return reqs
}

func fakeReq(id int) *segment.SetupReq {
	return &segment.SetupReq{
		MinBW: reservation.BWCls(id),
	}
}

func newSequence(t *testing.T, str string) *pathpol.Sequence {
	t.Helper()
	seq, err := pathpol.NewSequence(str)
	xtest.FailOnErr(t, err)
	return seq
}

func modOneRsv(rsvs []*segment.Reservation, whichRsv int,
	mods ...st.ReservationMod) []*segment.Reservation {

	rsvs[whichRsv] = st.ModRsv(rsvs[whichRsv], mods...)
	return rsvs
}

func mockManager(ctrl *gomock.Controller, now time.Time,
	localIA addr.IA) *mockmanager.MockManager {

	m := mockmanager.NewMockManager(ctrl)
	m.EXPECT().LocalIA().AnyTimes().Return(localIA)
	m.EXPECT().Now().AnyTimes().Return(now)
	return m
}
