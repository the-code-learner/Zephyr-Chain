package api

import (
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
	ProposalPresent              bool               `json:"proposalPresent"`
	ProposalBlockHash            string             `json:"proposalBlockHash,omitempty"`
	ProposalProposer             string             `json:"proposalProposer,omitempty"`
	LatestKnownProposalRound     *uint64            `json:"latestKnownProposalRound,omitempty"`
	LatestKnownProposalBlockHash string             `json:"latestKnownProposalBlockHash,omitempty"`
	VoteTallies                  []ledger.VoteTally `json:"voteTallies"`
	LocalVotePresent             bool               `json:"localVotePresent"`
	LocalVoteBlockHash           string             `json:"localVoteBlockHash,omitempty"`
	CertificatePresent           bool               `json:"certificatePresent"`
	CertificateBlockHash         string             `json:"certificateBlockHash,omitempty"`
}

func (s *Server) buildRoundEvidence(now time.Time) RoundEvidence {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	consensusView := s.ledger.Consensus()
	roundState := s.ledger.RoundState()
	evidence := RoundEvidence{
		Height:       consensusView.NextHeight,
		Round:        consensusView.CurrentRound,
		State:        "idle",
		NextProposer: consensusView.NextProposer,
		VoteTallies:  s.ledger.VoteTalliesAt(consensusView.NextHeight, consensusView.CurrentRound),
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

	return evidence
}
