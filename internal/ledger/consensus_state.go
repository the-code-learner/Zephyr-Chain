package ledger

import (
	"errors"
	"sort"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var (
	ErrNoValidatorSet               = errors.New("no validator set configured")
	ErrValidatorNotActive           = errors.New("validator is not part of the active validator set")
	ErrUnexpectedProposer           = errors.New("proposal proposer is not scheduled for this height")
	ErrConsensusHeightMismatch      = errors.New("consensus message height does not match the next block height")
	ErrConsensusPreviousHash        = errors.New("proposal previous hash does not match the current chain tip")
	ErrConflictingProposal          = errors.New("conflicting proposal already recorded for this height and round")
	ErrUnknownProposal              = errors.New("vote references an unknown proposal")
	ErrConflictingVote              = errors.New("validator already voted for a different block at this height and round")
	ErrConsensusProposalRequired    = errors.New("block is missing a matching proposal")
	ErrConsensusTemplateMismatch    = errors.New("block template does not match the stored proposal set")
	ErrConsensusCertificateRequired = errors.New("block is missing a matching quorum certificate")
)

type VoteRecord struct {
	Vote        consensus.Vote `json:"vote"`
	VotingPower uint64         `json:"votingPower"`
	RecordedAt  time.Time      `json:"recordedAt"`
}

type CommitCertificate struct {
	Height            uint64    `json:"height"`
	Round             uint64    `json:"round"`
	BlockHash         string    `json:"blockHash"`
	VotingPower       uint64    `json:"votingPower"`
	QuorumVotingPower uint64    `json:"quorumVotingPower"`
	VoterCount        int       `json:"voterCount"`
	Voters            []string  `json:"voters"`
	CreatedAt         time.Time `json:"createdAt"`
}

type VoteTally struct {
	Height        uint64 `json:"height"`
	Round         uint64 `json:"round"`
	BlockHash     string `json:"blockHash"`
	VoteCount     int    `json:"voteCount"`
	VotingPower   uint64 `json:"votingPower"`
	QuorumReached bool   `json:"quorumReached"`
}

type ConsensusArtifactsView struct {
	LatestProposal    *consensus.Proposal `json:"latestProposal,omitempty"`
	LatestCertificate *CommitCertificate  `json:"latestCertificate,omitempty"`
	VoteTallies       []VoteTally         `json:"voteTallies"`
	ProposalCount     int                 `json:"proposalCount"`
	VoteCount         int                 `json:"voteCount"`
	CertificateCount  int                 `json:"certificateCount"`
}

func (s *Store) ConsensusArtifacts() ConsensusArtifactsView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return consensusArtifactsFromState(s.snapshotLocked())
}

func (s *Store) RecordProposal(proposal consensus.Proposal) error {
	return s.RecordProposalWithAction(proposal, nil)
}

func (s *Store) RecordProposalWithAction(proposal consensus.Proposal, action *ConsensusAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	nextState, err := recordProposalIntoState(state, proposal)
	if err != nil {
		return err
	}
	if action != nil {
		nextState = recordConsensusActionIntoState(nextState, *action)
	}
	if err := s.writeState(nextState); err != nil {
		return err
	}

	s.applyStateLocked(nextState)
	return nil
}

func (s *Store) RecordVote(vote consensus.Vote) (VoteTally, *CommitCertificate, error) {
	return s.RecordVoteWithAction(vote, nil)
}

func (s *Store) RecordVoteWithAction(vote consensus.Vote, action *ConsensusAction) (VoteTally, *CommitCertificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	nextState, tally, certificate, err := recordVoteIntoState(state, vote)
	if err != nil {
		return VoteTally{}, nil, err
	}
	if action != nil {
		nextState = recordConsensusActionIntoState(nextState, *action)
	}
	if err := s.writeState(nextState); err != nil {
		return VoteTally{}, nil, err
	}

	s.applyStateLocked(nextState)
	return tally, certificate, nil
}

