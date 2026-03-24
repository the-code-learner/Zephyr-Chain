package api

import (
	"errors"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

func (s *Server) recordConsensusDiagnostic(kind string, source string, err error, height uint64, round uint64, blockHash string, validator string) {
	if err == nil {
		return
	}
	diagnostic := ledger.ConsensusDiagnostic{
		Kind:       kind,
		Code:       consensusDiagnosticCode(err),
		Message:    err.Error(),
		Height:     height,
		Round:      round,
		BlockHash:  blockHash,
		Validator:  validator,
		Source:     source,
		ObservedAt: time.Now().UTC(),
	}
	if recordErr := s.ledger.RecordConsensusDiagnostic(diagnostic); recordErr != nil {
		recordPeerLog("consensus-diagnostic", recordErr)
		return
	}
	s.eventLogger.logConsensusDiagnostic(diagnostic)
}

func consensusDiagnosticCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ledger.ErrUnexpectedProposer):
		return "unexpected_proposer"
	case errors.Is(err, ledger.ErrConsensusRoundMismatch):
		return "stale_round"
	case errors.Is(err, ledger.ErrConflictingProposal):
		return "conflicting_proposal"
	case errors.Is(err, ledger.ErrConflictingVote):
		return "conflicting_vote"
	case errors.Is(err, ledger.ErrConsensusCertificateRequired):
		return "certificate_required"
	case errors.Is(err, ledger.ErrConsensusTemplateMismatch):
		return "template_mismatch"
	case errors.Is(err, ledger.ErrConsensusProposalRequired):
		return "proposal_required"
	case errors.Is(err, ledger.ErrConsensusPreviousHash):
		return "previous_hash_mismatch"
	case errors.Is(err, ledger.ErrUnknownProposal):
		return "unknown_proposal"
	case errors.Is(err, ledger.ErrConsensusHeightMismatch):
		return "height_mismatch"
	case errors.Is(err, ledger.ErrValidatorNotActive):
		return "validator_not_active"
	case errors.Is(err, errNotScheduledProposer):
		return "not_scheduled_proposer"
	case errors.Is(err, errValidatorAddressRequired):
		return "validator_address_required"
	case errors.Is(err, errBlockProductionDisabled):
		return "block_production_disabled"
	default:
		return "consensus_error"
	}
}
