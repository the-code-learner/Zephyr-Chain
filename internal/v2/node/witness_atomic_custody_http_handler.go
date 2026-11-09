package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	witnessCustodyHandlerPath     = "/v1/reserve-and-sign"
	maxWitnessCustodyRequestBytes = 1024
)

// HTTPWitnessCustodyHandler is an HTTP adapter for an OUT-OF-PROCESS custody
// backend. It never owns private witness or authority keys. The backend MUST
// independently verify QC history and perform serialized durable reservation
// and signing under one remote key authority. This handler does NOT implement
// those properties or configure TLS listener certificates.
type HTTPWitnessCustodyHandler struct {
	backend       WitnessAtomicCustodian
	network       types.NetworkID
	validatorRoot types.Hash
	authorityKey  []byte
	witnessKey    []byte
}

// NewHTTPWitnessCustodyHandler pins both independent public keys. Deployment
// MUST configure the HTTP server with tls.RequireAndVerifyClientCert and a
// separately provisioned ClientCAs policy; requests without a verified client
// TLS chain are rejected even if they reach this handler.
func NewHTTPWitnessCustodyHandler(backend WitnessAtomicCustodian, network types.NetworkID, validatorRoot types.Hash, authorityKey, witnessKey []byte) (*HTTPWitnessCustodyHandler, error) {
	if backend == nil || types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(validatorRoot)) ||
		len(authorityKey) != ed25519.PublicKeySize ||
		len(witnessKey) != ed25519.PublicKeySize || bytes.Equal(authorityKey, witnessKey) {
		return nil, ErrWitnessSigningIntent
	}
	return &HTTPWitnessCustodyHandler{
		backend:       backend,
		network:       network,
		validatorRoot: validatorRoot,
		authorityKey:  append([]byte(nil), authorityKey...),
		witnessKey:    append([]byte(nil), witnessKey...),
	}, nil
}

func custodyHTTPReject(w http.ResponseWriter, code int) {
	http.Error(w, "invalid or unavailable witness custody request", code)
}

// ServeHTTP enforces authenticated client TLS, canonical bounded POST JSON,
// exact identity/challenge binding and BOTH pinned signatures before returning
// any attestation. It never delegates unchecked data to a private-key backend.
// Application-level mTLS policy and real remote HSM durability require separate
// deployment evidence and third-party review before public mainnet.
func (h *HTTPWitnessCustodyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if h == nil || h.backend == nil || r == nil {
		custodyHTTPReject(w, http.StatusServiceUnavailable)
		return
	}
	if r.URL == nil || r.URL.Path != witnessCustodyHandlerPath || r.URL.RawQuery != "" {
		custodyHTTPReject(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		custodyHTTPReject(w, http.StatusMethodNotAllowed)
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 {
		custodyHTTPReject(w, http.StatusForbidden)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Body == nil {
		custodyHTTPReject(w, http.StatusUnsupportedMediaType)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxWitnessCustodyRequestBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxWitnessCustodyRequestBytes {
		custodyHTTPReject(w, http.StatusBadRequest)
		return
	}
	var request witnessCustodyRequestJSON
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		custodyHTTPReject(w, http.StatusBadRequest)
		return
	}
	canonical, err := json.Marshal(request)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		custodyHTTPReject(w, http.StatusBadRequest)
		return
	}
	networkBytes, err := decodeCanonicalWitnessCustodyHex(request.NetworkID, 32)
	if err != nil {
		custodyHTTPReject(w, http.StatusBadRequest)
		return
	}
	rootBytes, err := decodeCanonicalWitnessCustodyHex(request.ValidatorRoot, 32)
	if err != nil {
		custodyHTTPReject(w, http.StatusBadRequest)
		return
	}
	headerBytes, err := decodeCanonicalWitnessCustodyHex(request.HeaderHash, 32)
	if err != nil {
		custodyHTTPReject(w, http.StatusBadRequest)
		return
	}
	challengeBytes, err := decodeCanonicalWitnessCustodyHex(request.Challenge, 32)
	if err != nil {
		custodyHTTPReject(w, http.StatusBadRequest)
		return
	}
	var network types.NetworkID
	var root, header types.Hash
	var challenge [32]byte
	copy(network[:], networkBytes)
	copy(root[:], rootBytes)
	copy(header[:], headerBytes)
	copy(challenge[:], challengeBytes)
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: request.Height, HeaderHash: header}
	if cp.Validate(h.network, h.validatorRoot) != nil || isZeroWitnessFenceChallenge(challenge) {
		custodyHTTPReject(w, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	a, err := h.backend.ReserveAndSignWitnessCheckpoint(ctx, cp, challenge)
	if err != nil || ctx.Err() != nil || VerifyWitnessAtomicCustodyAttestation(a, cp, challenge, h.authorityKey, h.witnessKey) != nil {
		custodyHTTPReject(w, http.StatusServiceUnavailable)
		return
	}
	response := witnessCustodyResponseJSON{
		NetworkID: request.NetworkID, ValidatorRoot: request.ValidatorRoot,
		Height: cp.Height, HeaderHash: request.HeaderHash, Challenge: request.Challenge,
		Revision: a.Revision, WitnessPublicKey: hex.EncodeToString(a.WitnessPublicKey),
		WitnessSignature:   hex.EncodeToString(a.WitnessSignature),
		AuthoritySignature: hex.EncodeToString(a.AuthoritySignature),
	}
	encoded, err := json.Marshal(response)
	if err != nil || ctx.Err() != nil || len(encoded) > maxWitnessCustodyResponseBytes {
		custodyHTTPReject(w, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}
