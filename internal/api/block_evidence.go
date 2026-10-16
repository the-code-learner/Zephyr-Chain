package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

type BlockEvidenceResponse struct {
	Evidence []ledger.CertifiedBlockEvidence `json:"evidence"`
}

func (s *Server) handleBlockEvidence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.validatePeerRequest(r); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	rawHeight := strings.TrimPrefix(r.URL.Path, "/v1/internal/block-evidence/")
	height, err := strconv.ParseUint(rawHeight, 10, 64)
	if err != nil || height == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid block evidence height"})
		return
	}
	fragments := s.ledger.CertifiedBlockEvidenceFragments(height)
	if len(fragments) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "certified block evidence not found"})
		return
	}
	writeJSON(w, http.StatusOK, BlockEvidenceResponse{Evidence: fragments})
}