func consensusArtifactsFromState(state persistedState) ConsensusArtifactsView {
	state = normalizeState(state)
	view := ConsensusArtifactsView{
		VoteTallies:      make([]VoteTally, 0),
		ProposalCount:    len(state.Proposals),
		VoteCount:        len(state.Votes),
		CertificateCount: len(state.CommitCertificates),
	}
	if len(state.Proposals) > 0 {
		latestProposal := cloneProposal(state.Proposals[len(state.Proposals)-1])
		view.LatestProposal = &latestProposal
		view.VoteTallies = voteTalliesForHeightRound(state.Votes, latestProposal.Height, latestProposal.Round, quorumVotingPower(totalVotingPower(state.ValidatorSnapshot)))
	}
	if len(state.CommitCertificates) > 0 {
		latestCertificate := cloneCommitCertificate(state.CommitCertificates[len(state.CommitCertificates)-1])
		view.LatestCertificate = &latestCertificate
	}
	return view
}

func recordProposalIntoState(state persistedState, proposal consensus.Proposal) (persistedState, error) {
	state = normalizeState(state)
	if proposal.ProposedAt.IsZero() {
		proposal.ProposedAt = time.Now().UTC()
	}
	if err := proposal.ValidateStatic(); err != nil {
		return state, err
	}
	if len(state.ValidatorSnapshot.Validators) == 0 {
		return state, ErrNoValidatorSet
	}
	if proposal.Height != uint64(len(state.Blocks))+1 {
		return state, ErrConsensusHeightMismatch
	}
	if proposal.PreviousHash != previousHashForHeight(state.Blocks, proposal.Height) {
		return state, ErrConsensusPreviousHash
	}
	if _, ok := validatorVotingPower(state.ValidatorSnapshot, proposal.Proposer); !ok {
		return state, ErrValidatorNotActive
	}
	var err error
	state, err = alignRoundStateWithProposal(state, proposal)
	if err != nil {
		return state, err
	}
	if expected := proposerForHeightRound(state.ValidatorSnapshot.Validators, proposal.Height, proposal.Round); expected != "" && proposal.Proposer != expected {
		return state, ErrUnexpectedProposer
	}
	for index, existing := range state.Proposals {
		if existing.Height != proposal.Height || existing.Round != proposal.Round {
			continue
		}
		if existing.BlockHash == proposal.BlockHash && existing.Proposer == proposal.Proposer {
			if !equalStrings(existing.TransactionIDs, proposal.TransactionIDs) || !equalTransactions(existing.Transactions, proposal.Transactions) {
				state.Proposals[index] = cloneProposal(proposal)
				state = normalizeState(state)
			}
			return state, nil
		}
		return state, ErrConflictingProposal
	}

	state.Proposals = append(state.Proposals, proposal)
	state = normalizeState(state)
	return state, nil
}

func recordVoteIntoState(state persistedState, vote consensus.Vote) (persistedState, VoteTally, *CommitCertificate, error) {
	state = normalizeState(state)
	if vote.VotedAt.IsZero() {
		vote.VotedAt = time.Now().UTC()
	}
	if err := vote.ValidateStatic(); err != nil {
		return state, VoteTally{}, nil, err
	}
	if len(state.ValidatorSnapshot.Validators) == 0 {
		return state, VoteTally{}, nil, ErrNoValidatorSet
	}
	if vote.Height != uint64(len(state.Blocks))+1 {
		return state, VoteTally{}, nil, ErrConsensusHeightMismatch
	}
	var err error
	state, err = alignRoundStateWithVote(state, vote)
	if err != nil {
		return state, VoteTally{}, nil, err
	}
	votingPower, ok := validatorVotingPower(state.ValidatorSnapshot, vote.Voter)
	if !ok {
		return state, VoteTally{}, nil, ErrValidatorNotActive
	}
	if !hasProposal(state.Proposals, vote.Height, vote.Round, vote.BlockHash) {
		return state, VoteTally{}, nil, ErrUnknownProposal
	}

	for _, existing := range state.Votes {
		if existing.Vote.Height != vote.Height || existing.Vote.Round != vote.Round || existing.Vote.Voter != vote.Voter {
			continue
		}
		if existing.Vote.BlockHash == vote.BlockHash {
			tally := tallyForVotes(state.Votes, vote.Height, vote.Round, vote.BlockHash, quorumVotingPower(totalVotingPower(state.ValidatorSnapshot)))
			return state, tally, findCertificate(state.CommitCertificates, vote.Height, vote.Round, vote.BlockHash), nil
		}
		return state, VoteTally{}, nil, ErrConflictingVote
	}

	state.Votes = append(state.Votes, VoteRecord{
		Vote:        vote,
		VotingPower: votingPower,
		RecordedAt:  time.Now().UTC(),
	})

	quorum := quorumVotingPower(totalVotingPower(state.ValidatorSnapshot))
	tally := tallyForVotes(state.Votes, vote.Height, vote.Round, vote.BlockHash, quorum)
	certificate := findCertificate(state.CommitCertificates, vote.Height, vote.Round, vote.BlockHash)
	if certificate == nil && tally.QuorumReached {
		voters := votersForBlock(state.Votes, vote.Height, vote.Round, vote.BlockHash)
		created := CommitCertificate{
			Height:            vote.Height,
			Round:             vote.Round,
			BlockHash:         vote.BlockHash,
			VotingPower:       tally.VotingPower,
			QuorumVotingPower: quorum,
			VoterCount:        len(voters),
			Voters:            voters,
			CreatedAt:         time.Now().UTC(),
		}
		state.CommitCertificates = append(state.CommitCertificates, created)
		certificate = &created
	}

	state = normalizeState(state)
	return state, tally, certificate, nil
}

