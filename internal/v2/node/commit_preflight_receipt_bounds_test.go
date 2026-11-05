package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCommitPreflightRejectsOutOfRangeReceiptEntries(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("preflight-receipts-out-of-range")))
	native := types.TokenID(types.HashBytes("token", []byte("preflight-receipts-out-of-range")))
	validatorRoot := types.HashBytes("validators", []byte("preflight-receipts-out-of-range"))
	for _, workers := range []int{1, 4} {
		states := map[uint32]worldstate.Backend{
			0: worldstate.NewMemory(),
			1: worldstate.NewMemory(),
		}
		r, err := NewRuntime(network, native, validatorRoot, states, workers)
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := r.BuildCandidate(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.preflightCandidateState(candidate); err != nil {
			t.Fatalf("workers=%d: canonical candidate rejected: %v", workers, err)
		}
		originalRoot := r.States[0].Root()
		candidate.Receipts[2] = []sharding.CrossShardReceipt{}
		if _, _, err := r.preflightCandidateState(candidate); err != ErrCandidateState {
			t.Fatalf("workers=%d: out-of-range receipt map key not rejected: %v", workers, err)
		}
		if r.States[0].Root() != originalRoot || r.Height != 0 {
			t.Fatalf("workers=%d: rejected candidate modified committed state", workers)
		}
	}
}
