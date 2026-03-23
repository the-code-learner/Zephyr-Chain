package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var (
	ErrDuplicateTransaction   = errors.New("transaction already exists")
	ErrInvalidNonce          = errors.New("invalid nonce for account")
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrNoTransactionsToBlock = errors.New("no transactions available for block production")
	ErrBlockInvariant        = errors.New("block production invariant failed")
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

type MempoolEntry struct {
	ID       string      `json:"id"`
	QueuedAt time.Time   `json:"queuedAt"`
	Envelope tx.Envelope `json:"envelope"`
}

type Block struct {
	Height           uint64        `json:"height"`
	Hash             string        `json:"hash"`
	PreviousHash     string        `json:"previousHash"`
	ProducedAt       time.Time     `json:"producedAt"`
	TransactionCount int           `json:"transactionCount"`
	TransactionIDs   []string      `json:"transactionIds"`
	Transactions     []tx.Envelope `json:"transactions"`
}

type StatusView struct {
	Height          uint64     `json:"height"`
	LatestBlockHash string     `json:"latestBlockHash"`
	LatestBlockAt   *time.Time `json:"latestBlockAt,omitempty"`
	MempoolSize     int        `json:"mempoolSize"`
}

type pendingState struct {
	NextNonce       uint64
	ReservedBalance uint64
	PendingCount    int
}

type persistedState struct {
	Accounts                map[string]AccountState `json:"accounts"`
	Mempool                 []MempoolEntry          `json:"mempool"`
	Blocks                  []Block                 `json:"blocks"`
	CommittedTransactionIDs []string                `json:"committedTransactionIds"`
}

type Store struct {
	mu                    sync.RWMutex
	dataDir               string
	statePath             string
	accounts              map[string]AccountState
	pending               map[string]pendingState
	mempool               []MempoolEntry
	blocks                []Block
	committedTransactions map[string]struct{}
}

func NewStore(dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = filepath.Join("var", "node")
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	store := &Store{
		dataDir:               dataDir,
		statePath:             filepath.Join(dataDir, "state.json"),
		accounts:              make(map[string]AccountState),
		pending:               make(map[string]pendingState),
		mempool:               make([]MempoolEntry, 0),
		blocks:                make([]Block, 0),
		committedTransactions: make(map[string]struct{}),
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) DataDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.dataDir
}

func (s *Store) Credit(address string, amount uint64) (AccountState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	account := state.Accounts[address]
	account.Address = address
	account.Balance += amount
	state.Accounts[address] = account

	if err := s.writeState(state); err != nil {
		return AccountState{}, err
	}

	s.applyStateLocked(state)
	return account, nil
}

func (s *Store) View(address string) AccountView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.accountViewLocked(address)
}

func (s *Store) MempoolSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.mempool)
}

func (s *Store) LatestBlock() (Block, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.blocks) == 0 {
		return Block{}, false
	}

	return cloneBlock(s.blocks[len(s.blocks)-1]), true
}

func (s *Store) Status() StatusView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := StatusView{
		MempoolSize: len(s.mempool),
	}

	if len(s.blocks) == 0 {
		return status
	}

	latest := s.blocks[len(s.blocks)-1]
	producedAt := latest.ProducedAt
	status.Height = latest.Height
	status.LatestBlockHash = latest.Hash
	status.LatestBlockAt = &producedAt
	return status
}

func (s *Store) Accept(envelope tx.Envelope) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	id := tx.ID(envelope)
	if containsCommitted(state.CommittedTransactionIDs, id) {
		return "", ErrDuplicateTransaction
	}

	for _, entry := range state.Mempool {
		if entry.ID == id {
			return "", ErrDuplicateTransaction
		}
	}

	pending := rebuildPendingState(state.Accounts, state.Mempool)
	account := state.Accounts[envelope.From]
	account.Address = envelope.From

	expectedNonce := account.Nonce + 1
	if senderPending, ok := pending[envelope.From]; ok && senderPending.PendingCount > 0 {
		expectedNonce = senderPending.NextNonce
	}

	if envelope.Nonce != expectedNonce {
		return "", ErrInvalidNonce
	}

	availableBalance := account.Balance
	if senderPending, ok := pending[envelope.From]; ok {
		if senderPending.ReservedBalance > availableBalance {
			availableBalance = 0
		} else {
			availableBalance -= senderPending.ReservedBalance
		}
	}

	if envelope.Amount > availableBalance {
		return "", ErrInsufficientBalance
	}

	state.Accounts[envelope.From] = account
	state.Mempool = append(state.Mempool, MempoolEntry{
		ID:       id,
		QueuedAt: time.Now().UTC(),
		Envelope: envelope,
	})

	if err := s.writeState(state); err != nil {
		return "", err
	}

	s.applyStateLocked(state)
	return id, nil
}

