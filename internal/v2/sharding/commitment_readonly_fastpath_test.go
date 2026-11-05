package sharding

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestSortedCommitmentsCanonicalReadOnlyFastPath(t *testing.T) {
	a := Commitment{ShardID: 0, StateRoot: types.HashBytes("state", []byte("shard-zero"))}
	b := Commitment{ShardID: 1, StateRoot: types.HashBytes("state", []byte("shard-one"))}
	c := Commitment{ShardID: 2, StateRoot: types.HashBytes("state", []byte("shard-two"))}
	canonical := []Commitment{a, b, c}
	got, err := sortedCommitments(canonical)
	if err != nil || len(got) != len(canonical) {
		t.Fatalf("canonical commitments invalid: %v", err)
	}
	if &got[0] != &canonical[0] {
		t.Fatal("canonical sortedCommitments unnecessarily copied input")
	}
	if root, err := CommitmentRoot(canonical); err != nil || types.IsZero32([32]byte(root)) {
		t.Fatalf("canonical Merkle root failed: %v", err)
	}
	// Unsorted inputs must be copied and sorted without mutating caller memory.
	unordered := []Commitment{c, a, b}
	sorted, err := sortedCommitments(unordered)
	if err != nil || sorted[0] != a || sorted[1] != b || sorted[2] != c {
		t.Fatalf("unordered commitments not canonicalized: %v", err)
	}
	if &sorted[0] == &unordered[0] {
		t.Fatal("unordered input sorted in caller-owned memory")
	}
	sorted[0].ShardID = 100
	if unordered[0] != c || unordered[1] != a || unordered[2] != b {
		t.Fatal("sorting unordered commitments mutated caller input")
	}
	if _, err := sortedCommitments([]Commitment{b, a, b}); err != ErrShardCount {
		t.Fatalf("duplicate shard commitments accepted: %v", err)
	}
	if _, err := sortedCommitments([]Commitment{{ShardID: 0}}); err != ErrShardCount {
		t.Fatalf("zero state root accepted: %v", err)
	}
}
