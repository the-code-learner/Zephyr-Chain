package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var (
	errMissingTransportIdentityFields     = errors.New("missing transport identity fields")
	errInvalidTransportIdentityPayload    = errors.New("transport identity payload does not match canonical identity")
	errTransportIdentityTimestamp         = errors.New("transport identity timestamp is outside the allowed window")
	errInvalidTransportIdentityPublicKey  = errors.New("invalid transport identity public key")
	errTransportIdentityAddressMismatch   = errors.New("transport identity validator address does not match public key")
	errInvalidTransportIdentitySignature  = errors.New("invalid transport identity signature")
	errTransportIdentityNodeMismatch      = errors.New("transport identity node does not match status response")
	errTransportIdentityValidatorMismatch = errors.New("transport identity validator does not match status response")
	errInvalidValidatorPrivateKey         = errors.New("invalid validator private key")
	errValidatorIdentityMismatch          = errors.New("validator private key does not match configured validator address")
)

const transportIdentityMaxSkew = 2 * time.Minute

type TransportIdentity struct {
	NodeID           string    `json:"nodeId"`
	ValidatorAddress string    `json:"validatorAddress"`
	Payload          string    `json:"payload"`
	PublicKey        string    `json:"publicKey"`
	Signature        string    `json:"signature"`
	SignedAt         time.Time `json:"signedAt"`
}

type canonicalTransportIdentity struct {
	NodeID           string `json:"nodeId"`
	SignedAt         string `json:"signedAt"`
	ValidatorAddress string `json:"validatorAddress"`
}

type transportIdentitySigner struct {
	nodeID           string
	validatorAddress string
	publicKey        string
	privateKey       *ecdsa.PrivateKey
}

func (i TransportIdentity) CanonicalPayload() string {
	payload, _ := json.Marshal(canonicalTransportIdentity{
		NodeID:           i.NodeID,
		SignedAt:         i.SignedAt.UTC().Format(time.RFC3339Nano),
		ValidatorAddress: i.ValidatorAddress,
	})
	return string(payload)
}

func (i TransportIdentity) ValidateAt(now time.Time) error {
	if i.NodeID == "" || i.ValidatorAddress == "" || i.Payload == "" || i.PublicKey == "" || i.Signature == "" {
		return errMissingTransportIdentityFields
	}
	if i.SignedAt.IsZero() {
		return errTransportIdentityTimestamp
	}
	if i.Payload != i.CanonicalPayload() {
		return errInvalidTransportIdentityPayload
	}
	address, err := tx.DeriveAddressFromPublicKey(i.PublicKey)
	if err != nil {
		return errInvalidTransportIdentityPublicKey
	}
	if address != i.ValidatorAddress {
		return errTransportIdentityAddressMismatch
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	signedAt := i.SignedAt.UTC()
	if signedAt.Before(now.Add(-transportIdentityMaxSkew)) || signedAt.After(now.Add(transportIdentityMaxSkew)) {
		return errTransportIdentityTimestamp
	}
	if err := tx.VerifySignature(i.PublicKey, i.Payload, i.Signature); err != nil {
		switch {
		case errors.Is(err, tx.ErrInvalidPublicKey):
			return errInvalidTransportIdentityPublicKey
		case errors.Is(err, tx.ErrInvalidSignature):
			return errInvalidTransportIdentitySignature
		default:
			return err
		}
	}
	return nil
}

func newTransportIdentitySigner(config Config) (*transportIdentitySigner, Config, error) {
	rawPrivateKey := strings.TrimSpace(config.ValidatorPrivateKey)
	if rawPrivateKey == "" {
		return nil, config, nil
	}

	privateKey, publicKey, err := parseValidatorPrivateKey(rawPrivateKey)
	if err != nil {
		return nil, config, errInvalidValidatorPrivateKey
	}
	address, err := tx.DeriveAddressFromPublicKey(publicKey)
	if err != nil {
		return nil, config, errInvalidValidatorPrivateKey
	}
	if config.ValidatorAddress != "" && config.ValidatorAddress != address {
		return nil, config, errValidatorIdentityMismatch
	}
	config.ValidatorAddress = address

	return &transportIdentitySigner{
		nodeID:           config.NodeID,
		validatorAddress: address,
		publicKey:        publicKey,
		privateKey:       privateKey,
	}, config, nil
}

func (s *transportIdentitySigner) Build(now time.Time) (TransportIdentity, error) {
	if s == nil {
		return TransportIdentity{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	identity := TransportIdentity{
		NodeID:           s.nodeID,
		ValidatorAddress: s.validatorAddress,
		PublicKey:        s.publicKey,
		SignedAt:         now.UTC(),
	}
	identity.Payload = identity.CanonicalPayload()
	signature, err := signTransportIdentityPayload(s.privateKey, identity.Payload)
	if err != nil {
		return TransportIdentity{}, err
	}
	identity.Signature = signature
	return identity, nil
}

func parseValidatorPrivateKey(raw string) (*ecdsa.PrivateKey, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", errInvalidValidatorPrivateKey
	}

	var keyBytes []byte
	if block, _ := pem.Decode([]byte(raw)); block != nil {
		keyBytes = block.Bytes
	} else {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, "", errInvalidValidatorPrivateKey
		}
		keyBytes = decoded
	}

	privateKey, err := parseECDSAPrivateKey(keyBytes)
	if err != nil {
		return nil, "", errInvalidValidatorPrivateKey
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, "", errInvalidValidatorPrivateKey
	}
	return privateKey, base64.StdEncoding.EncodeToString(publicKeyBytes), nil
}

func parseECDSAPrivateKey(keyBytes []byte) (*ecdsa.PrivateKey, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(keyBytes)
	if err == nil {
		privateKey, ok := parsed.(*ecdsa.PrivateKey)
		if !ok || privateKey.Curve.Params().Name != elliptic.P256().Params().Name {
			return nil, errInvalidValidatorPrivateKey
		}
		return privateKey, nil
	}
	privateKey, ecErr := x509.ParseECPrivateKey(keyBytes)
	if ecErr != nil {
		return nil, errInvalidValidatorPrivateKey
	}
	if privateKey.Curve.Params().Name != elliptic.P256().Params().Name {
		return nil, errInvalidValidatorPrivateKey
	}
	return privateKey, nil
}

func signTransportIdentityPayload(privateKey *ecdsa.PrivateKey, payload string) (string, error) {
	digest := sha256.Sum256([]byte(payload))
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		return "", err
	}
	signature := append(padTransportIdentity32(r), padTransportIdentity32(s)...)
	return base64.StdEncoding.EncodeToString(signature), nil
}