func (s *Store) ProduceBlock(maxTransactions int) (Block, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	if len(state.Mempool) == 0 {
		return Block{}, ErrNoTransactionsToBlock
	}

	if maxTransactions <= 0 || maxTransactions > len(state.Mempool) {
		maxTransactions = len(state.Mempool)
	}

	selected := append([]MempoolEntry(nil), state.Mempool[:maxTransactions]...)
	remaining := append([]MempoolEntry(nil), state.Mempool[maxTransactions:]...)

	previousHash := ""
	height := uint64(1)
	if len(state.Blocks) > 0 {
		latest := state.Blocks[len(state.Blocks)-1]
		previousHash = latest.Hash
		height = latest.Height + 1
	}

	transactionIDs := make([]string, 0, len(selected))
	transactions := make([]tx.Envelope, 0, len(selected))
	accounts := cloneAccounts(state.Accounts)

	for _, entry := range selected {
		envelope := entry.Envelope
		sender := accounts[envelope.From]
		sender.Address = envelope.From
		if sender.Balance < envelope.Amount || sender.Nonce+1 != envelope.Nonce {
			return Block{}, ErrBlockInvariant
		}

		sender.Balance -= envelope.Amount
		sender.Nonce = envelope.Nonce
		accounts[envelope.From] = sender

		receiver := accounts[envelope.To]
		receiver.Address = envelope.To
		receiver.Balance += envelope.Amount
		accounts[envelope.To] = receiver

		transactionIDs = append(transactionIDs, entry.ID)
		transactions = append(transactions, envelope)
	}

	producedAt := time.Now().UTC()
	block := Block{
		Height:           height,
		PreviousHash:     previousHash,
		ProducedAt:       producedAt,
		TransactionCount: len(selected),
		TransactionIDs:   append([]string(nil), transactionIDs...),
		Transactions:     append([]tx.Envelope(nil), transactions...),
	}
	block.Hash = blockHash(block)

	state.Accounts = accounts
	state.Mempool = remaining
	state.Blocks = append(state.Blocks, block)
	state.CommittedTransactionIDs = append(state.CommittedTransactionIDs, transactionIDs...)

	if err := s.writeState(state); err != nil {
		return Block{}, err
	}

	s.applyStateLocked(state)
	return cloneBlock(block), nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}

	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyStateLocked(state)
	return nil
}

func (s *Store) writeState(state persistedState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.statePath, raw, 0o644)
}

func (s *Store) snapshotLocked() persistedState {
	committedIDs := make([]string, 0, len(s.committedTransactions))
	for id := range s.committedTransactions {
		committedIDs = append(committedIDs, id)
	}
	sort.Strings(committedIDs)

	return persistedState{
		Accounts:                cloneAccounts(s.accounts),
		Mempool:                 cloneMempool(s.mempool),
		Blocks:                  cloneBlocks(s.blocks),
		CommittedTransactionIDs: committedIDs,
	}
}

func (s *Store) applyStateLocked(state persistedState) {
	s.accounts = cloneAccounts(state.Accounts)
	s.mempool = cloneMempool(state.Mempool)
	s.blocks = cloneBlocks(state.Blocks)
	s.committedTransactions = make(map[string]struct{}, len(state.CommittedTransactionIDs))
	for _, id := range state.CommittedTransactionIDs {
		s.committedTransactions[id] = struct{}{}
	}
	s.pending = rebuildPendingState(s.accounts, s.mempool)
}

func (s *Store) accountViewLocked(address string) AccountView {
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

func rebuildPendingState(accounts map[string]AccountState, mempool []MempoolEntry) map[string]pendingState {
	pending := make(map[string]pendingState)
	for _, entry := range mempool {
		envelope := entry.Envelope
		state := pending[envelope.From]
		if state.PendingCount == 0 {
			state.NextNonce = accounts[envelope.From].Nonce + 1
		}
		state.NextNonce = envelope.Nonce + 1
		state.ReservedBalance += envelope.Amount
		state.PendingCount++
		pending[envelope.From] = state
	}
	return pending
}

func cloneAccounts(accounts map[string]AccountState) map[string]AccountState {
	cloned := make(map[string]AccountState, len(accounts))
	for key, value := range accounts {
		cloned[key] = value
	}
	return cloned
}

func cloneMempool(mempool []MempoolEntry) []MempoolEntry {
	cloned := make([]MempoolEntry, len(mempool))
	copy(cloned, mempool)
	return cloned
}

func cloneBlocks(blocks []Block) []Block {
	cloned := make([]Block, len(blocks))
	for i, block := range blocks {
		cloned[i] = cloneBlock(block)
	}
	return cloned
}

func cloneBlock(block Block) Block {
	cloned := block
	cloned.TransactionIDs = append([]string(nil), block.TransactionIDs...)
	cloned.Transactions = append([]tx.Envelope(nil), block.Transactions...)
	return cloned
}

func containsCommitted(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func blockHash(block Block) string {
	payload, _ := json.Marshal(struct {
		Height         uint64   `json:"height"`
		PreviousHash   string   `json:"previousHash"`
		ProducedAt     string   `json:"producedAt"`
		TransactionIDs []string `json:"transactionIds"`
	}{
		Height:         block.Height,
		PreviousHash:   block.PreviousHash,
		ProducedAt:     block.ProducedAt.Format(time.RFC3339Nano),
		TransactionIDs: block.TransactionIDs,
	})

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
