package ledger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var (
	ErrDuplicateTransaction  = errors.New("transaction already exists")
	ErrInvalidNonce          = errors.New("invalid nonce for account")
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrNoTransactionsToBlock = errors.New("no transactions available for block production")
	ErrBlockInvariant        = errors.New("block production invariant failed")
	ErrInvalidBlock          = errors.New("invalid block")
	ErrBlockOutOfSequence    = errors.New("block out of sequence")
	ErrBlockConflict         = errors.New("conflicting block at height")
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

type ValidatorSnapshot struct {
	Validators     []dpos.Validator    `json:"validators"`
	ElectionConfig dpos.ElectionConfig `json:"electionConfig"`
	Version        uint64              `json:"version"`
	UpdatedAt      *time.Time          `json:"updatedAt,omitempty"`
}

type ConsensusView struct {
	CurrentHeight         uint64     `json:"currentHeight"`
	NextHeight            uint64     `json:"nextHeight"`
	ValidatorSetVersion   uint64     `json:"validatorSetVersion"`
	ValidatorSetUpdatedAt *time.Time `json:"validatorSetUpdatedAt,omitempty"`
	ValidatorCount        int        `json:"validatorCount"`
	TotalVotingPower      uint64     `json:"totalVotingPower"`
	QuorumVotingPower     uint64     `json:"quorumVotingPower"`
	NextProposer          string     `json:"nextProposer,omitempty"`
}

type Snapshot struct {
	Accounts                map[string]AccountState `json:"accounts"`
	Mempool                 []MempoolEntry          `json:"mempool"`
	Blocks                  []Block                 `json:"blocks"`
	CommittedTransactionIDs []string                `json:"committedTransactionIds"`
	AppliedFundingIDs       []string                `json:"appliedFundingIds"`
	ValidatorSnapshot       ValidatorSnapshot       `json:"validatorSnapshot"`
	Proposals               []consensus.Proposal    `json:"proposals"`
	Votes                   []VoteRecord            `json:"votes"`
	CommitCertificates      []CommitCertificate     `json:"commitCertificates"`
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
	AppliedFundingIDs       []string                `json:"appliedFundingIds"`
	ValidatorSnapshot       ValidatorSnapshot       `json:"validatorSnapshot"`
	Proposals               []consensus.Proposal    `json:"proposals"`
	Votes                   []VoteRecord            `json:"votes"`
	CommitCertificates      []CommitCertificate     `json:"commitCertificates"`
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
	appliedFundingIDs     map[string]struct{}
	validatorSnapshot     ValidatorSnapshot
	proposals             []consensus.Proposal
	votes                 []VoteRecord
	commitCertificates    []CommitCertificate
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
		appliedFundingIDs:     make(map[string]struct{}),
		validatorSnapshot:     normalizeValidatorSnapshot(ValidatorSnapshot{}),
		proposals:             make([]consensus.Proposal, 0),
		votes:                 make([]VoteRecord, 0),
		commitCertificates:    make([]CommitCertificate, 0),
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
	return s.CreditWithID("", address, amount)
}

func (s *Store) CreditWithID(requestID string, address string, amount uint64) (AccountState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	if requestID != "" {
		if containsString(state.AppliedFundingIDs, requestID) {
			account := state.Accounts[address]
			account.Address = address
			return account, nil
		}
	}

	account := state.Accounts[address]
	account.Address = address
	account.Balance += amount
	state.Accounts[address] = account
	if requestID != "" {
		state.AppliedFundingIDs = append(state.AppliedFundingIDs, requestID)
	}

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

func (s *Store) BlockAtHeight(height uint64) (Block, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if height == 0 || height > uint64(len(s.blocks)) {
		return Block{}, false
	}

	return cloneBlock(s.blocks[height-1]), true
}

func (s *Store) Status() StatusView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := StatusView{MempoolSize: len(s.mempool)}
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

func (s *Store) ValidatorSet() ValidatorSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneValidatorSnapshot(s.validatorSnapshot)
}

func (s *Store) SetValidators(validators []dpos.Validator, config dpos.ElectionConfig) (ValidatorSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	now := time.Now().UTC()
	state.ValidatorSnapshot = normalizeValidatorSnapshot(ValidatorSnapshot{
		Validators:     cloneValidators(validators),
		ElectionConfig: dpos.NormalizeElectionConfig(config),
		Version:        state.ValidatorSnapshot.Version + 1,
		UpdatedAt:      &now,
	})
	state.Proposals = make([]consensus.Proposal, 0)
	state.Votes = make([]VoteRecord, 0)
	state.CommitCertificates = make([]CommitCertificate, 0)

	if err := s.writeState(state); err != nil {
		return ValidatorSnapshot{}, err
	}

	s.applyStateLocked(state)
	return cloneValidatorSnapshot(s.validatorSnapshot), nil
}

func (s *Store) Consensus() ConsensusView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return consensusFromState(s.blocks, s.validatorSnapshot)
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return snapshotFromPersisted(s.snapshotLocked())
}

