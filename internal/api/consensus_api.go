package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

type ProposalResponse struct {
	Accepted  bool                          `json:"accepted"`
	Proposal  consensus.Proposal            `json:"proposal"`
	Artifacts ledger.ConsensusArtifactsView `json:"artifacts"`
	Consensus ledger.ConsensusView          `json:"consensus"`
	Recovery  ledger.ConsensusRecoveryView  `json:"recovery"`
}

type VoteResponse struct {
	Accepted    bool                          `json:"accepted"`
	Vote        consensus.Vote                `json:"vote"`
	Tally       ledger.VoteTally              `json:"tally"`
	Certificate *ledger.CommitCertificate     `json:"certificate,omitempty"`
	Artifacts   ledger.ConsensusArtifactsView `json:"artifacts"`
	Consensus   ledger.ConsensusView          `json:"consensus"`
	Recovery    ledger.ConsensusRecoveryView  `json:"recovery"`
}

func (s *Server) handleConsensusProposal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.validatePeerRequest(r); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	var request consensus.Proposal
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	if request.ProposedAt.IsZero() {
		request.ProposedAt = time.Now().UTC()
	}

	sourceNode := requestSourceNode(r)
	var err error
	if sourceNode == "" && s.config.ValidatorAddress != "" && request.Proposer == s.config.ValidatorAddress {
		err = s.ledger.RecordProposalWithAction(request, &ledger.ConsensusAction{
			Type:       ledger.ConsensusActionProposal,
			Height:     request.Height,
			Round:      request.Round,
			BlockHash:  request.BlockHash,
			Validator:  request.Proposer,
			RecordedAt: request.ProposedAt,
			Note:       "local proposal submitted",
		})
	} else {
		err = s.ledger.RecordProposal(request)
	}
	if err != nil {
		source := "local_api"
		if sourceNode != "" {
			source = "peer"
		}
		s.recordConsensusDiagnostic("proposal_rejected", source, err, request.Height, request.Round, request.BlockHash, request.Proposer)
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, ProposalResponse{
		Accepted:  true,
		Proposal:  request,
		Artifacts: s.ledger.ConsensusArtifacts(),
		Consensus: s.ledger.Consensus(),
		Recovery:  s.ledger.ConsensusRecovery(),
	})

	if sourceNode == "" {
		go s.broadcastProposal(request)
	}
}

func (s *Server) handleConsensusVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.validatePeerRequest(r); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	var request consensus.Vote
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	if request.VotedAt.IsZero() {
		request.VotedAt = time.Now().UTC()
	}

	sourceNode := requestSourceNode(r)
	var (
		tally       ledger.VoteTally
		certificate *ledger.CommitCertificate
		err         error
	)
	if sourceNode == "" && s.config.ValidatorAddress != "" && request.Voter == s.config.ValidatorAddress {
		tally, certificate, err = s.ledger.RecordVoteWithAction(request, &ledger.ConsensusAction{
			Type:       ledger.ConsensusActionVote,
			Height:     request.Height,
			Round:      request.Round,
			BlockHash:  request.BlockHash,
			Validator:  request.Voter,
			RecordedAt: request.VotedAt,
			Note:       "local vote submitted",
		})
	} else {
		tally, certificate, err = s.ledger.RecordVote(request)
	}
	if err != nil {
		source := "local_api"
		if sourceNode != "" {
			source = "peer"
		}
		s.recordConsensusDiagnostic("vote_rejected", source, err, request.Height, request.Round, request.BlockHash, request.Voter)
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, VoteResponse{
		Accepted:    true,
		Vote:        request,
		Tally:       tally,
		Certificate: certificate,
		Artifacts:   s.ledger.ConsensusArtifacts(),
		Consensus:   s.ledger.Consensus(),
		Recovery:    s.ledger.ConsensusRecovery(),
	})

	if sourceNode == "" {
		go s.broadcastVote(request)
	}
}
