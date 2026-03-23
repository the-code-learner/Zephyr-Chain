package dpos

import (
	"errors"
	"sort"
)

var ErrInvalidElectionConfig = errors.New("invalid election config")

type Service struct {
	config ElectionConfig
}

func NewService(config ElectionConfig) (*Service, error) {
	cfg := config.withDefaults()
	if cfg.MaxValidators <= 0 {
		return nil, ErrInvalidElectionConfig
	}

	return &Service{config: cfg}, nil
}

func (s *Service) ElectValidators(candidates []Candidate, votes []Vote) ([]Validator, error) {
	if s == nil {
		return nil, ErrInvalidElectionConfig
	}

	cfg := s.config.withDefaults()
	index := make(map[string]Candidate, len(candidates))
	delegatedStake := make(map[string]uint64, len(candidates))

	for _, candidate := range candidates {
		if candidate.Address == "" {
			continue
		}

		index[candidate.Address] = candidate
	}

	for _, vote := range votes {
		if vote.Candidate == "" || vote.Amount == 0 {
			continue
		}

		if _, exists := index[vote.Candidate]; !exists {
			continue
		}

		delegatedStake[vote.Candidate] += vote.Amount
	}

	validators := make([]Validator, 0, len(index))
	for address, candidate := range index {
		if candidate.SelfStake < cfg.MinSelfStake {
			continue
		}

		if candidate.MissedBlocks > cfg.MaxMissedBlocks {
			continue
		}

		validators = append(validators, Validator{
			Address:        address,
			VotingPower:    candidate.SelfStake + delegatedStake[address],
			SelfStake:      candidate.SelfStake,
			DelegatedStake: delegatedStake[address],
			CommissionRate: candidate.CommissionRate,
		})
	}

	sort.Slice(validators, func(i, j int) bool {
		if validators[i].VotingPower != validators[j].VotingPower {
			return validators[i].VotingPower > validators[j].VotingPower
		}

		if validators[i].SelfStake != validators[j].SelfStake {
			return validators[i].SelfStake > validators[j].SelfStake
		}

		if validators[i].CommissionRate != validators[j].CommissionRate {
			return validators[i].CommissionRate < validators[j].CommissionRate
		}

		return validators[i].Address < validators[j].Address
	})

	if len(validators) > cfg.MaxValidators {
		validators = validators[:cfg.MaxValidators]
	}

	for i := range validators {
		validators[i].Rank = i + 1
	}

	return validators, nil
}

