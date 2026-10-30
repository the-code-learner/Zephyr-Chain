//go:build linux || darwin

package node

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestWitnessQuorumConsistencyRejectsCompetingCertifiedClaims(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-quorum-pair", []byte("network")))
	root := types.HashBytes("witness-quorum-pair", []byte("root"))
	keys, private := witnessTestKeys()
	a := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 42, HeaderHash: types.Hash{1}}
	b := a
	b.HeaderHash = types.Hash{2}
	first := signedWitnessTestBundle(t, a, keys, private, 3)
	second := signedWitnessTestBundle(t, b, keys, private, 3)
	if err := CheckWitnessQuorumConsistency(first, second, keys, network, root); !errors.Is(err, ErrWitnessEquivocation) {
		t.Fatalf("two valid conflicting quorums accepted: %v", err)
	}
	if err := CheckWitnessQuorumConsistency(first, first, keys, network, root); err != nil {
		t.Fatalf("identical quorums rejected: %v", err)
	}
	// At different heights this function does not claim ancestry; that is
	// established separately by QC-certified history recovery.
	b.Height = 43
	later := signedWitnessTestBundle(t, b, keys, private, 3)
	if err := CheckWitnessQuorumConsistency(first, later, keys, network, root); err != nil {
		t.Fatalf("different heights incorrectly marked same-height equivocation: %v", err)
	}
	corrupt := append([]byte(nil), second...)
	corrupt[len(corrupt)-2] ^= 1
	if err := CheckWitnessQuorumConsistency(first, corrupt, keys, network, root); !errors.Is(err, ErrWitnessCheckpoint) {
		t.Fatalf("invalid envelope not rejected: %v", err)
	}
	if err := CheckWitnessQuorumConsistency(first, first, keys, types.NetworkID{9}, root); !errors.Is(err, ErrWitnessCheckpoint) {
		t.Fatalf("foreign network bundle accepted: %v", err)
	}
}
