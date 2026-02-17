package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scionproto/scion/control/ifstate"
	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/private/topology"
)

// Verify that private-only links are only selected when the membership matches
func TestPropagationFilterPrivateOnly(t *testing.T) {
	privateISD := addr.ISD(25)
	publicISD := addr.ISD(1)

	// interface to a neighbor in private ISD 25, private-only
	intfPriv := mkIF(10, topology.Core, true, []addr.ISD{privateISD}, nil, mustIA(t, "25-ff00:0:210"))
	// interface to a neighbor in public ISD 1, private-only (should be filtered for private membership)
	intfWrong := mkIF(11, topology.Core, true, nil, nil, mustIA(t, "1-ff00:0:130"))
	// public interface in ISD 1
	intfPub := mkIF(12, topology.Core, false, nil, nil, mustIA(t, "1-ff00:0:131"))

	ifaces := []*ifstate.Interface{intfPriv, intfWrong, intfPub}

	// private membership filter (ISD 25) should keep only intfPriv
	privFilter := newPropagationFilter(true, true, privateISD)
	privGot := filter(ifaces, privFilter)
	require.Equal(t, []uint16{10}, ids(privGot))

	// public membership filter (ISD 1) should keep intfPub only (private-only links rejected)
	pubFilter := newPropagationFilter(true, false, publicISD)
	pubGot := filter(ifaces, pubFilter)
	require.Equal(t, []uint16{12}, ids(pubGot))
}

// Private membership should not send beacons over public links to public neighbors
func TestPropagationFilterPrivateToPublicLink(t *testing.T) {
	privateISD := addr.ISD(25)
	publicISD := addr.ISD(1)

	intfPublic := mkIF(30, topology.Core, false, nil, nil, mustIA(t, "1-ff00:0:140"))
	intfPrivate := mkIF(31, topology.Core, true, []addr.ISD{privateISD}, []addr.ISD{privateISD}, mustIA(t, "25-ff00:0:240"))

	ifaces := []*ifstate.Interface{intfPublic, intfPrivate}
	privFilter := newPropagationFilter(true, true, privateISD)
	privGot := filter(ifaces, privFilter)
	require.Equal(t, []uint16{31}, ids(privGot))

	pubFilter := newPropagationFilter(true, false, publicISD)
	pubGot := filter(ifaces, pubFilter)
	require.Equal(t, []uint16{30}, ids(pubGot))
}

// Verify that private beacons are not sent to neighbors in other private ISDs even on private-only links
func TestPropagationFilterPrivateOnlyWrongISD(t *testing.T) {
	privateISD := addr.ISD(25)
	otherISD := addr.ISD(26)

	// private-only interface but allowed only ISD 26 (not our membership)
	intfWrongAllowed := mkIF(20, topology.Core, true, []addr.ISD{otherISD}, []addr.ISD{otherISD}, mustIA(t, "26-ff00:0:210"))
	// private-only interface allowed for ISD 25 (should pass)
	intfAllowed := mkIF(21, topology.Core, true, []addr.ISD{privateISD}, []addr.ISD{privateISD}, mustIA(t, "25-ff00:0:211"))

	ifaces := []*ifstate.Interface{intfWrongAllowed, intfAllowed}
	privFilter := newPropagationFilter(true, true, privateISD)
	privGot := filter(ifaces, privFilter)
	require.Equal(t, []uint16{21}, ids(privGot))
}

func mkIF(id uint16, lt topology.LinkType, privOnly bool, privISDs []addr.ISD, allowed []addr.ISD, remote addr.IA) *ifstate.Interface {
	info := ifstate.InterfaceInfo{
		ID:             id,
		IA:             remote,
		LinkType:       lt,
		PrivateOnly:    privOnly,
		PrivateISDs:    privISDs,
		AllowedPrivate: allowed,
	}
	intfs := ifstate.NewInterfaces(map[uint16]ifstate.InterfaceInfo{id: info}, ifstate.Config{})
	return intfs.Filtered(func(*ifstate.Interface) bool { return true })[0]
}

func filter(intfs []*ifstate.Interface, f func(*ifstate.Interface) bool) []*ifstate.Interface {
	res := []*ifstate.Interface{}
	for _, i := range intfs {
		if f(i) {
			res = append(res, i)
		}
	}
	return res
}

func ids(intfs []*ifstate.Interface) []uint16 {
	res := make([]uint16, 0, len(intfs))
	for _, i := range intfs {
		res = append(res, i.TopoInfo().ID)
	}
	return res
}

func mustIA(t *testing.T, s string) addr.IA {
	ia, err := addr.ParseIA(s)
	if err != nil {
		t.Fatalf("parse IA %q: %v", s, err)
	}
	return ia
}
