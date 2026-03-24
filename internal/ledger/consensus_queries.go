package ledger

import (
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
)

func (s *Store) ProposalAt(height uint64, round uint64) (*consensus.Proposal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	proposal := findProposal(s.proposals, height, round)
	if proposal == nil {
		return nil, false
	}
	return proposal, true
}

func (s *Store) LatestProposalForHeight(height uint64) (*consensus.Proposal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	proposal := latestProposalForHeight(s.proposals, height)
	if proposal == nil {
		return nil, false
	}
	return proposal, true
}

func (s *Store) VoteAt(height uint64, round uint64, voter string) (*consensus.Vote, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vote := findVoteByValidator(s.votes, height, round, voter)
	if vote == nil {
		return nil, false
	}
	return vote, true
}

func (s *Store) LatestVoteByValidatorForHeight(height uint64, voter string) (*consensus.Vote, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vote := latestVoteByValidatorForHeight(s.votes, height, voter)
	if vote == nil {
		return nil, false
	}
	return vote, true
}

func (s *Store) HasVote(height uint64, round uint64, voter string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return hasVoteFromValidator(s.votes, height, round, voter)
}

func (s *Store) VoteTalliesAt(height uint64, round uint64) []VoteTally {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return voteTalliesForHeightRound(s.votes, height, round, quorumVotingPower(totalVotingPower(s.validatorSnapshot)))
}

func (s *Store) Certificate(height uint64, round uint64, blockHash string) (*CommitCertificate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	certificate := findCertificate(s.commitCertificates, height, round, blockHash)
	if certificate == nil {
		return nil, false
	}
	return certificate, true
}

func (s *Store) LatestCertificateForHeightRound(height uint64, round uint64) (*CommitCertificate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	certificate := latestCertificateForHeightRound(s.commitCertificates, height, round)
	if certificate == nil {
		return nil, false
	}
	return certificate, true
}

func (s *Store) RoundState() ConsensusRoundState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneConsensusRoundState(s.roundState)
}

func (s *Store) EnsureRoundStarted(now time.Time) (ConsensusRoundState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	if len(state.ValidatorSnapshot.Validators) == 0 {
		return cloneConsensusRoundState(state.RoundState), ErrNoValidatorSet
	}
	if !state.RoundState.StartedAt.IsZero() {
		return cloneConsensusRoundState(state.RoundState), nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state.RoundState.StartedAt = now.UTC()
	if err := s.writeState(state); err != nil {
		return ConsensusRoundState{}, err
	}

	s.applyStateLocked(state)
	return cloneConsensusRoundState(s.roundState), nil
}

func (s *Store) AdvanceRound(now time.Time) (ConsensusRoundState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	if len(state.ValidatorSnapshot.Validators) == 0 {
		return cloneConsensusRoundState(state.RoundState), ErrNoValidatorSet
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state.RoundState.Round++
	state.RoundState.StartedAt = now.UTC()
	if err := s.writeState(state); err != nil {
		return ConsensusRoundState{}, err
	}

	s.applyStateLocked(state)
	return cloneConsensusRoundState(s.roundState), nil
}
