package node

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxWitnessCustodyResponseBytes = 4096

// HTTPWitnessAtomicCustodian transports a NON-SIGNING request to an external
// custody service. HTTPS authenticates the transport endpoint, but only the
// caller's independently pinned Ed25519 authority and witness keys authenticate
// the returned reservation/signature evidence. This client DOES NOT implement
// globally atomic or non-rollbackable server-side signing.
type HTTPWitnessAtomicCustodian struct {
	endpoint string
	client   *http.Client
}

type witnessCustodyRequestJSON struct {
	NetworkID     string `json:"networkId"`
	ValidatorRoot string `json:"validatorRoot"`
	Height        uint64 `json:"height"`
	HeaderHash    string `json:"headerHash"`
	Challenge     string `json:"challenge"`
}

type witnessCustodyResponseJSON struct {
	NetworkID          string `json:"networkId"`
	ValidatorRoot      string `json:"validatorRoot"`
	Height             uint64 `json:"height"`
	HeaderHash         string `json:"headerHash"`
	Challenge          string `json:"challenge"`
	Revision           uint64 `json:"revision"`
	WitnessPublicKey   string `json:"witnessPublicKey"`
	WitnessSignature   string `json:"witnessSignature"`
	AuthoritySignature string `json:"authoritySignature"`
}

// NewHTTPWitnessAtomicCustodian requires an explicit canonical HTTPS DNS
// origin and endpoint path. Production operators MUST configure TLS trust and
// witness/authority public-key pins outside this untrusted transport.
func NewHTTPWitnessAtomicCustodian(endpoint string) (*HTTPWitnessAtomicCustodian, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.Opaque != "" ||
		!canonicalWitnessHTTPSPath(u) {
		return nil, ErrWitnessSigningIntent
	}
	host := u.Hostname()
	if host != strings.ToLower(host) || !validWitnessPublisherDNSName(host) {
		return nil, ErrWitnessSigningIntent
	}
	// A process-wide HTTPS_PROXY must not silently become a custody
	// network control plane. The mTLS constructor and authenticated
	// witness floor already disable ambient proxies; do the same for
	// the default HTTPS custodian while keeping system CA verification.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	// Custody attestation bytes must not be implicitly decompressed by the
	// default Go transport, or transformed by an HTTP intermediary.
	transport.DisableCompression = true
	return &HTTPWitnessAtomicCustodian{
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

// NewHTTPWitnessAtomicCustodianWithTLS configures authenticated transport
// to a server that requires verified client certificates. Operators must
// provision the client TLS certificate and a separate trusted server CA;
// these credentials are NOT witness-signing or authority private keys.
func NewHTTPWitnessAtomicCustodianWithTLS(endpoint string, config *tls.Config) (*HTTPWitnessAtomicCustodian, error) {
	if config == nil || config.InsecureSkipVerify || config.RootCAs == nil ||
		(len(config.Certificates) == 0 && config.GetClientCertificate == nil) ||
		(config.MaxVersion != 0 && config.MaxVersion < tls.VersionTLS13) {
		return nil, ErrWitnessSigningIntent
	}
	u, err := url.Parse(endpoint)
	if err != nil || (config.ServerName != "" && config.ServerName != u.Hostname()) {
		return nil, ErrWitnessSigningIntent
	}
	client, err := NewHTTPWitnessAtomicCustodian(endpoint)
	if err != nil {
		return nil, err
	}
	tlsConfig := config.Clone()
	if tlsConfig.MinVersion < tls.VersionTLS13 {
		tlsConfig.MinVersion = tls.VersionTLS13
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Never silently route custody authority traffic through an ambient
	// HTTPS_PROXY/HTTP_PROXY process or a different trust domain.
	transport.Proxy = nil
	transport.DisableCompression = true
	transport.TLSClientConfig = tlsConfig
	client.client.Transport = transport
	return client, nil
}

func decodeCanonicalWitnessCustodyHex(value string, length int) ([]byte, error) {
	if len(value) != 2*length {
		return nil, ErrWitnessSigningIntent
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return nil, ErrWitnessSigningIntent
	}
	return decoded, nil
}

// ReserveAndSignWitnessCheckpoint exchanges a bounded, canonical JSON POST
// for externally generated signatures. It never signs locally, follows no
// redirects and rejects unknown or ambiguous response fields. The caller MUST
// verify the resulting attestation with VerifyWitnessAtomicCustodyAttestation,
// independently pinned keys and QC history before trusting it.
func (s *HTTPWitnessAtomicCustodian) ReserveAndSignWitnessCheckpoint(ctx context.Context, cp FinalityCheckpoint, challenge [32]byte) (WitnessAtomicCustodyAttestation, error) {
	fail := func(err error) (WitnessAtomicCustodyAttestation, error) {
		return WitnessAtomicCustodyAttestation{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	if s == nil || s.client == nil || ctx == nil || ctx.Err() != nil ||
		cp.Validate(cp.Network, cp.ValidatorRoot) != nil || isZeroWitnessFenceChallenge(challenge) {
		return fail(nil)
	}
	request := witnessCustodyRequestJSON{
		NetworkID: hex.EncodeToString(cp.Network[:]), ValidatorRoot: hex.EncodeToString(cp.ValidatorRoot[:]),
		Height: cp.Height, HeaderHash: hex.EncodeToString(cp.HeaderHash[:]),
		Challenge: hex.EncodeToString(challenge[:]),
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fail(err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fail(err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	response, err := s.client.Do(httpRequest)
	if err != nil {
		return fail(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fail(nil)
	}
	// Accept only an unchanged canonical wire representation. This check
	// must run before the bounded read and signature/authority verification.
	encodings := response.Header.Values("Content-Encoding")
	if len(encodings) > 1 || (len(encodings) == 1 && !strings.EqualFold(encodings[0], "identity")) {
		return fail(nil)
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return fail(nil)
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		return fail(err)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxWitnessCustodyResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxWitnessCustodyResponseBytes || requestCtx.Err() != nil {
		return fail(err)
	}
	var wire witnessCustodyResponseJSON
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return fail(err)
	}
	canonical, err := json.Marshal(wire)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return fail(err)
	}
	if wire.NetworkID != request.NetworkID || wire.ValidatorRoot != request.ValidatorRoot ||
		wire.Height != cp.Height || wire.HeaderHash != request.HeaderHash || wire.Challenge != request.Challenge ||
		wire.Revision == 0 {
		return fail(nil)
	}
	publicKey, err := decodeCanonicalWitnessCustodyHex(wire.WitnessPublicKey, 32)
	if err != nil {
		return fail(err)
	}
	witnessSignature, err := decodeCanonicalWitnessCustodyHex(wire.WitnessSignature, 64)
	if err != nil {
		return fail(err)
	}
	authoritySignature, err := decodeCanonicalWitnessCustodyHex(wire.AuthoritySignature, 64)
	if err != nil || requestCtx.Err() != nil {
		return fail(err)
	}
	// This only translates an untrusted JSON envelope. Key pinning, both
	// Ed25519 verifications, durable local intent and QC recheck live above.
	return WitnessAtomicCustodyAttestation{
		Checkpoint: cp, Challenge: challenge, Revision: wire.Revision,
		WitnessPublicKey: publicKey, WitnessSignature: witnessSignature,
		AuthoritySignature: authoritySignature,
	}, nil
}