func padTransportIdentity32(value *big.Int) []byte {
	bytes := value.Bytes()
	if len(bytes) >= 32 {
		return bytes[len(bytes)-32:]
	}
	padded := make([]byte, 32)
	copy(padded[32-len(bytes):], bytes)
	return padded
}

func transportIdentityFromRequest(r *http.Request) (*TransportIdentity, error) {
	nodeID := strings.TrimSpace(r.Header.Get(sourceNodeHeader))
	validatorAddress := strings.TrimSpace(r.Header.Get(sourceValidatorHeader))
	payload := strings.TrimSpace(r.Header.Get(sourceIdentityPayloadHeader))
	publicKey := strings.TrimSpace(r.Header.Get(sourcePublicKeyHeader))
	signature := strings.TrimSpace(r.Header.Get(sourceSignatureHeader))
	signedAtRaw := strings.TrimSpace(r.Header.Get(sourceSignedAtHeader))

	if nodeID == "" && validatorAddress == "" && payload == "" && publicKey == "" && signature == "" && signedAtRaw == "" {
		return nil, nil
	}
	if validatorAddress == "" && payload == "" && publicKey == "" && signature == "" && signedAtRaw == "" {
		return nil, nil
	}

	signedAt, err := time.Parse(time.RFC3339Nano, signedAtRaw)
	if err != nil {
		return nil, errTransportIdentityTimestamp
	}
	identity := &TransportIdentity{
		NodeID:           nodeID,
		ValidatorAddress: validatorAddress,
		Payload:          payload,
		PublicKey:        publicKey,
		Signature:        signature,
		SignedAt:         signedAt.UTC(),
	}
	if err := identity.ValidateAt(time.Now().UTC()); err != nil {
		return nil, err
	}
	return identity, nil
}

func validateRequestTransportIdentity(r *http.Request) error {
	_, err := transportIdentityFromRequest(r)
	return err
}

func verifyPeerTransportIdentity(status StatusResponse, now time.Time) (bool, string) {
	if status.Identity == nil {
		if status.ValidatorAddress != "" {
			return false, "peer does not expose a signed transport identity"
		}
		return false, ""
	}
	if status.Identity.NodeID != status.NodeID {
		return false, errTransportIdentityNodeMismatch.Error()
	}
	if status.ValidatorAddress != "" && status.Identity.ValidatorAddress != status.ValidatorAddress {
		return false, errTransportIdentityValidatorMismatch.Error()
	}
	if err := status.Identity.ValidateAt(now); err != nil {
		return false, err.Error()
	}
	return true, ""
}