func totalVotingPower(snapshot ValidatorSnapshot) uint64 {
	snapshot = normalizeValidatorSnapshot(snapshot)
	var total uint64
	for _, validator := range snapshot.Validators {
		total += validator.VotingPower
	}
	return total
}

func validatorVotingPower(snapshot ValidatorSnapshot, address string) (uint64, bool) {
	snapshot = normalizeValidatorSnapshot(snapshot)
	for _, validator := range snapshot.Validators {
		if validator.Address == address {
			return validator.VotingPower, true
		}
	}
	return 0, false
}

func hasProposal(proposals []consensus.Proposal, height uint64, round uint64, blockHash string) bool {
	for _, proposal := range proposals {
		if proposal.Height == height && proposal.Round == round && proposal.BlockHash == blockHash {
			return true
		}
	}
	return false
}

func tallyForVotes(votes []VoteRecord, height uint64, round uint64, blockHash string, quorum uint64) VoteTally {
	tally := VoteTally{Height: height, Round: round, BlockHash: blockHash}
	for _, vote := range votes {
		if vote.Vote.Height != height || vote.Vote.Round != round || vote.Vote.BlockHash != blockHash {
			continue
		}
		tally.VoteCount++
		tally.VotingPower += vote.VotingPower
	}
	tally.QuorumReached = tally.VotingPower >= quorum && quorum > 0
	return tally
}

func voteTalliesForHeightRound(votes []VoteRecord, height uint64, round uint64, quorum uint64) []VoteTally {
	byBlock := make(map[string]VoteTally)
	for _, vote := range votes {
		if vote.Vote.Height != height || vote.Vote.Round != round {
			continue
		}
		tally := byBlock[vote.Vote.BlockHash]
		tally.Height = height
		tally.Round = round
		tally.BlockHash = vote.Vote.BlockHash
		tally.VoteCount++
		tally.VotingPower += vote.VotingPower
		byBlock[vote.Vote.BlockHash] = tally
	}

	tallies := make([]VoteTally, 0, len(byBlock))
	for _, tally := range byBlock {
		tally.QuorumReached = tally.VotingPower >= quorum && quorum > 0
		tallies = append(tallies, tally)
	}
	sort.Slice(tallies, func(i, j int) bool {
		if tallies[i].VotingPower != tallies[j].VotingPower {
			return tallies[i].VotingPower > tallies[j].VotingPower
		}
		if tallies[i].VoteCount != tallies[j].VoteCount {
			return tallies[i].VoteCount > tallies[j].VoteCount
		}
		return tallies[i].BlockHash < tallies[j].BlockHash
	})
	return tallies
}

func votersForBlock(votes []VoteRecord, height uint64, round uint64, blockHash string) []string {
	voters := make([]string, 0)
	for _, vote := range votes {
		if vote.Vote.Height == height && vote.Vote.Round == round && vote.Vote.BlockHash == blockHash {
			voters = append(voters, vote.Vote.Voter)
		}
	}
	sort.Strings(voters)
	return voters
}

