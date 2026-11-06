package merkle

import (
	"reflect"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Preserve the previous allocate-per-level algorithm as a test-only oracle
// so that in-place construction cannot silently alter consensus Merkle roots.
func legacyMerkleRoot(leaves []types.Hash) types.Hash {
	if len(leaves) == 0 {
		return emptyLeaf()
	}
	level := append([]types.Hash(nil), leaves...)
	target := nextPowerOfTwo(len(level))
	for len(level) < target {
		level = append(level, emptyLeaf())
	}
	for len(level) > 1 {
		next := make([]types.Hash, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			next[i/2] = branch(level[i], level[i+1])
		}
		level = next
	}
	return level[0]
}

func legacyMerkleProof(leaves []types.Hash, index int) (Proof, error) {
	if index < 0 || index >= len(leaves) {
		return Proof{}, ErrIndex
	}
	leafCount := len(leaves)
	level := append([]types.Hash(nil), leaves...)
	target := nextPowerOfTwo(len(level))
	for len(level) < target {
		level = append(level, emptyLeaf())
	}
	proof := Proof{Index: uint32(index), LeafCount: uint32(leafCount)}
	position := index
	for len(level) > 1 {
		proof.Siblings = append(proof.Siblings, level[position^1])
		next := make([]types.Hash, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			next[i/2] = branch(level[i], level[i+1])
		}
		position /= 2
		level = next
	}
	return proof, nil
}

func TestMerkleInPlaceReductionMatchesLegacyRootsAndProofs(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65, 129} {
		leaves := make([]types.Hash, n)
		for i := range leaves {
			leaves[i] = Leaf("merkle-inplace", []byte{byte(i), byte(i >> 8), byte(n)})
		}
		original := make([]types.Hash, len(leaves))
		copy(original, leaves)
		want := legacyMerkleRoot(leaves)
		for repeat := 0; repeat < 2; repeat++ {
			got := Root(leaves)
			if got != want {
				t.Fatalf("n=%d repeat=%d: root changed", n, repeat)
			}
		}
		for index := range leaves {
			got, err := BuildProof(leaves, index)
			if err != nil {
				t.Fatalf("n=%d index=%d: proof failed: %v", n, index, err)
			}
			wantProof, err := legacyMerkleProof(leaves, index)
			if err != nil || !reflect.DeepEqual(got, wantProof) {
				t.Fatalf("n=%d index=%d: canonical proof bytes changed: %v", n, index, err)
			}
			if !Verify(want, leaves[index], got) {
				t.Fatalf("n=%d index=%d: in-place proof does not verify", n, index)
			}
		}
		if !reflect.DeepEqual(leaves, original) {
			t.Fatalf("n=%d: Merkle operation mutated caller input", n)
		}
	}
}

func TestMerkleInPlaceReductionRejectsOutOfRangeProof(t *testing.T) {
	leaves := []types.Hash{Leaf("merkle-inplace", []byte("one"))}
	for _, index := range []int{-1, 1, 100} {
		if _, err := BuildProof(leaves, index); err != ErrIndex {
			t.Fatalf("invalid proof index %d accepted: %v", index, err)
		}
	}
	if _, err := BuildProof(nil, 0); err != ErrIndex {
		t.Fatalf("empty proof allowed: %v", err)
	}
}
