package consensus

import (
	"bytes"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Root commits to the exact validator identities, public keys and integer
// voting powers that are authorized for a header. It is deliberately ordered
// by ValidatorID so peers and Citizen Nodes derive the same commitment.
func (s ValidatorSet) Root() (types.Hash, error) {
	if err := s.Validate(); err != nil {
		return types.Hash{}, err
	}
	validators := append([]Validator(nil), s.Validators...)
	sort.Slice(validators, func(i, j int) bool {
		return bytes.Compare(validators[i].ID[:], validators[j].ID[:]) < 0
	})
	leaves := make([]types.Hash, len(validators))
	for i, validator := range validators {
		var w codec.Writer
		w.Fixed(validator.ID[:])
		w.Bytes(validator.PublicKey)
		w.U64(validator.Power)
		leaves[i] = merkle.Leaf("validator", w.BytesCopy())
	}
	return merkle.Root(leaves), nil
}
