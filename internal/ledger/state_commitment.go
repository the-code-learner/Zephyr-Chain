package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
)

var ErrInvalidStateRoot = errors.New("invalid committed state root")

type committedAccount struct {
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

type committedValidator struct {
	Address        string  `json:"address"`
	CommissionRate float64 `json:"commissionRate"`
	DelegatedStake uint64  `json:"delegatedStake"`
	Rank           int     `json:"rank"`
	SelfStake      uint64  `json:"selfStake"`
	VotingPower    uint64  `json:"votingPower"`
}

type committedStatePayload struct {
	Accounts            []committedAccount   `json:"accounts"`
	AppliedFundingIDs   []string             `json:"appliedFundingIds"`
	ChainID             string               `json:"chainId"`
	Domain              string               `json:"domain"`
	ElectionConfig      dpos.ElectionConfig  `json:"electionConfig"`
	ValidatorSetVersion uint64               `json:"validatorSetVersion"`
	Validators          []committedValidator `json:"validators"`
}

func StateRoot(chainID string, accounts map[string]AccountState, snapshot ValidatorSnapshot) (string, error) {
	return stateRootWithFundingIDs(chainID, accounts, snapshot, nil)
}

func stateRootWithFundingIDs(chainID string, accounts map[string]AccountState, snapshot ValidatorSnapshot, appliedFundingIDs []string) (string, error) {
	chainID = strings.TrimSpace(chainID)
	if err := protocol.ValidateChainID(chainID); err != nil {
		return "", ErrInvalidStateRoot
	}

	addresses := make([]string, 0, len(accounts))
	for address := range accounts {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	committedAccounts := make([]committedAccount, 0, len(addresses))
	for _, address := range addresses {
		account := accounts[address]
		if account.Address != "" && account.Address != address {
			return "", ErrInvalidStateRoot
		}
		committedAccounts = append(committedAccounts, committedAccount{
			Address: address,
			Balance: account.Balance,
			Nonce:   account.Nonce,
		})
	}

	snapshot = normalizeValidatorSnapshot(snapshot)
	validators := append([]dpos.Validator(nil), snapshot.Validators...)
	sort.Slice(validators, func(i, j int) bool {
		if validators[i].Rank != validators[j].Rank {
			return validators[i].Rank < validators[j].Rank
		}
		return validators[i].Address < validators[j].Address
	})
	committedValidators := make([]committedValidator, 0, len(validators))
	for _, validator := range validators {
		committedValidators = append(committedValidators, committedValidator{
			Address:        validator.Address,
			CommissionRate: validator.CommissionRate,
			DelegatedStake: validator.DelegatedStake,
			Rank:           validator.Rank,
			SelfStake:      validator.SelfStake,
			VotingPower:    validator.VotingPower,
		})
	}

	payload, err := json.Marshal(committedStatePayload{
		Accounts:            committedAccounts,
		AppliedFundingIDs:   uniqueSortedStrings(appliedFundingIDs),
		ChainID:             chainID,
		Domain:              protocol.StateDomain,
		ElectionConfig:      snapshot.ElectionConfig,
		ValidatorSetVersion: snapshot.Version,
		Validators:          committedValidators,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func stateRootFromState(chainID string, state persistedState) (string, error) {
	state = normalizeState(state)
	return stateRootWithFundingIDs(chainID, state.Accounts, state.ValidatorSnapshot, state.AppliedFundingIDs)
}