func (s *Store) Restore(snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := persistedFromSnapshot(snapshot)
	if err := s.writeState(state); err != nil {
		return err
	}

	s.applyStateLocked(state)
	return nil
}

func (s *Store) Accept(envelope tx.Envelope) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	id := tx.ID(envelope)
	if containsString(state.CommittedTransactionIDs, id) {
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

func (s *Store) BuildNextBlock(maxTransactions int, producedAt time.Time) (Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := s.snapshotLocked()
	_, block, err := produceBlockFromState(state, maxTransactions, producedAt)
	if err != nil {
		return Block{}, err
	}

	return cloneBlock(block), nil
}

func (s *Store) ProduceBlock(maxTransactions int) (Block, error) {
	return s.ProduceBlockWithOptions(maxTransactions, time.Time{}, false)
}

func (s *Store) ProduceBlockWithOptions(maxTransactions int, producedAt time.Time, requireConsensus bool) (Block, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	var (
		nextState persistedState
		block     Block
		err       error
	)
	if requireConsensus {
		nextState, block, err = produceCertifiedBlockFromState(state, producedAt)
	} else {
		nextState, block, err = produceBlockFromState(state, maxTransactions, producedAt)
	}
	if err != nil {
		return Block{}, err
	}

	if err := s.writeState(nextState); err != nil {
		return Block{}, err
	}

	s.applyStateLocked(nextState)
	return cloneBlock(block), nil
}

func (s *Store) ImportBlock(block Block) error {
	return s.ImportBlockWithOptions(block, false)
}

func (s *Store) ImportBlockWithOptions(block Block, requireConsensus bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	nextState, err := importBlockIntoState(state, block)
	if err != nil {
		return err
	}
	if requireConsensus {
		if err := validateBlockConsensus(state, block, true); err != nil {
			return err
		}
	}

	if err := s.writeState(nextState); err != nil {
		return err
	}

	s.applyStateLocked(nextState)
	return nil
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

	state = normalizeState(state)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyStateLocked(state)
	return nil
}

func (s *Store) writeState(state persistedState) error {
	state = normalizeState(state)
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.statePath, raw, 0o644)
}

func (s *Store) snapshotLocked() persistedState {
	committedIDs := mapKeys(s.committedTransactions)
	appliedFundingIDs := mapKeys(s.appliedFundingIDs)
	return persistedState{
		Accounts:                cloneAccounts(s.accounts),
		Mempool:                 cloneMempool(s.mempool),
		Blocks:                  cloneBlocks(s.blocks),
		CommittedTransactionIDs: committedIDs,
		AppliedFundingIDs:       appliedFundingIDs,
		ValidatorSnapshot:       cloneValidatorSnapshot(s.validatorSnapshot),
		Proposals:               cloneProposals(s.proposals),
		Votes:                   cloneVoteRecords(s.votes),
		CommitCertificates:      cloneCommitCertificates(s.commitCertificates),
	}
}

