package node

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"sort"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/lightapi"
	p2p "github.com/zephyr-chain/zephyr-chain/internal/v2/network/p2p"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

var (
	ErrServiceConfig   = errors.New("invalid v2 consensus service configuration")
	ErrNotValidator    = errors.New("node is not an active v2 validator")
	ErrNotProposer     = errors.New("node is not the scheduled v2 proposer")
	ErrNoQuorum        = errors.New("v2 proposal did not collect 2/3+ voting power")
	ErrProposalState   = errors.New("v2 proposal does not match local execution")
	ErrDoubleVote      = errors.New("refusing conflicting vote for same v2 height/round")
	ErrMempoolConflict = errors.New("v2 mempool object conflict")
	ErrNoSnapshot      = errors.New("no finalized v2 snapshot available")
)

type voteSlot struct {
	Height uint64
	Round  uint64
}

type Service struct {
	mu         sync.Mutex
	proposeMu  sync.Mutex
	Runtime    *Runtime
	Validators v2consensus.ValidatorSet
	Key        *ecdsa.PrivateKey
	P2P        *p2p.Node
	Peers      []peer.ID

	mempool       map[uint32][]tx.Transaction
	mempoolInputs map[types.ObjectID]types.Hash
	imports       map[uint32][]ReceiptImport
	votes         map[voteSlot]v2consensus.Vote
	latest        *lightapi.Snapshot
}

func NewService(runtime *Runtime, validators v2consensus.ValidatorSet, key *ecdsa.PrivateKey, transport *p2p.Node) (*Service, error) {
	if runtime == nil || transport == nil || validators.Network != runtime.Network || validators.Validate() != nil {
		return nil, ErrServiceConfig
	}
	root, err := validators.Root()
	if err != nil || root != runtime.ValidatorRoot {
		return nil, ErrServiceConfig
	}
	service := &Service{
		Runtime: runtime, Validators: validators, Key: key, P2P: transport,
		mempool: make(map[uint32][]tx.Transaction), mempoolInputs: make(map[types.ObjectID]types.Hash),
		imports: make(map[uint32][]ReceiptImport), votes: make(map[voteSlot]v2consensus.Vote),
	}
	if key != nil {
		id, err := validatorID(key)
		if err != nil || !containsValidator(validators, id) {
			return nil, ErrNotValidator
		}
	}
	transport.SetConsensusHandler(service.HandleConsensus)
	transport.SetTransactionHandler(service.HandleTransaction)
	return service, nil
}

func (s *Service) SetPeers(peers []peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[peer.ID]struct{}, len(peers))
	s.Peers = s.Peers[:0]
	for _, id := range peers {
		if id == "" || id == s.P2P.ID() {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		s.Peers = append(s.Peers, id)
	}
}

func (s *Service) Submit(transaction tx.Transaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if transaction.Network != s.Runtime.Network || transaction.ShardID >= s.Runtime.ShardCount {
		return ErrCandidateState
	}
	store := s.Runtime.States[transaction.ShardID]
	if store == nil || transaction.StateRoot != store.Root() {
		return ErrCandidateState
	}
	engine := execution.Engine{Network: s.Runtime.Network, NativeToken: s.Runtime.NativeToken, ShardCount: s.Runtime.ShardCount, Height: s.Runtime.Height + 1}
	if _, err := engine.Execute(transaction); err != nil {
		return err
	}
	id := transaction.ID()
	for _, input := range transaction.Inputs {
		if existing, conflict := s.mempoolInputs[input.ObjectID]; conflict && existing != id {
			return ErrMempoolConflict
		}
	}
	for _, pending := range s.mempool[transaction.ShardID] {
		if pending.ID() == id {
			return nil
		}
	}
	for _, input := range transaction.Inputs {
		s.mempoolInputs[input.ObjectID] = id
	}
	s.mempool[transaction.ShardID] = append(s.mempool[transaction.ShardID], transaction)
	return nil
}

func (s *Service) QueueReceiptImport(shard uint32, receiptImport ReceiptImport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if shard >= s.Runtime.ShardCount || s.Runtime.validateReceiptImport(shard, receiptImport) != nil {
		return ErrReceiptImport
	}
	s.imports[shard] = append(s.imports[shard], receiptImport)
	return nil
}

func (s *Service) HandleTransaction(_ context.Context, _ peer.ID, payload []byte) ([]byte, error) {
	transaction, err := tx.ParseTransaction(payload)
	if err != nil {
		return nil, err
	}
	if err := s.Submit(transaction); err != nil {
		return nil, err
	}
	id := transaction.ID()
	return append([]byte(nil), id[:]...), nil
}

func (s *Service) HandleConsensus(_ context.Context, _ peer.ID, payload []byte) ([]byte, error) {
	message, err := ParseConsensusMessage(payload)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch message.Kind {
	case NetworkMessageProposal:
		return s.handleProposalLocked(message)
	case NetworkMessageCommit:
		return s.handleCommitLocked(message)
	default:
		return nil, ErrNetworkWire
	}
}

