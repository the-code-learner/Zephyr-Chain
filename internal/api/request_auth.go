package api

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

const (
	sourceChainIDHeader      = "X-Zephyr-Chain-ID"
	sourceRequestDomainHeader = "X-Zephyr-Request-Domain"
	sourceRequestNonceHeader = "X-Zephyr-Request-Nonce"
	maxReplayEntries         = 10000
)

var (
	errMissingRequestProof       = errors.New("peer request must include a signed request proof")
	errInvalidRequestProof       = errors.New("invalid peer request proof")
	errRequestChainMismatch      = errors.New("peer request chain ID does not match local chain")
	errRequestDomainMismatch     = errors.New("invalid peer request signing domain")
	errRequestTimestamp          = errors.New("peer request timestamp is outside the allowed window")
	errRequestReplay             = errors.New("peer request nonce has already been used")
	errRequestReplayStoreFull    = errors.New("peer request replay store is full")
)

type requestProof struct {
	BodyHash         string
	ChainID          string
	Domain           string
	Method           string
	NodeID           string
	Nonce            string
	Path             string
	Payload          string
	PublicKey        string
	Signature        string
	SignedAt         time.Time
	ValidatorAddress string
}

type canonicalRequestProof struct {
	BodyHash         string `json:"bodyHash"`
	ChainID          string `json:"chainId"`
	Domain           string `json:"domain"`
	Method           string `json:"method"`
	NodeID           string `json:"nodeId"`
	Nonce            string `json:"nonce"`
	Path             string `json:"path"`
	SignedAt         string `json:"signedAt"`
	ValidatorAddress string `json:"validatorAddress"`
}

func (p requestProof) canonicalPayload() string {
	payload, _ := json.Marshal(canonicalRequestProof{
		BodyHash:         p.BodyHash,
		ChainID:          strings.TrimSpace(p.ChainID),
		Domain:           strings.TrimSpace(p.Domain),
		Method:           strings.ToUpper(strings.TrimSpace(p.Method)),
		NodeID:           strings.TrimSpace(p.NodeID),
		Nonce:            strings.TrimSpace(p.Nonce),
		Path:             p.Path,
		SignedAt:         p.SignedAt.UTC().Format(time.RFC3339Nano),
		ValidatorAddress: strings.TrimSpace(p.ValidatorAddress),
	})
	return string(payload)
}

func (s *transportIdentitySigner) buildRequestProof(method string, path string, body []byte, now time.Time) (requestProof, error) {
	if s == nil {
		return requestProof{}, errInvalidValidatorPrivateKey
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return requestProof{}, err
	}
	proof := requestProof{
		BodyHash:         bodySHA256(body),
		ChainID:          s.chainID,
		Domain:           protocol.TransportRequestDomain,
		Method:           strings.ToUpper(method),
		NodeID:           s.nodeID,
		Nonce:            hex.EncodeToString(nonceBytes),
		Path:             path,
		PublicKey:        s.publicKey,
		SignedAt:         now.UTC(),
		ValidatorAddress: s.validatorAddress,
	}
	proof.Payload = proof.canonicalPayload()
	signature, err := tx.SignPayload(s.privateKey, proof.Payload)
	if err != nil {
		return requestProof{}, err
	}
	proof.Signature = signature
	return proof, nil
}

