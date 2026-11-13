package ledger

import (
	"errors"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestLedgerRejectsOversizedEnvelopeBeforeMempoolWrite(t *testing.T) {
	store := newTestStore(t)
	huge := tx.Envelope{From: "zph_sender", To: "zph_receiver", Amount: 1, Nonce: 1, Memo: strings.Repeat("M", tx.MaxEnvelopeJSONBytes+1)}
	if _, err := store.Accept(huge); !errors.Is(err, tx.ErrEnvelopeTooLarge) {
		t.Fatalf("ledger accepted oversized envelope: %v", err)
	}
	if count := store.Status().MempoolSize; count != 0 {
		t.Fatalf("oversized envelope entered durable mempool: %d", count)
	}
}

func TestLedgerImportRejectsTooManyTransactionsBeforeStateChange(t *testing.T) {
	store := newTestStore(t)
	block := Block{
		ChainID: store.chainID, Height: 1, StateRoot: "ignored",
		TransactionCount: consensus.MaxConsensusBlockTransactions + 1,
		TransactionIDs: make([]string, consensus.MaxConsensusBlockTransactions+1),
		Transactions: make([]tx.Envelope, consensus.MaxConsensusBlockTransactions+1),
	}
	if err := store.ImportBlock(block); !errors.Is(err, ErrInvalidBlock) {
		t.Fatalf("oversized block import did not fail closed: %v", err)
	}
	if height := store.Status().Height; height != 0 {
		t.Fatalf("oversized block advanced finalized ledger: %d", height)
	}
}
