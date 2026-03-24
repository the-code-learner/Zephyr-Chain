package api

import (
	"errors"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

const automatedConsensusRound uint64 = 0

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
	if err := s.maybeAutomateProposal(); err != nil {
		return err
	}
	if err := s.maybeAutomateVote(); err != nil {
		return err
	}
	if err := s.maybeAutomateCommit(); err != nil {
		return err
	}
	return nil
}

func (s *Server) maybeAutomateProposal() error {
	if !s.config.EnableBlockProduction {
		return nil
	}

	consensusView := s.ledger.Consensus()
	if consensusView.ValidatorCount == 0 || consensusView.NextProposer == "" || consensusView.NextProposer != s.config.ValidatorAddress {
		return nil
	}
	if _, exists := s.ledger.ProposalAt(consensusView.NextHeight, automatedConsensusRound); exists {
		return nil
	}

	block, err := s.ledger.BuildNextBlock(s.config.MaxTransactionsPerBlock, time.Now().UTC())
	if err != nil {
		return err
	}
	proposal, err := s.identitySigner.SignProposal(consensus.Proposal{
		Height:         block.Height,
		Round:          automatedConsensusRound,
		BlockHash:      block.Hash,
		PreviousHash:   block.PreviousHash,
		ProducedAt:     block.ProducedAt,
		TransactionIDs: append([]string(nil), block.TransactionIDs...),
		Transactions:   append([]tx.Envelope(nil), block.Transactions...),
	}, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := s.ledger.RecordProposal(proposal); err != nil {
		return err
	}

	go s.broadcastProposal(proposal)
	return nil
}

func (s *Server) maybeAutomateVote() error {
	consensusView := s.ledger.Consensus()
	if consensusView.ValidatorCount == 0 {
		return nil
	}

	proposal, exists := s.ledger.ProposalAt(consensusView.NextHeight, automatedConsensusRound)
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
	}, time.Now().UTC())
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

	proposal, exists := s.ledger.ProposalAt(consensusView.NextHeight, automatedConsensusRound)
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