func alignRoundStateWithProposal(state persistedState, proposal consensus.Proposal) (persistedState, error) {
	currentRound := state.RoundState.Round
	switch {
	case proposal.Round < currentRound:
		return state, ErrConsensusRoundMismatch
	case proposal.Round > currentRound:
		state.RoundState.Round = proposal.Round
		state.RoundState.StartedAt = proposal.ProposedAt.UTC()
	case state.RoundState.StartedAt.IsZero():
		state.RoundState.StartedAt = proposal.ProposedAt.UTC()
	}
	return state, nil
}

func alignRoundStateWithVote(state persistedState, vote consensus.Vote) (persistedState, error) {
	currentRound := state.RoundState.Round
	switch {
	case vote.Round < currentRound:
		return state, ErrConsensusRoundMismatch
	case vote.Round > currentRound:
		state.RoundState.Round = vote.Round
		state.RoundState.StartedAt = vote.VotedAt.UTC()
	case state.RoundState.StartedAt.IsZero():
		state.RoundState.StartedAt = vote.VotedAt.UTC()
	}
	return state, nil
}

func findCertificate(certificates []CommitCertificate, height uint64, round uint64, blockHash string) *CommitCertificate {
	for _, certificate := range certificates {
		if certificate.Height == height && certificate.Round == round && certificate.BlockHash == blockHash {
			cloned := cloneCommitCertificate(certificate)
			return &cloned
		}
	}
	return nil
}

func matchProposalForBlock(proposals []consensus.Proposal, block Block) *consensus.Proposal {
	for _, proposal := range proposals {
		if proposal.Height != block.Height || proposal.BlockHash != block.Hash {
			continue
		}
		if proposal.PreviousHash != block.PreviousHash {
			continue
		}
		if !proposal.ProducedAt.Equal(block.ProducedAt) {
			continue
		}
		if !equalStrings(proposal.TransactionIDs, block.TransactionIDs) {
			continue
		}
		if !equalTransactions(proposal.Transactions, block.Transactions) {
			continue
		}
		cloned := cloneProposal(proposal)
		return &cloned
	}
	return nil
}

func matchCertificateForBlock(certificates []CommitCertificate, block Block) *CommitCertificate {
	for _, certificate := range certificates {
		if certificate.Height == block.Height && certificate.BlockHash == block.Hash {
			cloned := cloneCommitCertificate(certificate)
			return &cloned
		}
	}
	return nil
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalTransactions(left []tx.Envelope, right []tx.Envelope) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func previousHashForHeight(blocks []Block, height uint64) string {
	if height <= 1 || len(blocks) == 0 {
		return ""
	}
	return blocks[len(blocks)-1].Hash
}

func cloneProposal(proposal consensus.Proposal) consensus.Proposal {
	cloned := proposal
	cloned.TransactionIDs = append([]string(nil), proposal.TransactionIDs...)
	cloned.Transactions = append([]tx.Envelope(nil), proposal.Transactions...)
	return cloned
}

func cloneProposals(proposals []consensus.Proposal) []consensus.Proposal {
	cloned := make([]consensus.Proposal, len(proposals))
	for i, proposal := range proposals {
		cloned[i] = cloneProposal(proposal)
	}
	return cloned
}

func cloneVoteRecord(record VoteRecord) VoteRecord {
	return VoteRecord{
		Vote:        record.Vote,
		VotingPower: record.VotingPower,
		RecordedAt:  record.RecordedAt,
	}
}

func cloneVoteRecords(records []VoteRecord) []VoteRecord {
	cloned := make([]VoteRecord, len(records))
	for i, record := range records {
		cloned[i] = cloneVoteRecord(record)
	}
	return cloned
}

func cloneCommitCertificate(certificate CommitCertificate) CommitCertificate {
	cloned := certificate
	cloned.Voters = append([]string(nil), certificate.Voters...)
	return cloned
}

func cloneCommitCertificates(certificates []CommitCertificate) []CommitCertificate {
	cloned := make([]CommitCertificate, len(certificates))
	for i, certificate := range certificates {
		cloned[i] = cloneCommitCertificate(certificate)
	}
	return cloned
}

func normalizeVoteTallies(tallies []VoteTally) []VoteTally {
	if tallies == nil {
		return make([]VoteTally, 0)
	}
	cloned := make([]VoteTally, len(tallies))
	copy(cloned, tallies)
	return cloned
}
