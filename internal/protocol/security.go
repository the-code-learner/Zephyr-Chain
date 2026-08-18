package protocol

import (
	"errors"
	"strings"
	"unicode"
)

const (
	DefaultChainID = "zephyr-devnet-1"

	TransactionDomain       = "zephyr/transaction/v1"
	ConsensusProposalDomain = "zephyr/consensus/proposal/v1"
	ConsensusVoteDomain     = "zephyr/consensus/vote/v1"
	TransportIdentityDomain = "zephyr/transport/identity/v1"
	TransportRequestDomain  = "zephyr/transport/request/v1"
	StateDomain             = "zephyr/state/v1"
	SnapshotDomain          = "zephyr/snapshot/v1"
)

var ErrInvalidChainID = errors.New("invalid chain ID")

func ConfiguredChainID(chainID string) string {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		return DefaultChainID
	}
	return chainID
}

func ValidateChainID(chainID string) error {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" || len(chainID) > 64 {
		return ErrInvalidChainID
	}
	for _, r := range chainID {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return ErrInvalidChainID
	}
	return nil
}
