package consensus

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestConsensusProposalRejectsExcessiveTransactionCountBeforeSignatureWork(t *testing.T) {
	p := Proposal{
		TransactionIDs: make([]string, MaxConsensusBlockTransactions+1),
		Transactions:   make([]tx.Envelope, MaxConsensusBlockTransactions+1),
	}
	if err := p.ValidateStatic(); !errors.Is(err, ErrTooManyTransactions) {
		t.Fatalf("proposal above the hard block count limit: %v", err)
	}
	if err := validateProposalTransactions("", p.TransactionIDs, p.Transactions); !errors.Is(err, ErrTooManyTransactions) {
		t.Fatalf("proposal transaction helper did not enforce count limit: %v", err)
	}
	p.TransactionIDs = p.TransactionIDs[:MaxConsensusBlockTransactions]
	p.Transactions = p.Transactions[:MaxConsensusBlockTransactions]
	if err := p.ValidateStatic(); errors.Is(err, ErrTooManyTransactions) {
		t.Fatalf("at-limit proposal rejected by transaction count guard: %v", err)
	}
}
