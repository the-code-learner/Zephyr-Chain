package api

import (
	"errors"
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

// maxCertifiedBlockDeliveryBytes limits the total signed JSON payload on
// each peer request. Even an admitted validator must not force unbounded
// decoding, hashing or memory use in the finality import path. This limit
// applies to both declared Content-Length and streaming/chunked bodies.
const maxCertifiedBlockDeliveryBytes int64 = ledger.MaxCertifiedDeliveryJSONBytes

var errCertifiedDeliveryTooLarge = errors.New("signed certified block payload exceeds transport byte limit")

func (s *Server) handleImportCertifiedBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.ContentLength > maxCertifiedBlockDeliveryBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "certified block payload exceeds size limit"})
		return
	}
	// Wrap before peer-proof verification, which may inspect the request
	// body, so chunked bodies are bounded as well as Content-Length.
	r.Body = http.MaxBytesReader(w, r.Body, maxCertifiedBlockDeliveryBytes)
	if err := s.validatePeerRequest(r); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "certified block payload exceeds size limit"})
			return
		}
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}
	if !s.config.RequireConsensusCertificates {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "certified block delivery requires consensus certificate enforcement"})
		return
	}

	var delivery certifiedBlockDelivery
	if err := decodeSinglePeerJSON(r.Body, &delivery); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "certified block payload exceeds size limit"})
			return
		}
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
