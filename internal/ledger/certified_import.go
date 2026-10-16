package ledger

import (
	"errors"
	"sort"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
)

var (
	ErrCertifiedEvidenceInvalid = errors.New("invalid certified block evidence")
	ErrCertifiedEvidenceQuorum  = errors.New("certified block evidence does not reach quorum")
)

// CertifiedBlockEvidence carries signed consensus artifacts that can be used
// to independently prove that a block reached validator quorum. Sources may
// expose partial evidence; only the receiving node decides whether the merged
// signatures reach quorum.
type CertifiedBlockEvidence struct {
	Proposal consensus.Proposal `json:"proposal"`
	Votes    []consensus.Vote   `json:"votes"`
}

// CertifiedBlockEvidenceFragments returns valid proposal/vote fragments the
// node has retained for a height, even when it has not materialized the block
// locally yet. This is important during partition recovery: a validator may
// have cast a durable vote that contributed to finality without receiving the
// eventual block commit before the network split/healed.
func (s *Store) CertifiedBlockEvidenceFragments(height uint64) []CertifiedBlockEvidence {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return certifiedBlockEvidenceFragmentsFromState(s.snapshotLocked(), s.chainID, height)
}

// CertifiedBlockEvidenceAt keeps the committed-block-oriented collector used
// by tests and callers that want the evidence matching a local committed block.
func (s *Store) CertifiedBlockEvidenceAt(height uint64) (CertifiedBlockEvidence, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if height == 0 || height > uint64(len(s.blocks)) {
		return CertifiedBlockEvidence{}, false
	}
	state := s.snapshotLocked()
	block := state.Blocks[height-1]
	for _, fragment := range certifiedBlockEvidenceFragmentsFromState(state, s.chainID, height) {
		proposal := fragment.Proposal
		if proposal.BlockHash != block.Hash || proposal.StateRoot != block.StateRoot {
			continue
		}
		if matchProposalForBlock([]consensus.Proposal{proposal}, block) != nil {
			return fragment, true
		}
	}
	return CertifiedBlockEvidence{}, false
}

func certifiedBlockEvidenceFragmentsFromState(state persistedState, chainID string, height uint64) []CertifiedBlockEvidence {
	state = normalizeState(state)
	proposals := proposalsForHeight(state.Proposals, height)
	fragments := make([]CertifiedBlockEvidence, 0, len(proposals))
	for _, proposal := range proposals {
		if proposal.ValidateForChain(chainID) != nil {
			continue
		}
		if _, ok := validatorVotingPower(state.ValidatorSnapshot, proposal.Proposer); !ok {
			continue
		}

		seen := make(map[string]struct{})
		votes := make([]consensus.Vote, 0)
		for _, record := range state.Votes {
			vote := record.Vote
			if vote.Height != proposal.Height || vote.Round != proposal.Round || vote.BlockHash != proposal.BlockHash {
				continue
			}
			if _, duplicate := seen[vote.Voter]; duplicate {
				continue
			}
			if _, ok := validatorVotingPower(state.ValidatorSnapshot, vote.Voter); !ok {
				continue
			}
			if vote.ValidateForChain(chainID) != nil {
				continue
			}
			seen[vote.Voter] = struct{}{}
			votes = append(votes, vote)
		}
		if len(votes) == 0 {
			continue
		}
		sort.Slice(votes, func(i, j int) bool { return votes[i].Voter < votes[j].Voter })
		fragments = append(fragments, CertifiedBlockEvidence{
			Proposal: cloneProposal(proposal),
			Votes:    votes,
		})
	}

	sort.Slice(fragments, func(i, j int) bool {
		left := fragments[i].Proposal
		right := fragments[j].Proposal
		if left.Round != right.Round {
			return left.Round < right.Round
		}
		if left.BlockHash != right.BlockHash {
			return left.BlockHash < right.BlockHash
		}
		return left.Proposer < right.Proposer
	})
	return fragments
}

// ImportBlockWithEvidence imports the next block after independently
// validating a quorum of signed votes for its signed proposal. Matching valid
// votes already retained by the recovering node are combined with incoming
// signed evidence before quorum is evaluated.
func (s *Store) ImportBlockWithEvidence(block Block, evidence CertifiedBlockEvidence) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	if len(state.ValidatorSnapshot.Validators) == 0 {
		return ErrNoValidatorSet
	}

	nextState, err := importBlockIntoState(state, block, s.chainID)
	if err != nil {
		return err
	}

	evidenceState, err := attachCertifiedBlockEvidence(state, block, evidence, s.chainID)
	if err != nil {
		return err
	}

	nextState.Proposals = cloneProposals(evidenceState.Proposals)
	nextState.Votes = cloneVoteRecords(evidenceState.Votes)
	nextState.CommitCertificates = cloneCommitCertificates(evidenceState.CommitCertificates)
	nextState = completeConsensusActionsForHeightInState(nextState, block.Height, time.Now().UTC(), "certified block evidence imported")
	if err := s.writeState(nextState); err != nil {
		return err
	}
	s.applyStateLocked(nextState)
	return nil
}