func (s *Service) Propose(ctx context.Context) (lightapi.Snapshot, error) {
	s.proposeMu.Lock()
	defer s.proposeMu.Unlock()

	s.mu.Lock()
	if s.Key == nil {
		s.mu.Unlock()
		return lightapi.Snapshot{}, ErrNotValidator
	}
	height := s.Runtime.Height + 1
	const round uint64 = 0
	expected, err := s.Validators.Proposer(height, round)
	if err != nil {
		s.mu.Unlock()
		return lightapi.Snapshot{}, err
	}
	localID, err := validatorID(s.Key)
	if err != nil || localID != expected.ID {
		s.mu.Unlock()
		return lightapi.Snapshot{}, ErrNotProposer
	}
	block := s.pendingBlockLocked()
	candidate, err := s.Runtime.BuildCandidate(height, block.Batches)
	if err != nil {
		s.mu.Unlock()
		return lightapi.Snapshot{}, err
	}
	proposal, err := v2consensus.SignProposal(s.Key, candidate.Header, round)
	if err != nil {
		s.mu.Unlock()
		return lightapi.Snapshot{}, err
	}
	selfVote, err := s.signVoteLocked(proposal)
	peers := append([]peer.ID(nil), s.Peers...)
	if err != nil {
		s.mu.Unlock()
		return lightapi.Snapshot{}, err
	}
	s.mu.Unlock()

	proposalWire, err := (ConsensusMessage{Kind: NetworkMessageProposal, Proposal: proposal, Block: block}).MarshalBinary()
	if err != nil {
		return lightapi.Snapshot{}, err
	}
	votes := []v2consensus.Vote{selfVote}
	for _, remote := range peers {
		response, err := s.P2P.SendConsensus(ctx, remote, proposalWire)
		if err != nil {
			continue
		}
		vote, err := v2consensus.ParseVote(response)
		if err != nil || vote.HeaderHash != v2consensus.HeaderConsensusHash(proposal.Header) || vote.Height != height || vote.Round != round || s.Validators.VerifyVote(vote) != nil {
			continue
		}
		votes = append(votes, vote)
	}
	certificate, err := s.Validators.BuildCertificate(proposal, votes)
	if err != nil {
		return lightapi.Snapshot{}, ErrNoQuorum
	}

	s.mu.Lock()
	finalized, err := s.Runtime.Commit(candidate, certificate, s.Validators)
	if err != nil {
		s.mu.Unlock()
		return lightapi.Snapshot{}, err
	}
	snapshot := s.recordFinalizedLocked(finalized, certificate, candidate.Commitments)
	s.removeCommittedLocked(block)
	s.mu.Unlock()

	commitWire, err := (ConsensusMessage{Kind: NetworkMessageCommit, Proposal: proposal, Block: block, Certificate: &certificate}).MarshalBinary()
	if err == nil {
		for _, remote := range peers {
			_, _ = s.P2P.SendConsensus(ctx, remote, commitWire)
		}
	}
	return snapshot, nil
}

func (s *Service) handleProposalLocked(message ConsensusMessage) ([]byte, error) {
	if s.Key == nil {
		return nil, ErrNotValidator
	}
	if err := s.Validators.VerifyProposal(message.Proposal); err != nil {
		return nil, err
	}
	if message.Proposal.Header.Height != s.Runtime.Height+1 {
		return nil, ErrCandidateHeight
	}
	candidate, err := s.Runtime.BuildCandidate(message.Proposal.Header.Height, message.Block.Batches)
	if err != nil {
		return nil, err
	}
	if !sameProposalHeader(candidate.Header, message.Proposal.Header) {
		return nil, ErrProposalState
	}
	vote, err := s.signVoteLocked(message.Proposal)
	if err != nil {
		return nil, err
	}
	return vote.MarshalBinary()
}

func (s *Service) handleCommitLocked(message ConsensusMessage) ([]byte, error) {
	if message.Certificate == nil || s.Validators.VerifyProposal(message.Proposal) != nil {
		return nil, ErrCandidateCert
	}
	certificate := *message.Certificate
	if certificate.HeaderHash != v2consensus.HeaderConsensusHash(message.Proposal.Header) || certificate.Height != message.Proposal.Header.Height || certificate.Round != message.Proposal.Round || s.Validators.VerifyCertificate(certificate) != nil {
		return nil, ErrCandidateCert
	}
	if message.Proposal.Header.Height <= s.Runtime.Height {
		if message.Proposal.Header.Height == s.Runtime.Height && s.latest != nil && s.latest.Header.Hash() == message.Proposal.Header.Hash() {
			return []byte{1}, nil
		}
		return nil, ErrCandidateHeight
	}
	if message.Proposal.Header.Height != s.Runtime.Height+1 {
		return nil, ErrCandidateHeight
	}
	candidate, err := s.Runtime.BuildCandidate(message.Proposal.Header.Height, message.Block.Batches)
	if err != nil || !sameProposalHeader(candidate.Header, message.Proposal.Header) {
		return nil, ErrProposalState
	}
	finalized, err := s.Runtime.Commit(candidate, certificate, s.Validators)
	if err != nil {
		return nil, err
	}
	s.recordFinalizedLocked(finalized, certificate, candidate.Commitments)
	s.removeCommittedLocked(message.Block)
	return []byte{1}, nil
}

