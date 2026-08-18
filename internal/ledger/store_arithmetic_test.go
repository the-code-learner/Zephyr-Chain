package ledger

import (
	"errors"
	"math"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestStoreCreditRejectsBalanceOverflow(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Credit("zph_account", math.MaxUint64); err != nil {
		t.Fatalf("seed max balance: %v", err)
	}

	if _, err := store.Credit("zph_account", 1); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("expected balance overflow error, got %v", err)
	}
	if balance := store.View("zph_account").Balance; balance != math.MaxUint64 {
		t.Fatalf("expected balance to remain max uint64, got %d", balance)
	}
}

func TestStoreSetValidatorsRejectsTotalVotingPowerOverflow(t *testing.T) {
	store := newTestStore(t)
	_, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: "a", VotingPower: math.MaxUint64},
		{Rank: 2, Address: "b", VotingPower: 1},
	}, dpos.ElectionConfig{MaxValidators: 2})
	if !errors.Is(err, ErrVotingPowerOverflow) {
		t.Fatalf("expected voting power overflow error, got %v", err)
	}
}

func TestStoreProduceBlockRejectsReceiverBalanceOverflow(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Credit("sender", 1); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	if _, err := store.Credit("receiver", math.MaxUint64); err != nil {
		t.Fatalf("credit receiver: %v", err)
	}
	if _, err := store.Accept(tx.Envelope{From: "sender", To: "receiver", Amount: 1, Nonce: 1}); err != nil {
		t.Fatalf("queue transaction: %v", err)
	}

	if _, err := store.ProduceBlock(1); !errors.Is(err, ErrBlockInvariant) {
		t.Fatalf("expected block invariant error for receiver overflow, got %v", err)
	}
	if balance := store.View("receiver").Balance; balance != math.MaxUint64 {
		t.Fatalf("expected receiver balance to remain max uint64, got %d", balance)
	}
}

func TestStoreRejectsNonceAfterUint64Exhaustion(t *testing.T) {
	store := newTestStore(t)
	if err := store.Restore(Snapshot{Accounts: map[string]AccountState{
		"sender": {Address: "sender", Balance: 1, Nonce: math.MaxUint64},
	}}); err != nil {
		t.Fatalf("restore exhausted account: %v", err)
	}

	_, err := store.Accept(tx.Envelope{From: "sender", To: "receiver", Amount: 1, Nonce: 0})
	if !errors.Is(err, ErrNonceExhausted) {
		t.Fatalf("expected nonce exhausted error, got %v", err)
	}
}
