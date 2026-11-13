package ledger

import (
	"errors"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestLedgerRejectsOversizedMempoolEnvelopeBeforePersistence(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Accept(tx.Envelope{Memo: strings.Repeat("M", tx.MaxEnvelopeJSONBytes+1)})
	if !errors.Is(err, tx.ErrEnvelopeTooLarge) {
		t.Fatalf("oversized direct ledger submission: got %v", err)
	}
	if got := store.Status().MempoolSize; got != 0 {
		t.Fatalf("oversized envelope persisted in mempool: %d", got)
	}
}

func TestLedgerRejectsImportedBlockWithMoreThanConsensusTransactionCap(t *testing.T) {
	count := consensus.MaxConsensusBlockTransactions + 1
	block := Block{
		ChainID: protocol.DefaultChainID, StateRoot: "state-root",
		Height: 1, TransactionCount: count,
		TransactionIDs: make([]string, count),
		Transactions: make([]tx.Envelope, count),
	}
	_, err := importBlockIntoState(persistedState{}, block, protocol.DefaultChainID)
	if !errors.Is(err, ErrInvalidBlock) {
		t.Fatalf("oversized imported block should fail before state execution: %v", err)
	}
}
