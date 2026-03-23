package ledger

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestStoreAcceptReservesBalanceAndAdvancesPendingNonce(t *testing.T) {
	store := NewStore()
	store.Credit("zph_sender", 100)

	id, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 40,
		Nonce:  1,
	})
	if err != nil {
		t.Fatalf("expected transaction to be accepted, got %v", err)
	}

	if id == "" {
		t.Fatal("expected transaction id to be generated")
	}

	view := store.View("zph_sender")
	if view.AvailableBalance != 60 {
		t.Fatalf("expected available balance 60, got %d", view.AvailableBalance)
	}

	if view.NextNonce != 2 {
		t.Fatalf("expected next nonce 2, got %d", view.NextNonce)
	}

	if view.PendingTransactions != 1 {
		t.Fatalf("expected 1 pending transaction, got %d", view.PendingTransactions)
	}
}

func TestStoreAcceptRejectsDuplicateTransactions(t *testing.T) {
	store := NewStore()
	store.Credit("zph_sender", 100)

	envelope := tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 10,
		Nonce:  1,
	}

	if _, err := store.Accept(envelope); err != nil {
		t.Fatalf("expected first transaction to be accepted, got %v", err)
	}

	if _, err := store.Accept(envelope); !errors.Is(err, ErrDuplicateTransaction) {
		t.Fatalf("expected duplicate transaction error, got %v", err)
	}
}

func TestStoreAcceptRejectsNonceGapsAndInsufficientBalance(t *testing.T) {
	store := NewStore()
	store.Credit("zph_sender", 50)

	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 10,
		Nonce:  2,
	}); !errors.Is(err, ErrInvalidNonce) {
		t.Fatalf("expected invalid nonce error, got %v", err)
	}

	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 40,
		Nonce:  1,
	}); err != nil {
		t.Fatalf("expected first valid transaction to be accepted, got %v", err)
	}

	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 20,
		Nonce:  2,
	}); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected insufficient balance error, got %v", err)
	}
}
