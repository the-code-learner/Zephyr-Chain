package api

import (
	"net/http"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

// certifiedBlockDelivery is authenticated by the peer request proof, but
// the block and each consensus signature are independently verified by the
// ledger using its locally pinned validator set and chain ID. A peer's claim
// that a block is finalized is never sufficient.
type certifiedBlockDelivery struct {
	Block    ledger.Block                  `json:"block"`
	Evidence ledger.CertifiedBlockEvidence `json:"evidence"`
}

func (s *Server) handleImportCertifiedBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.validatePeerRequest(r); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}
	if !s.config.RequireConsensusCertificates {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "certified block delivery requires consensus certificate enforcement"})
		return
	}

	var delivery certifiedBlockDelivery
	if err := decodeSinglePeerJSON(r.Body, &delivery); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid certified block payload"})
		return
	}
	if err := s.ledger.ImportBlockWithEvidence(delivery.Block, delivery.Evidence); err != nil {
		s.recordBlockImportFailure("certified_peer_delivery", delivery.Block, err, requestSourceNode(r))
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}
