package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const witnessHTTPSChallengePublicationPath = "/v1/checkpoint"

// WitnessHTTPSPublicationSigner is a separately provisioned ONLINE key
// boundary. Implementations must keep the private publication key outside
// the HTTP handler and must never use an offline operator roster key or a
// witness checkpoint-signing key for this purpose.
type WitnessHTTPSPublicationSigner interface {
	SignWitnessHTTPSPublicationDigest(context.Context, [32]byte) ([]byte, error)
}

// WitnessHTTPSChallengePublicationHandler serves a challenge-bound,
// independently QC-verified witness bundle from an operator HTTPS origin.
// It NEVER owns a private key. Production must use a real, independently
// validated local QC history source and isolated online signer; passing an
// RPC assertion or test verifier is not sufficient for a safe deployment.
// Its HTTP server must be configured with genuine TLS, not a fake proxy
// TLS assertion. No public mainnet launch authorization is implied.
type WitnessHTTPSChallengePublicationHandler struct {
	source interface {
		SignedWitnessFloorContext(context.Context) ([]byte, error)
	}
	verifier       WitnessFinalityVerifier
	signer         WitnessHTTPSPublicationSigner
	network        types.NetworkID
	validatorRoot  types.Hash
	operatorID     string
	revision       uint64
	publicationKey ed25519.PublicKey
	witnessKeys    [][]byte
}

// NewWitnessHTTPSChallengePublicationHandler pins source, signing and
// verification roles. The offline inventory identity is used only to
// enforce key separation. The distinct online publication identity must
// also be pinned independently by each requesting verifier.
func NewWitnessHTTPSChallengePublicationHandler(
	source SignedWitnessFloorSource,
	verifier WitnessFinalityVerifier,
	signer WitnessHTTPSPublicationSigner,
	network types.NetworkID,
	validatorRoot types.Hash,
	operatorID string,
	revision uint64,
	offlineInventoryKey []byte,
	publicationKey []byte,
	witnessKeys [][]byte,
) (*WitnessHTTPSChallengePublicationHandler, error) {
	contextual, ok := source.(interface {
		SignedWitnessFloorContext(context.Context) ([]byte, error)
	})
	if !ok || verifier == nil || signer == nil ||
		len(offlineInventoryKey) != ed25519.PublicKeySize ||
		len(publicationKey) != ed25519.PublicKeySize ||
		bytes.Equal(offlineInventoryKey, publicationKey) ||
		len(witnessKeys) < 4 || len(witnessKeys) > maxWitnessKeys ||
		!validWitnessPublicationOperatorID(operatorID) || revision == 0 {
		return nil, ErrWitnessSigningIntent
	}
	if _, err := NewQuorumAuthenticatedWitnessFloor(source, witnessKeys, network, validatorRoot); err != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, err)
	}
	for _, key := range witnessKeys {
		if bytes.Equal(key, publicationKey) || bytes.Equal(key, offlineInventoryKey) {
			return nil, ErrWitnessSigningIntent
		}
	}
	copied := make([][]byte, len(witnessKeys))
	for i, key := range witnessKeys {
		copied[i] = append([]byte(nil), key...)
	}
	return &WitnessHTTPSChallengePublicationHandler{
		source: contextual, verifier: verifier, signer: signer,
		network: network, validatorRoot: validatorRoot, operatorID: operatorID,
		revision: revision, publicationKey: append(ed25519.PublicKey(nil), publicationKey...),
		witnessKeys: copied,
	}, nil
}

func rejectWitnessPublication(w http.ResponseWriter, status int) {
	http.Error(w, "invalid or unavailable witness publication", status)
}

// ServeHTTP is strictly a public READ endpoint. There is no signing request
// with an arbitrary (height,hash): the server obtains a quorum-signed bundle
// from its own configured source, checks pinned witness quorum, independently
// verifies local QC finality, then signs the fresh challenge+exact bytes.
// An online operator signature does not prove QC freshness or HSM monotonicity.
func (h *WitnessHTTPSChallengePublicationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if h == nil || h.source == nil || h.verifier == nil || h.signer == nil || r == nil {
		rejectWitnessPublication(w, http.StatusServiceUnavailable)
		return
	}
	if r.URL == nil || r.URL.Path != witnessHTTPSChallengePublicationPath ||
		r.URL.EscapedPath() != witnessHTTPSChallengePublicationPath ||
		(r.URL.RawPath != "" && r.URL.RawPath != witnessHTTPSChallengePublicationPath) ||
		r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Opaque != "" ||
		r.URL.Fragment != "" {
		rejectWitnessPublication(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet || r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		rejectWitnessPublication(w, http.StatusMethodNotAllowed)
		return
	}
	if r.TLS == nil {
		rejectWitnessPublication(w, http.StatusForbidden)
		return
	}
	challenges := r.Header.Values(witnessHTTPSChallengeHeader)
	if len(challenges) != 1 || len(challenges[0]) != 64 ||
		strings.ContainsAny(challenges[0], ", \t\r\n") {
		rejectWitnessPublication(w, http.StatusBadRequest)
		return
	}
	raw, err := hex.DecodeString(challenges[0])
	if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != challenges[0] {
		rejectWitnessPublication(w, http.StatusBadRequest)
		return
	}
	var challenge [32]byte
	copy(challenge[:], raw)
	if isZeroWitnessFenceChallenge(challenge) || r.Context().Err() != nil {
		rejectWitnessPublication(w, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	payload, err := h.source.SignedWitnessFloorContext(ctx)
	if err != nil || ctx.Err() != nil {
		rejectWitnessPublication(w, http.StatusServiceUnavailable)
		return
	}
	checkpoint, err := ParseSignedWitnessCheckpoint(payload, h.witnessKeys, h.network, h.validatorRoot)
	if err != nil || ctx.Err() != nil {
		rejectWitnessPublication(w, http.StatusServiceUnavailable)
		return
	}
	if err := h.verifier.VerifyFinalizedCheckpoint(*checkpoint); err != nil || ctx.Err() != nil {
		rejectWitnessPublication(w, http.StatusServiceUnavailable)
		return
	}
	digest, err := WitnessHTTPSPublicationChallengeDigest(
		h.network, h.validatorRoot, h.operatorID, h.revision, challenge, payload,
	)
	if err != nil || ctx.Err() != nil {
		rejectWitnessPublication(w, http.StatusServiceUnavailable)
		return
	}
	signature, err := h.signer.SignWitnessHTTPSPublicationDigest(ctx, digest)
	if err != nil || ctx.Err() != nil || !ed25519.Verify(h.publicationKey, digest[:], signature) {
		rejectWitnessPublication(w, http.StatusServiceUnavailable)
		return
	}
	if ctx.Err() != nil {
		rejectWitnessPublication(w, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(witnessHTTPSOperatorSignatureHeader, hex.EncodeToString(signature))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
