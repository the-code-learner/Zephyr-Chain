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
// to independently prove that a committed block reached validator quorum.
//
// A transport source may return only the valid proposal/vote fragment it has
// retained for the committed block. The receiving node is the authority that
// reconstructs voting power and requires quorum before importing the block.
// CommitCertificate is deliberately not trusted as a transport proof because
// it contains derived metadata rather than validator signatures.
type CertifiedBlockEvidence struct {
	Proposal consensus.Proposal `json:"proposal"`
	Votes    []consensus.Vote   `json:"votes"`
}

// CertifiedBlockEvidenceAt returns the signed proposal and every valid signed
// vote this node retained for an already committed block. It intentionally
// does not require a locally persisted derived CommitCertificate: a node that
// received/imported a committed block may retain useful signed evidence even
// when that derived artifact is absent. The receiving node still requires a
// full quorum before ImportBlockWithEvidence can mutate state.
func (s *Store) CertifiedBlockEvidenceAt(height uint64) (CertifiedBlockEvidence, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if height == 0 || height > uint64(len(s.blocks)) {
		return CertifiedBlockEvidence{}, false
	}
	state := s.snapshotLocked()
	block := state.Blocks[height-1]
	proposal := matchProposalForBlock(proposalsForHeight(state.Proposals, height), block)
	if proposal == nil || proposal.ValidateForChain(s.chainID) != nil {
		return CertifiedBlockEvidence{}, false
	}

	seen := make(map[string]struct{})
	votes := make([]consensus.Vote, 0)
	for _, record := range state.Votes {
		vote := record.Vote
		if vote.Height != height || vote.Round != proposal.Round || vote.BlockHash != block.Hash {
			continue
		}
		if _, duplicate := seen[vote.Voter]; duplicate {
			continue
		}
		if _, ok := validatorVotingPower(state.ValidatorSnapshot, vote.Voter); !ok {
			continue
		}
		if vote.ValidateForChain(s.chainID) != nil {
			continue
		}
		seen[vote.Voter] = struct{}{}
		votes = append(votes, vote)
	}
	sort.Slice(votes, func(i, j int) bool { return votes[i].Voter < votes[j].Voter })
	if len(votes) == 0 {
		return CertifiedBlockEvidence{}, false
	}
	return CertifiedBlockEvidence{Proposal: cloneProposal(*proposal), Votes: votes}, true
}

// ImportBlockWithEvidence imports the next block after independently
// validating a quorum of signed votes for its signed proposal. This is the
// catch-up path for a validator that contributed to (or missed) finality but
// did not receive the committed block before peers advanced to the next
// height.
func (s *Store) ImportBlockWithEvidence(block Block, evidence CertifiedBlockEvidence) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	if len(state.ValidatorSnapshot.Validators) == 0 {
		return ErrNoValidatorSet
	}

	// Execute and validate the block against local committed state first. This
	// checks chain continuity, transactions, balances/nonces, state root and
	// block hash without mutating the live store.
	nextState, err := importBlockIntoState(state, block, s.chainID)
	if err != nil {
		return err
	}

	evidenceState, err := attachCertifiedBlockEvidence(state, block, evidence, s.chainID)
	if err != nil {
		return err
	}

	// Preserve the independently validated historical consensus evidence while
	// taking the account/mempool/block transition from importBlockIntoState.
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
	seen := make(map[string]struct{}, len(evidence.Votes))
	voters := make([]string, 0, len(evidence.Votes))
	var signedPower uint64
	validatedVotes := make([]VoteRecord, 0, len(evidence.Votes))
	for _, vote := range evidence.Votes {
		if err := vote.ValidateForChain(chainID); err != nil {
			return state, ErrCertifiedEvidenceInvalid
		}
		if vote.Height != proposal.Height || vote.Round != proposal.Round || vote.BlockHash != proposal.BlockHash {
			return state, ErrCertifiedEvidenceInvalid
		}
		if _, duplicate := seen[vote.Voter]; duplicate {
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
		existingVote := findVoteByValidator(state.Votes, record.Vote.Height, record.Vote.Round, record.Vote.Voter)
		if existingVote != nil {
			if existingVote.BlockHash != record.Vote.BlockHash {
				return state, ErrConflictingVote
			}
			continue
		}
		state.Votes = append(state.Votes, record)
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
