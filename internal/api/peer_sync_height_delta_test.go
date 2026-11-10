package api

import (
	"math"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

func TestPeerHeightDeltaSaturatesWithoutDirectionInversion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote uint64
		local  uint64
		want   int64
	}{
		{name: "both zero", remote: 0, local: 0, want: 0},
		{name: "single ahead", remote: 10, local: 9, want: 1},
		{name: "single behind", remote: 9, local: 10, want: -1},
		{name: "maximum positive in range", remote: uint64(math.MaxInt64), local: 0, want: math.MaxInt64},
		{name: "positive saturates across sign bit", remote: uint64(math.MaxInt64) + 1, local: 0, want: math.MaxInt64},
		{name: "maximum uint64 ahead", remote: math.MaxUint64, local: 0, want: math.MaxInt64},
		{name: "maximum negative in range", remote: 0, local: uint64(math.MaxInt64), want: -math.MaxInt64},
		{name: "negative saturates across sign bit", remote: 0, local: uint64(math.MaxInt64) + 1, want: math.MinInt64},
		{name: "maximum uint64 behind", remote: 0, local: math.MaxUint64, want: math.MinInt64},
		{name: "large adjacent heights", remote: math.MaxUint64, local: math.MaxUint64 - 1, want: 1},
		{name: "large adjacent heights reversed", remote: math.MaxUint64 - 1, local: math.MaxUint64, want: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := peerHeightDelta(tc.remote, tc.local); got != tc.want {
				t.Fatalf("peerHeightDelta(%d, %d)=%d, want %d", tc.remote, tc.local, got, tc.want)
			}
		})
	}
}

func TestDerivePeerSyncStatePreservesDirectionForUntrustedLargeHeights(t *testing.T) {
	for _, tc := range []struct {
		name         string
		remoteHeight uint64
		localHeight  uint64
		wantState    string
		wantDelta    int64
		wantSync     bool
	}{
		{name: "extreme peer ahead", remoteHeight: math.MaxUint64, localHeight: 0, wantState: "peer_ahead", wantDelta: math.MaxInt64, wantSync: true},
		{name: "extreme peer behind", remoteHeight: 0, localHeight: math.MaxUint64, wantState: "peer_behind", wantDelta: math.MinInt64, wantSync: false},
		{name: "high adjacent peer ahead", remoteHeight: math.MaxUint64, localHeight: math.MaxUint64 - 1, wantState: "peer_ahead", wantDelta: 1, wantSync: true},
		{name: "high adjacent peer behind", remoteHeight: math.MaxUint64 - 1, localHeight: math.MaxUint64, wantState: "peer_behind", wantDelta: -1, wantSync: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, delta, sync := derivePeerSyncState(
				ledger.StatusView{Height: tc.localHeight},
				ledger.StatusView{Height: tc.remoteHeight},
			)
			if state != tc.wantState || delta != tc.wantDelta || sync != tc.wantSync {
				t.Fatalf("derived state=%q delta=%d needsSync=%t; want %q %d %t", state, delta, sync, tc.wantState, tc.wantDelta, tc.wantSync)
			}
		})
	}
}
