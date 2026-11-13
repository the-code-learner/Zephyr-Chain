package ledger

import (
	"encoding/json"
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
)

// MaxCertifiedDeliveryJSONBytes is a consensus admission invariant as well
// as the transport's byte limit. A node must not finalize a block whose
// transferable proof could be rejected by a correctly configured peer.
const MaxCertifiedDeliveryJSONBytes int64 = 64 << 20

// The wrapper {"block":...,"evidence":{"proposal":...,"votes":[...]}}
// has fewer than 128 bytes of fixed JSON punctuation/property names. Count
// one comma per vote; the estimate deliberately reserves a conservative
// maximum serialized size for EVERY authorized validator, not only quorum.
const certifiedDeliveryWrapperBudget int64 = 128

var ErrCertifiedDeliveryBudget = errors.New("certified block exceeds protocol delivery byte budget")

// certifiedDeliveryBudgetFits uses subtraction to avoid integer overflow when
// computing the worst case for an attacker-controlled validator roster.
func certifiedDeliveryBudgetFits(blockBytes, proposalBytes, validators int64) bool {
	if blockBytes < 0 || proposalBytes < 0 || validators < 0 ||
		blockBytes > MaxCertifiedDeliveryJSONBytes-certifiedDeliveryWrapperBudget {
		return false
	}
	remaining := MaxCertifiedDeliveryJSONBytes - certifiedDeliveryWrapperBudget - blockBytes
	if proposalBytes > remaining {
		return false
	}
	remaining -= proposalBytes
	return validators <= remaining/(consensus.MaxSignedVoteJSONBytes+1)
}

// checkCertifiedDeliveryBudget is deliberately called before a proposal is
// recorded/voted, and again on certificate-backed block imports. Both the
// signed proposal and block encode the transactions; measuring only one would
// underestimate the actual authenticated delivery JSON. Every signed vote is
// independently bounded in consensus.Vote.ValidateStatic().
func checkCertifiedDeliveryBudget(proposal consensus.Proposal, validatorCount int) error {
	if validatorCount < 0 {
		return ErrCertifiedDeliveryBudget
	}
	blockJSON, err := json.Marshal(blockFromProposal(proposal))
	if err != nil {
		return ErrCertifiedDeliveryBudget
	}
	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		return ErrCertifiedDeliveryBudget
	}
	if !certifiedDeliveryBudgetFits(int64(len(blockJSON)), int64(len(proposalJSON)), int64(validatorCount)) {
		return ErrCertifiedDeliveryBudget
	}
	return nil
}
