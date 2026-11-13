package consensus

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestProposalRejectsTooManyTransactionsBeforeSignatureValidation(t *testing.T) {
	proposal := Proposal{
		TransactionIDs: make([]string, MaxConsensusBlockTransactions+1),
		Transactions:   make([]tx.Envelope, MaxConsensusBlockTransactions+1),
	}
	if err := proposal.ValidateStatic(); !errors.Is(err, ErrTooManyTransactions) {
		t.Fatalf("oversized proposal returned %v, want ErrTooManyTransactions", err)
	}
	if err := validateProposalTransactions("zephyr-test", proposal.TransactionIDs, proposal.Transactions); !errors.Is(err, ErrTooManyTransactions) {
		t.Fatalf("oversized transaction list returned %v", err)
	}
}
