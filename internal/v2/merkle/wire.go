package merkle

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func (p Proof) MarshalBinary() []byte {
	var w codec.Writer
	w.U32(p.Index)
	w.U32(p.LeafCount)
	w.U32(uint32(len(p.Siblings)))
	for _, sibling := range p.Siblings {
		w.Fixed(sibling[:])
	}
	return w.BytesCopy()
}

func ParseProof(data []byte) (Proof, error) {
	r := codec.NewReader(data)
	index, err := r.U32()
	if err != nil {
		return Proof{}, ErrIndex
	}
	leafCount, err := r.U32()
	if err != nil || leafCount == 0 || index >= leafCount {
		return Proof{}, ErrIndex
	}
	count, err := r.U32()
	if err != nil || count > 32 {
		return Proof{}, ErrIndex
	}
	proof := Proof{Index: index, LeafCount: leafCount, Siblings: make([]types.Hash, int(count))}
	for i := range proof.Siblings {
		raw, err := r.Fixed(32)
		if err != nil {
			return Proof{}, ErrIndex
		}
		copy(proof.Siblings[i][:], raw)
	}
	if r.Done() != nil {
		return Proof{}, ErrIndex
	}
	return proof, nil
}