func attachCertifiedBlockEvidence(state persistedState, block Block, evidence CertifiedBlockEvidence, chainID string) (persistedState, error) {
	state = normalizeState(state)
	proposal := cloneProposal(evidence.Proposal)
	if err := proposal.ValidateForChain(chainID); err != nil {
		return state, ErrCertifiedEvidenceInvalid
	}
	if proposal.Height != block.Height || proposal.BlockHash != block.Hash || proposal.StateRoot != block.StateRoot {
		return state, ErrCertifiedEvidenceInvalid
	}
	if matchProposalForBlock([]consensus.Proposal{proposal}, block) == nil {
		return state, ErrCertifiedEvidenceInvalid
	}
	if _, ok := validatorVotingPower(state.ValidatorSnapshot, proposal.Proposer); !ok {
		return state, ErrCertifiedEvidenceInvalid
	}
	if expected := proposerForHeightRound(state.ValidatorSnapshot.Validators, block.Height, proposal.Round); expected == "" || proposal.Proposer != expected {
		return state, ErrCertifiedEvidenceInvalid
	}

	for _, existing := range state.Proposals {
		if existing.Height != proposal.Height || existing.Round != proposal.Round {
			continue
		}
		if existing.BlockHash != proposal.BlockHash || existing.Proposer != proposal.Proposer {
			return state, ErrConflictingProposal
		}
	}

	quorum := quorumVotingPower(totalVotingPower(state.ValidatorSnapshot))
	if quorum == 0 {
		return state, ErrCertifiedEvidenceQuorum
	}

	seen := make(map[string]struct{}, len(state.Votes)+len(evidence.Votes))
	providedSeen := make(map[string]struct{}, len(evidence.Votes))
	voters := make([]string, 0, len(state.Votes)+len(evidence.Votes))
	validatedVotes := make([]VoteRecord, 0, len(evidence.Votes))
	var signedPower uint64

	for _, record := range state.Votes {
		vote := record.Vote
		if vote.Height != proposal.Height || vote.Round != proposal.Round || vote.BlockHash != proposal.BlockHash {
			continue
		}
		if _, duplicate := seen[vote.Voter]; duplicate {
			continue
		}
		if err := vote.ValidateForChain(chainID); err != nil {
			return state, ErrCertifiedEvidenceInvalid
		}
		power, ok := validatorVotingPower(state.ValidatorSnapshot, vote.Voter)
		if !ok {
			return state, ErrCertifiedEvidenceInvalid
		}
		nextPower, ok := addUint64(signedPower, power)
		if !ok {
			return state, ErrVotingPowerOverflow
		}
		signedPower = nextPower
		seen[vote.Voter] = struct{}{}
		voters = append(voters, vote.Voter)
	}

	for _, vote := range evidence.Votes {
		if _, duplicate := providedSeen[vote.Voter]; duplicate {
			return state, ErrCertifiedEvidenceInvalid
		}
		providedSeen[vote.Voter] = struct{}{}
		if err := vote.ValidateForChain(chainID); err != nil {
			return state, ErrCertifiedEvidenceInvalid
		}
		if vote.Height != proposal.Height || vote.Round != proposal.Round || vote.BlockHash != proposal.BlockHash {
			return state, ErrCertifiedEvidenceInvalid
		}
		power, ok := validatorVotingPower(state.ValidatorSnapshot, vote.Voter)
		if !ok {
			return state, ErrCertifiedEvidenceInvalid
		}
		if existingVote := findVoteByValidator(state.Votes, vote.Height, vote.Round, vote.Voter); existingVote != nil && existingVote.BlockHash != vote.BlockHash {
			return state, ErrConflictingVote
		}
		if _, alreadyCounted := seen[vote.Voter]; alreadyCounted {
			continue
		}
		nextPower, ok := addUint64(signedPower, power)
		if !ok {
			return state, ErrVotingPowerOverflow
		}
		signedPower = nextPower
		seen[vote.Voter] = struct{}{}
		voters = append(voters, vote.Voter)
		validatedVotes = append(validatedVotes, VoteRecord{Vote: vote, VotingPower: power, RecordedAt: time.Now().UTC()})
	}
	if signedPower < quorum {
		return state, ErrCertifiedEvidenceQuorum
	}
	sort.Strings(voters)

	proposalPresent := false
	for _, existing := range state.Proposals {
		if existing.Height == proposal.Height && existing.Round == proposal.Round && existing.BlockHash == proposal.BlockHash && existing.Proposer == proposal.Proposer {
			proposalPresent = true
			break
		}
	}
	if !proposalPresent {
		state.Proposals = append(state.Proposals, proposal)
	}

	for _, record := range validatedVotes {
		if findVoteByValidator(state.Votes, record.Vote.Height, record.Vote.Round, record.Vote.Voter) == nil {
			state.Votes = append(state.Votes, record)
		}
	}

	if findCertificate(state.CommitCertificates, proposal.Height, proposal.Round, proposal.BlockHash) == nil {
		state.CommitCertificates = append(state.CommitCertificates, CommitCertificate{
			Height:            proposal.Height,
			Round:             proposal.Round,
			BlockHash:         proposal.BlockHash,
			VotingPower:       signedPower,
			QuorumVotingPower: quorum,
			VoterCount:        len(voters),
			Voters:            voters,
			CreatedAt:         time.Now().UTC(),
		})
	}
	return normalizeState(state), nil
}