func requestProofFromRequest(r *http.Request, expectedChainID string) (*requestProof, []byte, error) {
	if strings.TrimSpace(r.Header.Get(sourceNodeHeader)) == "" {
		return nil, nil, nil
	}

	body, err := readAndRestoreRequestBody(r)
	if err != nil {
		return nil, nil, errInvalidRequestProof
	}
	proof := &requestProof{
		BodyHash:         bodySHA256(body),
		ChainID:          strings.TrimSpace(r.Header.Get(sourceChainIDHeader)),
		Domain:           strings.TrimSpace(r.Header.Get(sourceRequestDomainHeader)),
		Method:           r.Method,
		NodeID:           strings.TrimSpace(r.Header.Get(sourceNodeHeader)),
		Nonce:            strings.TrimSpace(r.Header.Get(sourceRequestNonceHeader)),
		Path:             canonicalRequestPath(r),
		Payload:          strings.TrimSpace(r.Header.Get(sourceIdentityPayloadHeader)),
		PublicKey:        strings.TrimSpace(r.Header.Get(sourcePublicKeyHeader)),
		Signature:        strings.TrimSpace(r.Header.Get(sourceSignatureHeader)),
		ValidatorAddress: strings.TrimSpace(r.Header.Get(sourceValidatorHeader)),
	}
	signedAtRaw := strings.TrimSpace(r.Header.Get(sourceSignedAtHeader))
	if proof.ChainID == "" || proof.Domain == "" || proof.NodeID == "" || proof.Nonce == "" || proof.Payload == "" || proof.PublicKey == "" || proof.Signature == "" || proof.ValidatorAddress == "" || signedAtRaw == "" {
		return nil, body, errMissingRequestProof
	}
	proof.SignedAt, err = time.Parse(time.RFC3339Nano, signedAtRaw)
	if err != nil {
		return nil, body, errRequestTimestamp
	}
	proof.SignedAt = proof.SignedAt.UTC()

	expectedChainID = strings.TrimSpace(expectedChainID)
	if proof.ChainID != expectedChainID {
		return nil, body, errRequestChainMismatch
	}
	if proof.Domain != protocol.TransportRequestDomain {
		return nil, body, errRequestDomainMismatch
	}
	if proof.Payload != proof.canonicalPayload() {
		return nil, body, errInvalidRequestProof
	}
	address, err := tx.DeriveAddressFromPublicKey(proof.PublicKey)
	if err != nil || address != proof.ValidatorAddress {
		return nil, body, errInvalidRequestProof
	}
	now := time.Now().UTC()
	if proof.SignedAt.Before(now.Add(-transportIdentityMaxSkew)) || proof.SignedAt.After(now.Add(transportIdentityMaxSkew)) {
		return nil, body, errRequestTimestamp
	}
	if err := tx.VerifySignature(proof.PublicKey, proof.Payload, proof.Signature); err != nil {
		return nil, body, errInvalidRequestProof
	}
	return proof, body, nil
}

func canonicalRequestPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	return path
}

func readAndRestoreRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func bodySHA256(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

type replayState struct {
	Entries []replayEntry `json:"entries"`
}

type replayEntry struct {
	Key       string    `json:"key"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type requestReplayGuard struct {
	mu      sync.Mutex
	path    string
	entries map[string]time.Time
}

var replayGuards = struct {
	sync.Mutex
	byServer map[*Server]*requestReplayGuard
}{byServer: make(map[*Server]*requestReplayGuard)}

func replayGuardForServer(s *Server) (*requestReplayGuard, error) {
	replayGuards.Lock()
	defer replayGuards.Unlock()
	if guard := replayGuards.byServer[s]; guard != nil {
		return guard, nil
	}
	guard, err := newRequestReplayGuard(filepath.Join(s.ledger.DataDir(), "request-replay.json"))
	if err != nil {
		return nil, err
	}
	replayGuards.byServer[s] = guard
	return guard, nil
}

func newRequestReplayGuard(path string) (*requestReplayGuard, error) {
	guard := &requestReplayGuard{path: path, entries: make(map[string]time.Time)}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return guard, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return guard, nil
	}
	var state replayState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, entry := range state.Entries {
		if entry.Key == "" || !entry.ExpiresAt.After(now) {
			continue
		}
		guard.entries[entry.Key] = entry.ExpiresAt.UTC()
	}
	return guard, nil
}

func (g *requestReplayGuard) remember(proof *requestProof) error {
	if g == nil || proof == nil {
		return errInvalidRequestProof
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UTC()
	for key, expiresAt := range g.entries {
		if !expiresAt.After(now) {
			delete(g.entries, key)
		}
	}
	key := proof.ValidatorAddress + "|" + proof.Nonce
	if expiresAt, exists := g.entries[key]; exists && expiresAt.After(now) {
		return errRequestReplay
	}
	if len(g.entries) >= maxReplayEntries {
		return errRequestReplayStoreFull
	}
	g.entries[key] = proof.SignedAt.Add(transportIdentityMaxSkew).UTC()
	return g.persistLocked()
}

func (g *requestReplayGuard) persistLocked() error {
	entries := make([]replayEntry, 0, len(g.entries))
	for key, expiresAt := range g.entries {
		entries = append(entries, replayEntry{Key: key, ExpiresAt: expiresAt})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	raw, err := json.MarshalIndent(replayState{Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	return writeSecurityStateAtomic(g.path, raw)
}

func writeSecurityStateAtomic(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".zephyr-security-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func validateAndRememberRequestProof(s *Server, r *http.Request) (*requestProof, error) {
	proof, _, err := requestProofFromRequest(r, s.config.ChainID)
	if err != nil {
		return nil, err
	}
	if proof == nil {
		return nil, nil
	}
	guard, err := replayGuardForServer(s)
	if err != nil {
		return nil, fmt.Errorf("request replay guard: %w", err)
	}
	if err := guard.remember(proof); err != nil {
		return nil, err
	}
	return proof, nil
}
