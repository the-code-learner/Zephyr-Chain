package ledger

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestStoreAcceptReservesBalanceAndAdvancesPendingNonce(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit account: %v", err)
	}

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
	store := newTestStore(t)
	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit account: %v", err)
	}

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
	store := newTestStore(t)
	if _, err := store.Credit("zph_sender", 50); err != nil {
		t.Fatalf("credit account: %v", err)
	}

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

func TestStorePersistsMempoolAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit account: %v", err)
	}
	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 25,
		Nonce:  1,
	}); err != nil {
		t.Fatalf("accept transaction: %v", err)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	if reopened.MempoolSize() != 1 {
		t.Fatalf("expected mempool size 1 after restart, got %d", reopened.MempoolSize())
	}

	view := reopened.View("zph_sender")
	if view.AvailableBalance != 75 {
		t.Fatalf("expected available balance 75 after restart, got %d", view.AvailableBalance)
	}
	if view.NextNonce != 2 {
		t.Fatalf("expected next nonce 2 after restart, got %d", view.NextNonce)
	}
}

func TestStoreProduceBlockCommitsTransactionsAndPersistsState(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit account: %v", err)
	}
	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 25,
		Nonce:  1,
	}); err != nil {
		t.Fatalf("accept transaction: %v", err)
	}

	block, err := store.ProduceBlock(10)
	if err != nil {
		t.Fatalf("produce block: %v", err)
	}

	if block.Height != 1 {
		t.Fatalf("expected block height 1, got %d", block.Height)
	}
	if block.TransactionCount != 1 {
		t.Fatalf("expected block transaction count 1, got %d", block.TransactionCount)
	}
	if store.MempoolSize() != 0 {
		t.Fatalf("expected empty mempool after block production, got %d", store.MempoolSize())
	}

	sender := store.View("zph_sender")
	if sender.Balance != 75 || sender.Nonce != 1 || sender.PendingTransactions != 0 {
		t.Fatalf("unexpected sender state after block commit: %+v", sender)
	}

	receiver := store.View("zph_receiver")
	if receiver.Balance != 25 {
		t.Fatalf("expected receiver balance 25, got %d", receiver.Balance)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	latest, ok := reopened.LatestBlock()
	if !ok {
		t.Fatal("expected latest block after restart")
	}
	if latest.Hash != block.Hash {
		t.Fatalf("expected persisted block hash %s, got %s", block.Hash, latest.Hash)
	}

	reopenedSender := reopened.View("zph_sender")
	if reopenedSender.Balance != 75 || reopenedSender.Nonce != 1 {
		t.Fatalf("unexpected reopened sender state: %+v", reopenedSender)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	return store
}
