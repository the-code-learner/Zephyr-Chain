package merkle

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrIndex = errors.New("merkle proof index out of range")

type Proof struct {
	Index     uint32
	LeafCount uint32
	Siblings  []types.Hash
}

func Root(leaves []types.Hash) types.Hash {
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

func BuildProof(leaves []types.Hash, index int) (Proof, error) {
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
		sibling := position ^ 1
		proof.Siblings = append(proof.Siblings, level[sibling])
		next := make([]types.Hash, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			next[i/2] = branch(level[i], level[i+1])
		}
		position /= 2
		level = next
	}
	return proof, nil
}

func Verify(root, leaf types.Hash, proof Proof) bool {
	if proof.LeafCount == 0 || proof.Index >= proof.LeafCount {
		return false
	}
	target := nextPowerOfTwo(int(proof.LeafCount))
	requiredDepth := 0
	for n := target; n > 1; n /= 2 {
		requiredDepth++
	}
	if len(proof.Siblings) != requiredDepth {
		return false
	}
	current := leaf
	position := int(proof.Index)
	for _, sibling := range proof.Siblings {
		if position%2 == 0 {
			current = branch(current, sibling)
		} else {
			current = branch(sibling, current)
		}
		position /= 2
	}
	return current == root
}

func Leaf(domain string, payload []byte) types.Hash {
	return types.Hash(codec.DomainHash("zephyr/merkle/leaf/v2/"+domain, payload))
}

func branch(left, right types.Hash) types.Hash {
	var w codec.Writer
	w.Fixed(left[:])
	w.Fixed(right[:])
	return types.Hash(codec.DomainHash("zephyr/merkle/branch/v2", w.BytesCopy()))
}

func emptyLeaf() types.Hash {
	return types.Hash(codec.DomainHash("zephyr/merkle/empty/v2", nil))
}

func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
