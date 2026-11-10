package api

import (
	"math"
	"reflect"
	"testing"
)

// This is the same transition used by syncFromPeer. The final iteration at
// MaxUint64 must not wrap back to block zero, and an equal/older peer must
// never be asked for a new height.
func TestNextPeerSyncBlockHeightNoUnsignedWrap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current uint64
		remote  uint64
		next    uint64
		ok      bool
	}{
		{name: "same genesis", current: 0, remote: 0},
		{name: "same maximum", current: math.MaxUint64, remote: math.MaxUint64},
		{name: "older peer", current: 17, remote: 16},
		{name: "maximum peer behind", current: math.MaxUint64, remote: 0},
		{name: "next genesis block", current: 0, remote: 1, next: 1, ok: true},
		{name: "advance near max", current: math.MaxUint64 - 2, remote: math.MaxUint64, next: math.MaxUint64 - 1, ok: true},
		{name: "reach max", current: math.MaxUint64 - 1, remote: math.MaxUint64, next: math.MaxUint64, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next, ok := nextPeerSyncBlockHeight(tc.current, tc.remote)
			if next != tc.next || ok != tc.ok {
				t.Fatalf("nextPeerSyncBlockHeight(%d,%d) = (%d,%t), want (%d,%t)", tc.current, tc.remote, next, ok, tc.next, tc.ok)
			}
		})
	}
}

func TestNextPeerSyncBlockHeightFullTerminalRange(t *testing.T) {
	for _, tc := range []struct {
		name          string
		local, remote uint64
		want          []uint64
	}{
		{name: "no extra fetch at maximum", local: math.MaxUint64, remote: math.MaxUint64},
		{name: "no fetch for behind peer", local: math.MaxUint64, remote: math.MaxUint64 - 1},
		{name: "one block at maximum", local: math.MaxUint64 - 1, remote: math.MaxUint64, want: []uint64{math.MaxUint64}},
		{name: "two blocks ending at maximum", local: math.MaxUint64 - 2, remote: math.MaxUint64, want: []uint64{math.MaxUint64 - 1, math.MaxUint64}},
		{name: "ordinary bounded range", local: 5, remote: 7, want: []uint64{6, 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []uint64
			for height, pending := nextPeerSyncBlockHeight(tc.local, tc.remote); pending; height, pending = nextPeerSyncBlockHeight(height, tc.remote) {
				got = append(got, height)
				if len(got) > 3 {
					t.Fatal("peer height iterator did not terminate")
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("peer block sync heights=%v, want %v", got, tc.want)
			}
		})
	}
}
