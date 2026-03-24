package api

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

func (s *Server) recordBlockImportFailure(source string, block ledger.Block, err error, peerLabel string) {
	if err == nil {
		return
	}

	round, validator := s.consensusContextForBlock(block)
	s.recordConsensusDiagnostic("block_import_rejected", source, err, block.Height, round, block.Hash, validator)
	if !shouldTrackImportRecovery(err) {
		return
	}

	now := time.Now().UTC()
	if recordErr := s.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:       ledger.ConsensusActionBlockImport,
		Height:     block.Height,
		Round:      round,
		BlockHash:  block.Hash,
		Validator:  validator,
		RecordedAt: now,
		Status:     ledger.ConsensusActionPending,
		Note:       blockImportRecoveryNote(source, peerLabel, err),
	}); recordErr != nil {
		recordPeerLog("consensus-recovery-import", recordErr)
	}
}

func (s *Server) recordSnapshotRestore(peerLabel string, snapshot ledger.Snapshot, now time.Time) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	height := uint64(len(snapshot.Blocks))
	blockHash := ""
	if len(snapshot.Blocks) > 0 {
		blockHash = snapshot.Blocks[len(snapshot.Blocks)-1].Hash
	}
	note := "restored peer snapshot"
	if peerLabel != "" {
		note = fmt.Sprintf("restored peer snapshot from %s", peerLabel)
	}

	if err := s.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:        ledger.ConsensusActionSnapshotSync,
		Height:      height,
		BlockHash:   blockHash,
		RecordedAt:  now,
		Status:      ledger.ConsensusActionCompleted,
		Note:        note,
		CompletedAt: cloneTimeValue(now),
	}); err != nil {
		recordPeerLog("consensus-recovery-snapshot", err)
	}
}

func (s *Server) consensusContextForBlock(block ledger.Block) (uint64, string) {
	proposals := s.ledger.ProposalsForHeight(block.Height)
	for _, proposal := range proposals {
		if proposalMatchesBlock(proposal, block) {
			return proposal.Round, proposal.Proposer
		}
	}
	for _, proposal := range proposals {
		if proposal.BlockHash == block.Hash {
			return proposal.Round, proposal.Proposer
		}
	}

	certificates := s.ledger.CertificatesForHeight(block.Height)
	foundRound := uint64(0)
	found := false
	for _, certificate := range certificates {
		if certificate.BlockHash != block.Hash {
			continue
		}
		if !found || certificate.Round >= foundRound {
			foundRound = certificate.Round
			found = true
		}
	}
	if found {
		return foundRound, ""
	}
	return 0, ""
}

func shouldTrackImportRecovery(err error) bool {
	switch {
	case errors.Is(err, ledger.ErrConsensusProposalRequired),
		errors.Is(err, ledger.ErrConsensusTemplateMismatch),
		errors.Is(err, ledger.ErrConsensusCertificateRequired),
		errors.Is(err, ledger.ErrConsensusPreviousHash),
		errors.Is(err, ledger.ErrBlockOutOfSequence),
		errors.Is(err, ledger.ErrBlockConflict):
		return true
	default:
		return false
	}
}

func blockImportRecoveryNote(source string, peerLabel string, err error) string {
	code := consensusDiagnosticCode(err)
	if code == "" {
		code = "consensus_error"
	}
	parts := []string{fmt.Sprintf("waiting for peer block import after %s", code)}
	if peerLabel != "" {
		parts = append(parts, fmt.Sprintf("from %s", peerLabel))
	} else if source != "" {
		parts = append(parts, fmt.Sprintf("via %s", source))
	}
	return strings.Join(parts, " ")
}
