package api

import (
	"errors"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

type BlockReadiness struct {
	Height                      uint64     `json:"height"`
	LocalTemplateAvailable      bool       `json:"localTemplateAvailable"`
	LocalTemplateBlockHash      string     `json:"localTemplateBlockHash,omitempty"`
	LocalTemplateProducedAt     *time.Time `json:"localTemplateProducedAt,omitempty"`
	LocalTemplateTxCount        int        `json:"localTemplateTransactionCount"`
	StoredProposalCount         int        `json:"storedProposalCount"`
	CertifiedProposalCount      int        `json:"certifiedProposalCount"`
	MatchingLocalProposalRound  *uint64    `json:"matchingLocalProposalRound,omitempty"`
	MatchingLocalCertificate    bool       `json:"matchingLocalCertificate"`
	ReadyToCommitLocalTemplate  bool       `json:"readyToCommitLocalTemplate"`
	ReadyToCommitStoredProposal bool       `json:"readyToCommitStoredProposal"`
	ReadyToImportCertifiedBlock bool       `json:"readyToImportCertifiedBlock"`
	LatestCertifiedRound        *uint64    `json:"latestCertifiedRound,omitempty"`
	LatestCertifiedBlockHash    string     `json:"latestCertifiedBlockHash,omitempty"`
	LatestCertifiedProducedAt   *time.Time `json:"latestCertifiedProducedAt,omitempty"`
	Warnings                    []string   `json:"warnings"`
}

func (s *Server) buildBlockReadiness(now time.Time) BlockReadiness {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	consensusView := s.ledger.Consensus()
	readiness := BlockReadiness{
		Height:   consensusView.NextHeight,
		Warnings: make([]string, 0),
	}

	proposals := s.ledger.ProposalsForHeight(consensusView.NextHeight)
	readiness.StoredProposalCount = len(proposals)

	referenceProducedAt := now
	if len(proposals) > 0 {
		referenceProducedAt = proposals[len(proposals)-1].ProducedAt
	}
	if block, err := s.ledger.BuildNextBlock(s.config.MaxTransactionsPerBlock, referenceProducedAt); err == nil {
		readiness.LocalTemplateAvailable = true
		readiness.LocalTemplateBlockHash = block.Hash
		readiness.LocalTemplateProducedAt = cloneTimeValue(block.ProducedAt)
		readiness.LocalTemplateTxCount = block.TransactionCount
	} else if !errors.Is(err, ledger.ErrNoTransactionsToBlock) {
		readiness.Warnings = append(readiness.Warnings, "local_template_unavailable")
	}

	for _, proposal := range proposals {
		if certificate, ok := s.ledger.Certificate(proposal.Height, proposal.Round, proposal.BlockHash); ok {
			readiness.CertifiedProposalCount++
			if readiness.LatestCertifiedRound == nil || proposal.Round >= *readiness.LatestCertifiedRound {
				round := proposal.Round
				readiness.LatestCertifiedRound = &round
				readiness.LatestCertifiedBlockHash = certificate.BlockHash
				readiness.LatestCertifiedProducedAt = cloneTimeValue(proposal.ProducedAt)
			}
		}

		block, err := s.ledger.BuildNextBlock(s.config.MaxTransactionsPerBlock, proposal.ProducedAt)
		if err != nil {
			continue
		}
		if !proposalMatchesBlock(proposal, block) {
			continue
		}
		round := proposal.Round
		if readiness.MatchingLocalProposalRound == nil || round >= *readiness.MatchingLocalProposalRound {
			readiness.MatchingLocalProposalRound = &round
			readiness.LocalTemplateAvailable = true
			readiness.LocalTemplateBlockHash = block.Hash
			readiness.LocalTemplateProducedAt = cloneTimeValue(block.ProducedAt)
			readiness.LocalTemplateTxCount = block.TransactionCount
			if _, ok := s.ledger.Certificate(proposal.Height, proposal.Round, proposal.BlockHash); ok {
				readiness.MatchingLocalCertificate = true
			} else {
				readiness.MatchingLocalCertificate = false
			}
		}
	}

	readiness.ReadyToCommitLocalTemplate = readiness.LocalTemplateAvailable && readiness.MatchingLocalCertificate
	readiness.ReadyToCommitStoredProposal = readiness.CertifiedProposalCount > 0
	readiness.ReadyToImportCertifiedBlock = readiness.CertifiedProposalCount > 0

	switch {
	case readiness.LocalTemplateAvailable && readiness.StoredProposalCount == 0:
		readiness.Warnings = append(readiness.Warnings, "proposal_missing")
	case readiness.LocalTemplateAvailable && readiness.MatchingLocalProposalRound == nil && readiness.StoredProposalCount > 0:
		readiness.Warnings = append(readiness.Warnings, "local_template_mismatch")
	case readiness.LocalTemplateAvailable && readiness.MatchingLocalProposalRound != nil && !readiness.MatchingLocalCertificate:
		readiness.Warnings = append(readiness.Warnings, "certificate_missing")
	}
	if readiness.CertifiedProposalCount > 0 && readiness.LocalTemplateAvailable && !readiness.ReadyToCommitLocalTemplate {
		readiness.Warnings = append(readiness.Warnings, "certified_proposal_differs_from_local_template")
	}
	if !readiness.LocalTemplateAvailable && readiness.CertifiedProposalCount > 0 {
		readiness.Warnings = append(readiness.Warnings, "certified_proposal_available_without_local_template")
	}

	return readiness
}

func proposalMatchesBlock(proposal consensus.Proposal, block ledger.Block) bool {
	return proposal.Height == block.Height &&
		proposal.BlockHash == block.Hash &&
		proposal.PreviousHash == block.PreviousHash &&
		proposal.ProducedAt.Equal(block.ProducedAt) &&
		equalStringSlices(proposal.TransactionIDs, block.TransactionIDs) &&
		equalEnvelopeSlices(proposal.Transactions, block.Transactions)
}

func equalStringSlices(left []string, right []string) bool {
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

func equalEnvelopeSlices(left []tx.Envelope, right []tx.Envelope) bool {
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

func cloneTimeValue(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	cloned := value
	return &cloned
}
