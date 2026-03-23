package ledger

import (
	"errors"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var (
	ErrDuplicateTransaction = errors.New("transaction already exists")
	ErrInvalidNonce         = errors.New("invalid nonce for account")
	ErrInsufficientBalance  = errors.New("insufficient balance")
)

type AccountState struct {
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

type AccountView struct {
	Address             string `json:"address"`
	Balance             uint64 `json:"balance"`
	AvailableBalance    uint64 `json:"availableBalance"`
	Nonce               uint64 `json:"nonce"`
	NextNonce           uint64 `json:"nextNonce"`
	PendingTransactions int    `json:"pendingTransactions"`
}

type pendingState struct {
	NextNonce       uint64
	ReservedBalance uint64
	PendingCount    int
}

type Store struct {
	mu           sync.RWMutex
	accounts     map[string]AccountState
	pending      map[string]pendingState
	transactions map[string]tx.Envelope
}

func NewStore() *Store {
	return &Store{
		accounts:     make(map[string]AccountState),
		pending:      make(map[string]pendingState),
		transactions: make(map[string]tx.Envelope),
	}
}

func (s *Store) Credit(address string, amount uint64) AccountState {
	s.mu.Lock()
	defer s.mu.Unlock()

	account := s.accounts[address]
	account.Address = address
	account.Balance += amount
	s.accounts[address] = account

	return account
}

func (s *Store) View(address string) AccountView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.accountView(address)
}

func (s *Store) MempoolSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.transactions)
}

func (s *Store) Accept(envelope tx.Envelope) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := tx.ID(envelope)
	if _, exists := s.transactions[id]; exists {
		return "", ErrDuplicateTransaction
	}

	account := s.accounts[envelope.From]
	account.Address = envelope.From

	pending := s.pending[envelope.From]
	expectedNonce := account.Nonce + 1
	if pending.PendingCount > 0 {
		expectedNonce = pending.NextNonce
	}

	if envelope.Nonce != expectedNonce {
		return "", ErrInvalidNonce
	}

	availableBalance := account.Balance
	if pending.ReservedBalance > availableBalance {
		availableBalance = 0
	} else {
		availableBalance -= pending.ReservedBalance
	}

	if envelope.Amount > availableBalance {
		return "", ErrInsufficientBalance
	}

	pending.NextNonce = envelope.Nonce + 1
	pending.ReservedBalance += envelope.Amount
	pending.PendingCount++

	s.pending[envelope.From] = pending
	s.accounts[envelope.From] = account
	s.transactions[id] = envelope

	return id, nil
}

func (s *Store) accountView(address string) AccountView {
	account := s.accounts[address]
	account.Address = address

	pending := s.pending[address]
	availableBalance := account.Balance
	if pending.ReservedBalance > availableBalance {
		availableBalance = 0
	} else {
		availableBalance -= pending.ReservedBalance
	}

	nextNonce := account.Nonce + 1
	if pending.PendingCount > 0 {
		nextNonce = pending.NextNonce
	}

	return AccountView{
		Address:             address,
		Balance:             account.Balance,
		AvailableBalance:    availableBalance,
		Nonce:               account.Nonce,
		NextNonce:           nextNonce,
		PendingTransactions: pending.PendingCount,
	}
}
