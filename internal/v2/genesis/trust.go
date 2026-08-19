package genesis

import (
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type TrustAnchor struct {
	Network       types.NetworkID
	ValidatorRoot types.Hash
}

func (g Config) ValidatorSet() (v2consensus.ValidatorSet, error) {
	network, err := g.NetworkID()
	if err != nil {
		return v2consensus.ValidatorSet{}, err
	}
	set := v2consensus.ValidatorSet{Network: network, Validators: make([]v2consensus.Validator, len(g.Validators))}
	for i, validator := range g.Validators {
		set.Validators[i] = v2consensus.Validator{
			ID:        validator.ID,
			PublicKey: append([]byte(nil), validator.ConsensusPublicKey...),
			Power:     validator.VotingPower,
		}
	}
	if err := set.Validate(); err != nil {
		return v2consensus.ValidatorSet{}, ErrInvalidGenesis
	}
	return set, nil
}

// TrustAnchor derives the two values a Citizen wallet must embed or obtain from
// a trusted checkpoint before it accepts self-verifiable light data.
func (g Config) TrustAnchor() (TrustAnchor, error) {
	set, err := g.ValidatorSet()
	if err != nil {
		return TrustAnchor{}, err
	}
	root, err := set.Root()
	if err != nil {
		return TrustAnchor{}, ErrInvalidGenesis
	}
	return TrustAnchor{Network: set.Network, ValidatorRoot: root}, nil
}
