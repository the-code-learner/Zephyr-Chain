package consensus

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrValidatorWire = errors.New("invalid canonical validator set")

func (s ValidatorSet) MarshalBinary() ([]byte, error) {
	if err := s.Validate(); err != nil || len(s.Validators) > MaxCertificateVotes {
		return nil, ErrValidatorWire
	}
	var w codec.Writer
	w.Fixed(s.Network[:])
	w.U32(uint32(len(s.Validators)))
	for _, validator := range s.Validators {
		w.Fixed(validator.ID[:])
		w.Bytes(validator.PublicKey)
		w.U64(validator.Power)
	}
	return w.BytesCopy(), nil
}

func ParseValidatorSet(data []byte) (ValidatorSet, error) {
	r := codec.NewReader(data)
	networkRaw, err := r.Fixed(32)
	if err != nil {
		return ValidatorSet{}, ErrValidatorWire
	}
	var network types.NetworkID
	copy(network[:], networkRaw)
	count, err := r.U32()
	if err != nil || count == 0 || count > MaxCertificateVotes {
		return ValidatorSet{}, ErrValidatorWire
	}
	set := ValidatorSet{Network: network, Validators: make([]Validator, int(count))}
	for i := range set.Validators {
		idRaw, err := r.Fixed(32)
		if err != nil {
			return ValidatorSet{}, ErrValidatorWire
		}
		copy(set.Validators[i].ID[:], idRaw)
		set.Validators[i].PublicKey, err = r.Bytes(65)
		if err != nil || len(set.Validators[i].PublicKey) != 65 {
			return ValidatorSet{}, ErrValidatorWire
		}
		set.Validators[i].Power, err = r.U64()
		if err != nil {
			return ValidatorSet{}, ErrValidatorWire
		}
	}
	if r.Done() != nil || set.Validate() != nil {
		return ValidatorSet{}, ErrValidatorWire
	}
	return set, nil
}
