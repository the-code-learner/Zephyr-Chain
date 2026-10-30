//go:build linux || darwin

package node

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestWitnessEvidenceSetFailClosed(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-batch", []byte("network")))
	root := types.HashBytes("witness-batch", []byte("root"))
	keys, privates := witnessTestKeys()
	a := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 8, HeaderHash: types.Hash{1}}
	b := a
	b.HeaderHash = types.Hash{2}
	c := a
	c.Height = 9
	bundle := func(cp FinalityCheckpoint) []byte {
		return signedWitnessTestBundle(t, cp, keys, privates, 3)
	}
	first := bundle(a)
	if claims, err := VerifyWitnessEvidenceSet([][]byte{first, first, bundle(c)}, keys, network, root); err != nil || len(claims) != 3 {
		t.Fatalf("consistent evidence rejected: %d %v", len(claims), err)
	}
	if _, err := VerifyWitnessEvidenceSet([][]byte{first, bundle(b)}, keys, network, root); !errors.Is(err, ErrWitnessEquivocation) {
		t.Fatalf("competing quorums accepted: %v", err)
	}
	invalid := append([]byte(nil), first...)
	invalid[len(invalid)-1] = 'x'
	if _, err := VerifyWitnessEvidenceSet([][]byte{first, invalid}, keys, network, root); !errors.Is(err, ErrWitnessCheckpoint) {
		t.Fatalf("invalid document silently ignored: %v", err)
	}
	if _, err := VerifyWitnessEvidenceSet(nil, keys, network, root); !errors.Is(err, ErrWitnessEvidenceSet) {
		t.Fatalf("empty evidence accepted: %v", err)
	}
	big := make([][]byte, 65)
	for i := range big {
		big[i] = first
	}
	if _, err := VerifyWitnessEvidenceSet(big, keys, network, root); !errors.Is(err, ErrWitnessEvidenceSet) {
		t.Fatalf("unbounded evidence accepted: %v", err)
	}
}
