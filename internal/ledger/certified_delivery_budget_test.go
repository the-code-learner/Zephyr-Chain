package ledger

import (
	"errors"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestCertifiedDeliveryBudgetExactBoundaryAndOverflow(t *testing.T) {
	const limit = MaxCertifiedDeliveryJSONBytes
	const wrapper = certifiedDeliveryWrapperBudget
	const vote = consensus.MaxSignedVoteJSONBytes + 1
	tests := []struct {
		name string
		block, proposal, validators int64
		want bool
	}{
		{"exact total transport budget", limit-wrapper, 0, 0, true},
		{"one over total transport budget", limit-wrapper+1, 0, 0, false},
		{"exact one validator allowance", limit-wrapper-vote, 0, 1, true},
		{"one byte short for vote", limit-wrapper-vote+1, 0, 1, false},
		{"negative block rejected", -1, 0, 0, false},
		{"negative validator count rejected", 0, 0, -1, false},
		{"attacker-count overflow rejected", 0, 0, int64(^uint64(0) >> 1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := certifiedDeliveryBudgetFits(tt.block, tt.proposal, tt.validators); got != tt.want {
				t.Fatalf("fits(%d,%d,%d)=%v, want %v", tt.block, tt.proposal, tt.validators, got, tt.want)
			}
		})
	}
}

func TestCertifiedDeliveryBudgetCountsDuplicatedEscapedTransactionsAndWorstCaseVotes(t *testing.T) {
	p := consensus.Proposal{
		Transactions: make([]tx.Envelope, consensus.MaxConsensusBlockTransactions),
		TransactionIDs: make([]string, consensus.MaxConsensusBlockTransactions),
	}
	for i := range p.Transactions {
		// Escapes expand on JSON serialization, and proposal and block each
		// carry the same transaction vector. Do not budget only memo bytes.
		p.Transactions[i].Memo = strings.Repeat("\\", 2048)
		p.TransactionIDs[i] = strings.Repeat("a", 64)
	}
	if err := checkCertifiedDeliveryBudget(p, 20); err != nil {
		t.Fatalf("bounded 512-transaction / 20-validator candidate rejected: %v", err)
	}
	if err := checkCertifiedDeliveryBudget(p, 20000); !errors.Is(err, ErrCertifiedDeliveryBudget) {
		t.Fatalf("oversized validator roster must reject before voting, got %v", err)
	}
}
