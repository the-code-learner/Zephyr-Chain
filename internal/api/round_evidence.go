package api

import (
	"sort"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

type RoundEvidence struct {
	Height                       uint64             `json:"height"`
	Round                        uint64             `json:"round"`
	State                        string             `json:"state"`
	StartedAt                    *time.Time         `json:"startedAt,omitempty"`
	DeadlineAt                   *time.Time         `json:"deadlineAt,omitempty"`
	TimedOut                     bool               `json:"timedOut"`
	NextProposer                 string             `json:"nextProposer,omitempty"`
	QuorumVotingPower            uint64             `json:"quorumVotingPower"`
	ProposalPresent              bool               `json:"proposalPresent"`
	ProposalBlockHash            string             `json:"proposalBlockHash,omitempty"`
	ProposalProposer             string             `json:"proposalProposer,omitempty"`
	LatestKnownProposalRound     *uint64            `json:"latestKnownProposalRound,omitempty"`
	LatestKnownProposalBlockHash string             `json:"latestKnownProposalBlockHash,omitempty"`
	VoteTallies                  []ledger.VoteTally `json:"voteTallies"`
	LeadingVoteBlockHash         string             `json:"leadingVoteBlockHash,omitempty"`
	LeadingVotePower             uint64             `json:"leadingVotePower"`
	LeadingVoteCount             int                `json:"leadingVoteCount"`
	QuorumRemaining              uint64             `json:"quorumRemaining"`
	PartialQuorum                bool               `json:"partialQuorum"`
	LocalVotePresent             bool               `json:"localVotePresent"`
	LocalVoteBlockHash           string             `json:"localVoteBlockHash,omitempty"`
	PendingReplayCount           int                `json:"pendingReplayCount"`
	PendingReplayRounds          []uint64           `json:"pendingReplayRounds"`
	CertificatePresent           bool               `json:"certificatePresent"`
	CertificateBlockHash         string             `json:"certificateBlockHash,omitempty"`
	Warnings                     []string           `json:"warnings"`
}

func (s *Server) buildRoundEvidence(now time.Time) RoundEvidence {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	consensusView := s.ledger.Consensus()
	roundState := s.ledger.RoundState()
	recovery := s.ledger.ConsensusRecovery()
	evidence := RoundEvidence{
		Height:              consensusView.NextHeight,
		Round:               consensusView.CurrentRound,
		State:               "idle",
		NextProposer:        consensusView.NextProposer,
		QuorumVotingPower:   consensusView.QuorumVotingPower,
		VoteTallies:         s.ledger.VoteTalliesAt(consensusView.NextHeight, consensusView.CurrentRound),
		QuorumRemaining:     consensusView.QuorumVotingPower,
		PendingReplayRounds: make([]uint64, 0),
		Warnings:            make([]string, 0),
	}

	if consensusView.ValidatorCount == 0 {
		evidence.State = "no_validator_set"
		return evidence
	}
	if !roundState.StartedAt.IsZero() {
		startedAt := roundState.StartedAt
		evidence.StartedAt = &startedAt
		if s.config.ConsensusRoundTimeout > 0 {
			deadlineAt := roundState.StartedAt.Add(s.config.ConsensusRoundTimeout)
			evidence.DeadlineAt = &deadlineAt
			evidence.TimedOut = !now.Before(deadlineAt)
		}
	}

	currentProposal, hasCurrentProposal := s.ledger.ProposalAt(consensusView.NextHeight, consensusView.CurrentRound)
	if hasCurrentProposal {
		evidence.ProposalPresent = true
		evidence.ProposalBlockHash = currentProposal.BlockHash
		evidence.ProposalProposer = currentProposal.Proposer
	}
	if latestProposal, hasLatestProposal := s.ledger.LatestProposalForHeight(consensusView.NextHeight); hasLatestProposal && latestProposal.Round != consensusView.CurrentRound {
		round := latestProposal.Round
		evidence.LatestKnownProposalRound = &round
		evidence.LatestKnownProposalBlockHash = latestProposal.BlockHash
	}
	if len(evidence.VoteTallies) > 0 {
		leading := evidence.VoteTallies[0]
		evidence.LeadingVoteBlockHash = leading.BlockHash
		evidence.LeadingVotePower = leading.VotingPower
		evidence.LeadingVoteCount = leading.VoteCount
		if leading.VotingPower >= consensusView.QuorumVotingPower {
			evidence.QuorumRemaining = 0
		} else {
			evidence.QuorumRemaining = consensusView.QuorumVotingPower - leading.VotingPower
			evidence.PartialQuorum = leading.VotingPower > 0
		}
	}
	if s.config.ValidatorAddress != "" {
		if localVote, hasLocalVote := s.ledger.VoteAt(consensusView.NextHeight, consensusView.CurrentRound, s.config.ValidatorAddress); hasLocalVote {
			evidence.LocalVotePresent = true
			evidence.LocalVoteBlockHash = localVote.BlockHash
		}
	}

	if hasCurrentProposal {
		if certificate, ok := s.ledger.Certificate(currentProposal.Height, currentProposal.Round, currentProposal.BlockHash); ok {
			evidence.CertificatePresent = true
			evidence.CertificateBlockHash = certificate.BlockHash
		}
	} else if certificate, ok := s.ledger.LatestCertificateForHeightRound(consensusView.NextHeight, consensusView.CurrentRound); ok {
		evidence.CertificatePresent = true
		evidence.CertificateBlockHash = certificate.BlockHash
	}

	pendingRounds := make(map[uint64]struct{})
	for _, action := range recovery.PendingActions {
		if action.Height != consensusView.NextHeight {
			continue
		}
		evidence.PendingReplayCount++
		pendingRounds[action.Round] = struct{}{}
	}
	for round := range pendingRounds {
		evidence.PendingReplayRounds = append(evidence.PendingReplayRounds, round)
	}
	sort.Slice(evidence.PendingReplayRounds, func(i, j int) bool {
		return evidence.PendingReplayRounds[i] < evidence.PendingReplayRounds[j]
	})

	switch {
	case evidence.CertificatePresent:
		evidence.State = "certified"
	case evidence.ProposalPresent:
		evidence.State = "collecting_votes"
	case evidence.LatestKnownProposalRound != nil:
		evidence.State = "waiting_for_reproposal"
	case s.ledger.MempoolSize() > 0:
		evidence.State = "waiting_for_proposal"
	default:
		evidence.State = "idle"
	}

	if evidence.TimedOut && !evidence.CertificatePresent {
		evidence.Warnings = append(evidence.Warnings, "timeout_elapsed")
	}
	if evidence.PartialQuorum {
		evidence.Warnings = append(evidence.Warnings, "partial_quorum")
	}
	if evidence.LatestKnownProposalRound != nil && !evidence.ProposalPresent {
		evidence.Warnings = append(evidence.Warnings, "reproposal_pending")
	}
	if evidence.PendingReplayCount > 0 {
		evidence.Warnings = append(evidence.Warnings, "replay_pending")
	}
	if evidence.ProposalPresent && evidence.NextProposer != "" && evidence.ProposalProposer != evidence.NextProposer {
		evidence.Warnings = append(evidence.Warnings, "proposal_not_from_scheduled_proposer")
	}

	return evidence
}