func (s *Service) signVoteLocked(proposal v2consensus.Proposal) (v2consensus.Vote, error) {
	if s.Key == nil {
		return v2consensus.Vote{}, ErrNotValidator
	}
	slot := voteSlot{Height: proposal.Header.Height, Round: proposal.Round}
	target := v2consensus.HeaderConsensusHash(proposal.Header)
	if prior, exists := s.votes[slot]; exists {
		if prior.HeaderHash != target {
			return v2consensus.Vote{}, ErrDoubleVote
		}
		return prior, nil
	}
	vote, err := v2consensus.SignVote(s.Key, s.Runtime.Network, proposal.Header.Height, proposal.Round, target)
	if err != nil {
		return v2consensus.Vote{}, err
	}
	if err := s.Validators.VerifyVote(vote); err != nil {
		return v2consensus.Vote{}, err
	}
	s.votes[slot] = vote
	return vote, nil
}

func (s *Service) pendingBlockLocked() BlockData {
	batches := make(map[uint32]ShardBatch)
	for shard := uint32(0); shard < s.Runtime.ShardCount; shard++ {
		transactions := append([]tx.Transaction(nil), s.mempool[shard]...)
		imports := append([]ReceiptImport(nil), s.imports[shard]...)
		if len(transactions) > 0 || len(imports) > 0 {
			batches[shard] = ShardBatch{Transactions: transactions, Imports: imports}
		}
	}
	return BlockData{Batches: batches}
}

func (s *Service) removeCommittedLocked(block BlockData) {
	included := make(map[types.Hash]struct{})
	for shard, batch := range block.Batches {
		for _, transaction := range batch.Transactions {
			included[transaction.ID()] = struct{}{}
		}
		if len(batch.Imports) > 0 {
			s.imports[shard] = nil
		}
	}
	s.mempoolInputs = make(map[types.ObjectID]types.Hash)
	for shard, transactions := range s.mempool {
		keep := transactions[:0]
		for _, transaction := range transactions {
			if _, ok := included[transaction.ID()]; ok {
				continue
			}
			keep = append(keep, transaction)
			for _, input := range transaction.Inputs {
				s.mempoolInputs[input.ObjectID] = transaction.ID()
			}
		}
		s.mempool[shard] = keep
	}
}

func (s *Service) recordFinalizedLocked(header interface{ CanonicalBytes() []byte }, certificate v2consensus.Certificate, commitments []interface{}) lightapi.Snapshot {
	panic("unreachable")
}

func (s *Service) LatestSnapshot() (lightapi.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return lightapi.Snapshot{}, ErrNoSnapshot
	}
	return cloneSnapshot(*s.latest), nil
}

func (s *Service) ShardState(shardID uint32) (worldstate.Backend, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, ok := s.Runtime.States[shardID]
	return store, ok
}

func sameProposalHeader(a, b interface{ CanonicalBytes() []byte }) bool {
	return bytes.Equal(a.CanonicalBytes(), b.CanonicalBytes())
}

func validatorID(key *ecdsa.PrivateKey) (types.ValidatorID, error) {
	if key == nil || key.Curve == nil {
		return types.ValidatorID{}, ErrNotValidator
	}
	public := ellipticMarshal(key)
	if len(public) != 65 {
		return types.ValidatorID{}, ErrNotValidator
	}
	return types.ValidatorIDFromPublicKey(public), nil
}

func ellipticMarshal(key *ecdsa.PrivateKey) []byte {
	return key.PublicKey.Curve.Params().NameBytes(key.PublicKey.X, key.PublicKey.Y)
}

func containsValidator(set v2consensus.ValidatorSet, id types.ValidatorID) bool {
	for _, validator := range set.Validators {
		if validator.ID == id {
			return true
		}
	}
	return false
}

func cloneSnapshot(snapshot lightapi.Snapshot) lightapi.Snapshot {
	out := snapshot
	out.Certificate.Votes = append([]v2consensus.Vote(nil), snapshot.Certificate.Votes...)
	out.Commitments = append(out.Commitments[:0:0], snapshot.Commitments...)
	out.Validators.Validators = append(out.Validators.Validators[:0:0], snapshot.Validators.Validators...)
	for i := range out.Validators.Validators {
		out.Validators.Validators[i].PublicKey = append([]byte(nil), snapshot.Validators.Validators[i].PublicKey...)
	}
	return out
}

func sortedPeers(peers []peer.ID) []peer.ID {
	out := append([]peer.ID(nil), peers...)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}
