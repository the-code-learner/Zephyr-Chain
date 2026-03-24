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
}

type VoteResponse struct {
	Accepted    bool                          `json:"accepted"`
	Vote        consensus.Vote                `json:"vote"`
	Tally       ledger.VoteTally              `json:"tally"`
	Certificate *ledger.CommitCertificate     `json:"certificate,omitempty"`
	Artifacts   ledger.ConsensusArtifactsView `json:"artifacts"`
	Consensus   ledger.ConsensusView          `json:"consensus"`
}

func (s *Server) handleConsensusProposal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := validateRequestTransportIdentity(r); err != nil {
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
	if err := s.ledger.RecordProposal(request); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, ProposalResponse{
		Accepted:  true,
		Proposal:  request,
		Artifacts: s.ledger.ConsensusArtifacts(),
		Consensus: s.ledger.Consensus(),
	})

	if requestSourceNode(r) == "" {
		go s.broadcastProposal(request)
	}
}

func (s *Server) handleConsensusVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := validateRequestTransportIdentity(r); err != nil {
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
	tally, certificate, err := s.ledger.RecordVote(request)
	if err != nil {
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
	})

	if requestSourceNode(r) == "" {
		go s.broadcastVote(request)
	}
}