func (s *Store) applyStateLocked(state persistedState) {
	state = normalizeState(state)
	s.accounts = cloneAccounts(state.Accounts)
	s.mempool = cloneMempool(state.Mempool)
	s.blocks = cloneBlocks(state.Blocks)
	s.validatorSnapshot = cloneValidatorSnapshot(state.ValidatorSnapshot)
	s.proposals = cloneProposals(state.Proposals)
	s.votes = cloneVoteRecords(state.Votes)
	s.commitCertificates = cloneCommitCertificates(state.CommitCertificates)
	s.committedTransactions = make(map[string]struct{}, len(state.CommittedTransactionIDs))
	for _, id := range state.CommittedTransactionIDs {
		s.committedTransactions[id] = struct{}{}
	}
	s.appliedFundingIDs = make(map[string]struct{}, len(state.AppliedFundingIDs))
	for _, id := range state.AppliedFundingIDs {
		s.appliedFundingIDs[id] = struct{}{}
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

func produceBlockFromState(state persistedState, maxTransactions int, producedAt time.Time) (persistedState, Block, error) {
	state = normalizeState(state)
	if len(state.Mempool) == 0 {
		return state, Block{}, ErrNoTransactionsToBlock
	}

	if maxTransactions <= 0 || maxTransactions > len(state.Mempool) {
		maxTransactions = len(state.Mempool)
	}

	selected := append([]MempoolEntry(nil), state.Mempool[:maxTransactions]...)
	remaining := append([]MempoolEntry(nil), state.Mempool[maxTransactions:]...)
	accounts := cloneAccounts(state.Accounts)

	previousHash := ""
	height := uint64(1)
	if len(state.Blocks) > 0 {
		latest := state.Blocks[len(state.Blocks)-1]
		previousHash = latest.Hash
		height = latest.Height + 1
	}

	transactionIDs := make([]string, 0, len(selected))
	transactions := make([]tx.Envelope, 0, len(selected))
	for _, entry := range selected {
		envelope := entry.Envelope
		sender := accounts[envelope.From]
		sender.Address = envelope.From
		if sender.Balance < envelope.Amount || sender.Nonce+1 != envelope.Nonce {
			return state, Block{}, ErrBlockInvariant
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

	if producedAt.IsZero() {
		producedAt = time.Now().UTC()
	}
	block := Block{
		Height:           height,
		PreviousHash:     previousHash,
		ProducedAt:       producedAt,
		TransactionCount: len(transactions),
		TransactionIDs:   append([]string(nil), transactionIDs...),
		Transactions:     append([]tx.Envelope(nil), transactions...),
	}
	block.Hash = blockHash(block)

	state.Accounts = accounts
	state.Mempool = remaining
	state.Blocks = append(state.Blocks, block)
	state.CommittedTransactionIDs = append(state.CommittedTransactionIDs, transactionIDs...)
	state = normalizeState(state)
	return state, block, nil
}

func importBlockIntoState(state persistedState, block Block) (persistedState, error) {
	state = normalizeState(state)
	if block.Height == 0 {
		return state, ErrInvalidBlock
	}
	if block.TransactionCount != len(block.Transactions) || len(block.TransactionIDs) != len(block.Transactions) {
		return state, ErrInvalidBlock
	}

	currentHeight := uint64(len(state.Blocks))
	if block.Height <= currentHeight {
		existing := state.Blocks[block.Height-1]
		if existing.Hash == block.Hash {
			return state, nil
		}
		return state, ErrBlockConflict
	}
	if block.Height != currentHeight+1 {
		return state, ErrBlockOutOfSequence
	}

	expectedPreviousHash := ""
	if currentHeight > 0 {
		expectedPreviousHash = state.Blocks[currentHeight-1].Hash
	}
	if block.PreviousHash != expectedPreviousHash {
		return state, ErrBlockOutOfSequence
	}

	accounts := cloneAccounts(state.Accounts)
	transactionIDs := make([]string, 0, len(block.Transactions))
	transactions := make([]tx.Envelope, 0, len(block.Transactions))
	committedSet := toStringSet(state.CommittedTransactionIDs)

	for index, envelope := range block.Transactions {
		if err := envelope.ValidateStatic(); err != nil {
			return state, ErrInvalidBlock
		}

		id := tx.ID(envelope)
		if block.TransactionIDs[index] != id {
			return state, ErrInvalidBlock
		}
		if _, exists := committedSet[id]; exists {
			return state, ErrBlockConflict
		}

		sender := accounts[envelope.From]
		sender.Address = envelope.From
		if sender.Balance < envelope.Amount || sender.Nonce+1 != envelope.Nonce {
			return state, ErrBlockInvariant
		}

		sender.Balance -= envelope.Amount
		sender.Nonce = envelope.Nonce
		accounts[envelope.From] = sender

		receiver := accounts[envelope.To]
		receiver.Address = envelope.To
		receiver.Balance += envelope.Amount
		accounts[envelope.To] = receiver

		transactionIDs = append(transactionIDs, id)
		transactions = append(transactions, envelope)
	}

	sanitized := Block{
		Height:           block.Height,
		PreviousHash:     block.PreviousHash,
		ProducedAt:       block.ProducedAt,
		TransactionCount: len(transactions),
		TransactionIDs:   append([]string(nil), transactionIDs...),
		Transactions:     append([]tx.Envelope(nil), transactions...),
	}
	expectedHash := blockHash(sanitized)
	if block.Hash != expectedHash {
		return state, ErrInvalidBlock
	}
	sanitized.Hash = expectedHash

	removeSet := toStringSet(transactionIDs)
	state.Accounts = accounts
	state.Mempool = filterMempool(state.Mempool, removeSet)
	state.Blocks = append(state.Blocks, sanitized)
	state.CommittedTransactionIDs = append(state.CommittedTransactionIDs, transactionIDs...)
	state = normalizeState(state)
	return state, nil
}

func validateBlockConsensus(state persistedState, block Block, requireCertificate bool) error {
	state = normalizeState(state)
	if len(state.ValidatorSnapshot.Validators) == 0 {
		return ErrNoValidatorSet
	}

	proposal := matchProposalForBlock(state.Proposals, block)
	if proposal == nil {
		return ErrConsensusProposalRequired
	}
	if proposal.PreviousHash != block.PreviousHash {
		return ErrConsensusPreviousHash
	}
	if expected := proposerForHeight(state.ValidatorSnapshot.Validators, block.Height); expected != "" && proposal.Proposer != expected {
		return ErrUnexpectedProposer
	}
	if requireCertificate && matchCertificateForBlock(state.CommitCertificates, block) == nil {
		return ErrConsensusCertificateRequired
	}
	return nil
}

func normalizeState(state persistedState) persistedState {
	if state.Accounts == nil {
		state.Accounts = make(map[string]AccountState)
	}
	if state.Mempool == nil {
		state.Mempool = make([]MempoolEntry, 0)
	}
	if state.Blocks == nil {
		state.Blocks = make([]Block, 0)
	}
	if state.CommittedTransactionIDs == nil {
		state.CommittedTransactionIDs = make([]string, 0)
	}
	if state.AppliedFundingIDs == nil {
		state.AppliedFundingIDs = make([]string, 0)
	}
	if state.Proposals == nil {
		state.Proposals = make([]consensus.Proposal, 0)
	}
	if state.Votes == nil {
		state.Votes = make([]VoteRecord, 0)
	}
	if state.CommitCertificates == nil {
		state.CommitCertificates = make([]CommitCertificate, 0)
	}
	state.ValidatorSnapshot = normalizeValidatorSnapshot(state.ValidatorSnapshot)
	state.CommittedTransactionIDs = uniqueSortedStrings(state.CommittedTransactionIDs)
	state.AppliedFundingIDs = uniqueSortedStrings(state.AppliedFundingIDs)
	return state
}

func snapshotFromPersisted(state persistedState) Snapshot {
	state = normalizeState(state)
	return Snapshot{
		Accounts:                cloneAccounts(state.Accounts),
		Mempool:                 cloneMempool(state.Mempool),
		Blocks:                  cloneBlocks(state.Blocks),
		CommittedTransactionIDs: append([]string(nil), state.CommittedTransactionIDs...),
		AppliedFundingIDs:       append([]string(nil), state.AppliedFundingIDs...),
		ValidatorSnapshot:       cloneValidatorSnapshot(state.ValidatorSnapshot),
		Proposals:               cloneProposals(state.Proposals),
		Votes:                   cloneVoteRecords(state.Votes),
		CommitCertificates:      cloneCommitCertificates(state.CommitCertificates),
	}
}

func persistedFromSnapshot(snapshot Snapshot) persistedState {
	return normalizeState(persistedState{
		Accounts:                cloneAccounts(snapshot.Accounts),
		Mempool:                 cloneMempool(snapshot.Mempool),
		Blocks:                  cloneBlocks(snapshot.Blocks),
		CommittedTransactionIDs: append([]string(nil), snapshot.CommittedTransactionIDs...),
		AppliedFundingIDs:       append([]string(nil), snapshot.AppliedFundingIDs...),
		ValidatorSnapshot:       cloneValidatorSnapshot(snapshot.ValidatorSnapshot),
		Proposals:               cloneProposals(snapshot.Proposals),
		Votes:                   cloneVoteRecords(snapshot.Votes),
		CommitCertificates:      cloneCommitCertificates(snapshot.CommitCertificates),
	})
}

func consensusFromState(blocks []Block, snapshot ValidatorSnapshot) ConsensusView {
	snapshot = normalizeValidatorSnapshot(snapshot)
	currentHeight := uint64(len(blocks))
	nextHeight := currentHeight + 1
	totalVotingPower := uint64(0)
	for _, validator := range snapshot.Validators {
		totalVotingPower += validator.VotingPower
	}

	return ConsensusView{
		CurrentHeight:         currentHeight,
		NextHeight:            nextHeight,
		ValidatorSetVersion:   snapshot.Version,
		ValidatorSetUpdatedAt: cloneTimePointer(snapshot.UpdatedAt),
		ValidatorCount:        len(snapshot.Validators),
		TotalVotingPower:      totalVotingPower,
		QuorumVotingPower:     quorumVotingPower(totalVotingPower),
		NextProposer:          proposerForHeight(snapshot.Validators, nextHeight),
	}
}

func normalizeValidatorSnapshot(snapshot ValidatorSnapshot) ValidatorSnapshot {
	if snapshot.Validators == nil {
		snapshot.Validators = make([]dpos.Validator, 0)
	}
	if snapshot.Version > 0 || len(snapshot.Validators) > 0 || snapshot.UpdatedAt != nil {
		snapshot.ElectionConfig = dpos.NormalizeElectionConfig(snapshot.ElectionConfig)
	}
	return snapshot
}

func cloneValidatorSnapshot(snapshot ValidatorSnapshot) ValidatorSnapshot {
	snapshot = normalizeValidatorSnapshot(snapshot)
	return ValidatorSnapshot{
		Validators:     cloneValidators(snapshot.Validators),
		ElectionConfig: snapshot.ElectionConfig,
		Version:        snapshot.Version,
		UpdatedAt:      cloneTimePointer(snapshot.UpdatedAt),
	}
}

func cloneValidators(validators []dpos.Validator) []dpos.Validator {
	cloned := make([]dpos.Validator, len(validators))
	copy(cloned, validators)
	return cloned
}

func proposerForHeight(validators []dpos.Validator, height uint64) string {
	if len(validators) == 0 || height == 0 {
		return ""
	}
	index := int((height - 1) % uint64(len(validators)))
	return validators[index].Address
}

func quorumVotingPower(totalVotingPower uint64) uint64 {
	if totalVotingPower == 0 {
		return 0
	}
	return (totalVotingPower*2)/3 + 1
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
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
	for index, block := range blocks {
		cloned[index] = cloneBlock(block)
	}
	return cloned
}

func cloneBlock(block Block) Block {
	cloned := block
	cloned.TransactionIDs = append([]string(nil), block.TransactionIDs...)
	cloned.Transactions = append([]tx.Envelope(nil), block.Transactions...)
	return cloned
}

func filterMempool(mempool []MempoolEntry, remove map[string]struct{}) []MempoolEntry {
	filtered := make([]MempoolEntry, 0, len(mempool))
	for _, entry := range mempool {
		if _, exists := remove[entry.ID]; exists {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return make([]string, 0)
	}
	set := toStringSet(values)
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func toStringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func blockHash(block Block) string {
	return consensus.BlockHash(block.Height, block.PreviousHash, block.ProducedAt, block.TransactionIDs)
}
