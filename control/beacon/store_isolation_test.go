package beacon_test

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/scionproto/scion/control/beacon"
	"github.com/scionproto/scion/control/beacon/mock_beacon"
	"github.com/scionproto/scion/pkg/addr"
	seg "github.com/scionproto/scion/pkg/segment"
)

// ensure two membership-specific stores do not leak into each other's DB/task
func TestStorePerMembershipIsolation(t *testing.T) {
	m := gomock.NewController(t)
	// one db shared by all membership stores
	db := mock_beacon.NewMockDB(m)

	corePolicies := beacon.CorePolicies{
		Prop:    beacon.Policy{BestSetSize: 1},
		CoreReg: beacon.Policy{BestSetSize: 1},
	}

	iaA := addr.MustParseIA("1-ff00:0:110")
	iaB := addr.MustParseIA("25-ff00:0:210")

	storeA, err := beacon.NewCoreBeaconStore(corePolicies, db,
		beacon.WithMembershipISD(iaA.ISD()))
	require.NoError(t, err)
	storeB, err := beacon.NewCoreBeaconStore(corePolicies, db,
		beacon.WithMembershipISD(iaB.ISD()))
	require.NoError(t, err)

	beaconA := beacon.Beacon{
		Segment: &seg.PathSegment{ASEntries: []seg.ASEntry{{Local: iaA}}},
	}
	beaconB := beacon.Beacon{
		Segment: &seg.PathSegment{ASEntries: []seg.ASEntry{{Local: iaB}}},
	}

	// both stores see same sources
	db.EXPECT().BeaconSources(gomock.Any()).
		Return([]addr.IA{iaA, iaB}, nil).Times(2)
	// but each store only gets beacons for its own membership ISD
	db.EXPECT().CandidateBeacons(
		gomock.Any(), gomock.Any(), beacon.UsageProp, iaA, iaA.ISD(),
	).
		Return([]beacon.Beacon{beaconA}, nil)
	db.EXPECT().CandidateBeacons(
		gomock.Any(), gomock.Any(), beacon.UsageProp, iaB, iaA.ISD(),
	).
		Return(nil, nil)
	db.EXPECT().CandidateBeacons(
		gomock.Any(), gomock.Any(), beacon.UsageProp, iaA, iaB.ISD(),
	).
		Return(nil, nil)
	db.EXPECT().CandidateBeacons(
		gomock.Any(), gomock.Any(), beacon.UsageProp, iaB, iaB.ISD(),
	).
		Return([]beacon.Beacon{beaconB}, nil)

	resA, err := storeA.BeaconsToPropagate(context.Background())
	require.NoError(t, err)
	require.Len(t, resA, 1)
	require.Equal(t, beaconA.Segment.ASEntries[0].Local, resA[0].Segment.ASEntries[0].Local)

	resB, err := storeB.BeaconsToPropagate(context.Background())
	require.NoError(t, err)
	require.Len(t, resB, 1)
	require.Equal(t, beaconB.Segment.ASEntries[0].Local, resB[0].Segment.ASEntries[0].Local)
}
