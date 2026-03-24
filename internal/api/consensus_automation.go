package api

import (
	"errors"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func (s *Server) startConsensusAutomation() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.config.ConsensusInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := s.runConsensusAutomation(); err != nil && !ignoreConsensusAutomationError(err) {
					recordPeerLog("consensus-automation", err)
				}
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *Server) runConsensusAutomation() error {
	if s.identitySigner == nil || s.config.ValidatorAddress == "" {
		return nil
	}
	if !isActiveValidator(s.ledger.ValidatorSet(), s.config.ValidatorAddress) {
		return nil
	}

	now := time.Now().UTC()
	if _, err := s.ledger.EnsureRoundStarted(now); err != nil && !errors.Is(err, ledger.ErrNoValidatorSet) {
		return err
	}
	if err := s.maybeAdvanceConsensusRound(now); err != nil {
		return err
	}
	if err := s.maybeAutomateProposal(now); err != nil {
		return err
	}
	if err := s.maybeAutomateVote(now); err != nil {
		return err
	}
	if err := s.maybeAutomateCommit(); err != nil {
		return err
	}
	return nil
}

func (s *Server) maybeAdvanceConsensusRound(now time.Time) error {
	if s.config.ConsensusRoundTimeout <= 0 {
		return nil
	}

	consensusView := s.ledger.Consensus()
	if consensusView.ValidatorCount == 0 {
		return nil
	}

	roundState := s.ledger.RoundState()
	if roundState.StartedAt.IsZero() {
		return nil
	}
	if now.Before(roundState.StartedAt.Add(s.config.ConsensusRoundTimeout)) {
		return nil
	}

	proposal, proposalExists := s.ledger.ProposalAt(consensusView.NextHeight, consensusView.CurrentRound)
	if proposalExists {
		if _, exists := s.ledger.Certificate(proposal.Height, proposal.Round, proposal.BlockHash); exists {
			return nil
		}
	}

	hasPendingWork := proposalExists || s.ledger.MempoolSize() > 0
	if !hasPendingWork {
		_, hasPendingWork = s.ledger.LatestProposalForHeight(consensusView.NextHeight)
	}
	if !hasPendingWork {
		return nil
	}

	_, err := s.ledger.AdvanceRound(now)
	return err
}

func (s *Server) maybeAutomateProposal(now time.Time) error {
	if !s.config.EnableBlockProduction {
		return nil
	}

	consensusView := s.ledger.Consensus()
	if consensusView.ValidatorCount == 0 || consensusView.NextProposer == "" || consensusView.NextProposer != s.config.ValidatorAddress {
		return nil
	}
	if _, exists := s.ledger.ProposalAt(consensusView.NextHeight, consensusView.CurrentRound); exists {
		return nil
	}

	proposal := consensus.Proposal{
		Height: consensusView.NextHeight,
		Round:  consensusView.CurrentRound,
	}
	if previousProposal, exists := s.ledger.LatestProposalForHeight(consensusView.NextHeight); exists && previousProposal.Round < consensusView.CurrentRound {
		proposal.BlockHash = previousProposal.BlockHash
		proposal.PreviousHash = previousProposal.PreviousHash
		proposal.ProducedAt = previousProposal.ProducedAt
		proposal.TransactionIDs = append([]string(nil), previousProposal.TransactionIDs...)
		proposal.Transactions = append([]tx.Envelope(nil), previousProposal.Transactions...)
	} else {
		block, err := s.ledger.BuildNextBlock(s.config.MaxTransactionsPerBlock, now)
		if err != nil {
			return err
		}
		proposal.BlockHash = block.Hash
		proposal.PreviousHash = block.PreviousHash
		proposal.ProducedAt = block.ProducedAt
		proposal.TransactionIDs = append([]string(nil), block.TransactionIDs...)
		proposal.Transactions = append([]tx.Envelope(nil), block.Transactions...)
	}

	signedProposal, err := s.identitySigner.SignProposal(proposal, now)
	if err != nil {
		return err
	}
	if err := s.ledger.RecordProposal(signedProposal); err != nil {
		return err
	}

	go s.broadcastProposal(signedProposal)
	return nil
}

func (s *Server) maybeAutomateVote(now time.Time) error {
	consensusView := s.ledger.Consensus()
	if consensusView.ValidatorCount == 0 {
		return nil
	}

	proposal, exists := s.ledger.ProposalAt(consensusView.NextHeight, consensusView.CurrentRound)
	if !exists {
		return nil
	}
	if s.ledger.HasVote(proposal.Height, proposal.Round, s.config.ValidatorAddress) {
		return nil
	}

	vote, err := s.identitySigner.SignVote(consensus.Vote{
		Height:    proposal.Height,
		Round:     proposal.Round,
		BlockHash: proposal.BlockHash,
	}, now)
	if err != nil {
		return err
	}
	if _, _, err := s.ledger.RecordVote(vote); err != nil {
		return err
	}

	go s.broadcastVote(vote)
	return nil
}

func (s *Server) maybeAutomateCommit() error {
	if !s.config.EnableBlockProduction || !s.config.RequireConsensusCertificates {
		return nil
	}

	consensusView := s.ledger.Consensus()
	if consensusView.ValidatorCount == 0 || consensusView.NextProposer != s.config.ValidatorAddress {
		return nil
	}

	proposal, exists := s.ledger.ProposalAt(consensusView.NextHeight, consensusView.CurrentRound)
	if !exists || proposal.Proposer != s.config.ValidatorAddress {
		return nil
	}
	if _, exists := s.ledger.Certificate(proposal.Height, proposal.Round, proposal.BlockHash); !exists {
		return nil
	}

	_, err := s.produceLocalBlock(proposal.ProducedAt)
	return err
}

func isActiveValidator(snapshot ledger.ValidatorSnapshot, address string) bool {
	for _, validator := range snapshot.Validators {
		if validator.Address == address {
			return true
		}
	}
	return false
}

func ignoreConsensusAutomationError(err error) bool {
	switch {
	case err == nil,
		errors.Is(err, ledger.ErrNoValidatorSet),
		errors.Is(err, ledger.ErrNoTransactionsToBlock),
		errors.Is(err, ledger.ErrValidatorNotActive),
		errors.Is(err, ledger.ErrUnexpectedProposer),
		errors.Is(err, ledger.ErrConsensusHeightMismatch),
		errors.Is(err, ledger.ErrConsensusRoundMismatch),
		errors.Is(err, ledger.ErrConsensusPreviousHash),
		errors.Is(err, ledger.ErrConflictingProposal),
		errors.Is(err, ledger.ErrUnknownProposal),
		errors.Is(err, ledger.ErrConflictingVote),
		errors.Is(err, ledger.ErrConsensusProposalRequired),
		errors.Is(err, ledger.ErrConsensusCertificateRequired),
		errors.Is(err, errBlockProductionDisabled),
		errors.Is(err, errValidatorAddressRequired),
		errors.Is(err, errNotScheduledProposer):
		return true
	default:
		return false
	}
}
