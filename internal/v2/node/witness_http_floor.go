package node

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPWitnessFloorSource fetches an untrusted, bounded canonical bundle. It
// deliberately does NOT authenticate the HTTP server as a witness: the
// QuorumAuthenticatedWitnessFloor must independently verify pinned keys.
// HTTPS is mandatory to reduce accidental transport disclosure/misdirection;
// deployment must still pin trusted signer keys through a separate channel.
type HTTPWitnessFloorSource struct {
	endpoint        string
	client          *http.Client
	challengePolicy *witnessHTTPSChallengePolicy
}

func NewHTTPWitnessFloorSource(endpoint string) (*HTTPWitnessFloorSource, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.Opaque != "" ||
		!canonicalWitnessHTTPSPath(u) {
		return nil, ErrWitnessSigningIntent
	}
	// This network endpoint comes from operator provisioning, not an
	// arbitrary requester. Disallow loopback, IP literals, pseudo-local
	// names and noncanonical DNS identities in production. Signatures
	// still need independent Ed25519 quorum verification above.
	host := u.Hostname()
	if host != strings.ToLower(host) || !validWitnessPublisherDNSName(host) {
		return nil, ErrWitnessSigningIntent
	}
	// Do not let an ambient HTTPS_PROXY silently become a witness floor
	// authority or a second, unreviewed network control plane.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	// Signed checkpoint bundles must use one uncompressed wire format.
	// Automatic gzip negotiation/decompression can make the effective
	// received payload much larger than its declared transfer size.
	transport.DisableCompression = true
	return &HTTPWitnessFloorSource{
		endpoint: u.String(),
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// SignedWitnessFloor is a fail-closed one-shot read. Non-200 responses,
// redirects, empty payloads, oversized bodies and unavailable origins fail.
// Callers MUST pass its output to the pinned quorum verification layer.
func (s *HTTPWitnessFloorSource) SignedWitnessFloor() ([]byte, error) {
	return s.SignedWitnessFloorContext(context.Background())
}

// SignedWitnessFloorContext also obeys the caller's cancellation. This
// prevents a cancelled custody request from holding its signing mutex while
// an otherwise valid HTTPS origin stalls. The five-second deadline still
// bounds standalone reads.
func (s *HTTPWitnessFloorSource) SignedWitnessFloorContext(ctx context.Context) ([]byte, error) {
	if s == nil || s.client == nil || ctx == nil || ctx.Err() != nil {
		return nil, ErrWitnessSigningIntent
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, err)
	}
	req.Header.Set("Accept", "application/json")
	var challenge [32]byte
	if s.challengePolicy != nil {
		if _, err := rand.Read(challenge[:]); err != nil || ctx.Err() != nil {
			return nil, errors.Join(ErrWitnessSigningIntent, err, ctx.Err())
		}
		req.Header.Set(witnessHTTPSChallengeHeader, hex.EncodeToString(challenge[:]))
		// A cache must not substitute an old response for this unpredictable
		// challenge. More importantly, it cannot forge the pinned signature.
		req.Header.Set("Cache-Control", "no-store")
		req.Header.Set("Pragma", "no-cache")
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: unexpected HTTP status %d", ErrWitnessSigningIntent, response.StatusCode)
	}
	// Do not let an HTTP intermediary change the representation passed to
	// canonical Ed25519 witness checkpoint verification. Encoding must be
	// absent or explicit identity; compressed bodies are never accepted.
	encodings := response.Header.Values("Content-Encoding")
	if len(encodings) > 1 || (len(encodings) == 1 && !strings.EqualFold(encodings[0], "identity")) {
		return nil, fmt.Errorf("%w: unsupported witness content encoding", ErrWitnessSigningIntent)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxWitnessBundleBytes+1))
	if err != nil || len(payload) == 0 || len(payload) > maxWitnessBundleBytes || ctx.Err() != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, err, ctx.Err())
	}
	if policy := s.challengePolicy; policy != nil {
		// Reject a cache replay, intermediary substitution or a DNS/TLS
		// origin lacking the independently pinned operator inventory key.
		// Both the challenge and the exact payload bytes are signed.
		contentTypes := response.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/json" {
			return nil, ErrWitnessSigningIntent
		}
		signatures := response.Header.Values(witnessHTTPSOperatorSignatureHeader)
		if len(signatures) != 1 || len(signatures[0]) != 2*ed25519.SignatureSize {
			return nil, ErrWitnessSigningIntent
		}
		sig, err := hex.DecodeString(signatures[0])
		if err != nil || len(sig) != ed25519.SignatureSize || hex.EncodeToString(sig) != signatures[0] {
			return nil, ErrWitnessSigningIntent
		}
		digest, err := WitnessHTTPSPublicationChallengeDigest(
			policy.network, policy.validatorRoot, policy.operatorID,
			policy.revision, challenge, payload,
		)
		if err != nil || ctx.Err() != nil || !ed25519.Verify(policy.publicationKey, digest[:], sig) {
			return nil, errors.Join(ErrWitnessSigningIntent, err, ctx.Err())
		}
	}
	if ctx.Err() != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, ctx.Err())
	}
	return payload, nil
}
