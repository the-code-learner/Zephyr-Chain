package tx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
)

var (
	ErrMissingFields         = errors.New("missing required transaction fields")
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrInvalidPayload        = errors.New("payload does not match canonical transaction")
	ErrInvalidPublicKey      = errors.New("invalid public key")
	ErrInvalidAddress        = errors.New("from address does not match public key")
	ErrInvalidSignature      = errors.New("invalid signature")
	ErrNonCanonicalSignature = errors.New("signature must use canonical low-S P-256 form")
	ErrInvalidChainID        = errors.New("transaction chain ID does not match local chain")
	ErrInvalidDomain         = errors.New("invalid transaction signing domain")
)

type Envelope struct {
	ChainID   string `json:"chainId"`
	Domain    string `json:"domain"`
	From      string `json:"from"`
	To        string `json:"to"`
	Amount    uint64 `json:"amount"`
	Nonce     uint64 `json:"nonce"`
	Memo      string `json:"memo"`
	Payload   string `json:"payload"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
}

type canonicalPayload struct {
	Amount  uint64 `json:"amount"`
	ChainID string `json:"chainId"`
	Domain  string `json:"domain"`
	From    string `json:"from"`
	Memo    string `json:"memo"`
	Nonce   uint64 `json:"nonce"`
	To      string `json:"to"`
}

type canonicalIdentity struct {
	Amount    uint64 `json:"amount"`
	ChainID   string `json:"chainId"`
	Domain    string `json:"domain"`
	From      string `json:"from"`
	Memo      string `json:"memo"`
	Nonce     uint64 `json:"nonce"`
	PublicKey string `json:"publicKey"`
	To        string `json:"to"`
}

func (e Envelope) CanonicalPayload() string {
	payload, _ := json.Marshal(canonicalPayload{
		Amount:  e.Amount,
		ChainID: strings.TrimSpace(e.ChainID),
		Domain:  strings.TrimSpace(e.Domain),
		From:    e.From,
		Memo:    e.Memo,
		Nonce:   e.Nonce,
		To:      e.To,
	})
	return string(payload)
}

func (e Envelope) ValidateStatic() error {
	chainID := strings.TrimSpace(e.ChainID)
	domain := strings.TrimSpace(e.Domain)
	if chainID == "" || domain == "" || e.From == "" || e.To == "" || e.Payload == "" || e.PublicKey == "" || e.Signature == "" {
		return ErrMissingFields
	}
	if err := protocol.ValidateChainID(chainID); err != nil {
		return ErrInvalidChainID
	}
	if domain != protocol.TransactionDomain {
		return ErrInvalidDomain
	}
	if e.Amount == 0 {
		return ErrInvalidAmount
	}

	address, err := DeriveAddressFromPublicKey(e.PublicKey)
	if err != nil {
		return err
	}
	if address != e.From {
		return ErrInvalidAddress
	}
	if e.Payload != e.CanonicalPayload() {
		return ErrInvalidPayload
	}
	return VerifySignature(e.PublicKey, e.Payload, e.Signature)
}

func (e Envelope) ValidateForChain(expectedChainID string) error {
	if err := e.ValidateStatic(); err != nil {
		return err
	}
	expectedChainID = strings.TrimSpace(expectedChainID)
	if err := protocol.ValidateChainID(expectedChainID); err != nil || strings.TrimSpace(e.ChainID) != expectedChainID {
		return ErrInvalidChainID
	}
	return nil
}

func DeriveAddressFromPublicKey(encodedPublicKey string) (string, error) {
	publicKeyBytes, err := base64.StdEncoding.DecodeString(encodedPublicKey)
	if err != nil {
		return "", ErrInvalidPublicKey
	}
	parsedPublicKey, err := x509.ParsePKIXPublicKey(publicKeyBytes)
	if err != nil {
		return "", ErrInvalidPublicKey
	}
	publicKey, ok := parsedPublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve.Params().Name != elliptic.P256().Params().Name {
		return "", ErrInvalidPublicKey
	}

	sum := sha256.Sum256(publicKeyBytes)
	return "zph_" + hex.EncodeToString(sum[:])[:40], nil
}

func SignPayload(privateKey *ecdsa.PrivateKey, payload string) (string, error) {
	if privateKey == nil || privateKey.Curve == nil || privateKey.Curve.Params().Name != elliptic.P256().Params().Name {
		return "", ErrInvalidPublicKey
	}
	digest := sha256.Sum256([]byte(payload))
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		return "", err
	}
	s = normalizeLowS(s)
	signature := append(padP256Int32(r), padP256Int32(s)...)
	return base64.StdEncoding.EncodeToString(signature), nil
}

func NormalizeP256Signature(encodedSignature string) (string, error) {
	r, s, err := decodeSignature(encodedSignature)
	if err != nil {
		return "", err
	}
	s = normalizeLowS(s)
	signature := append(padP256Int32(r), padP256Int32(s)...)
	return base64.StdEncoding.EncodeToString(signature), nil
}

func IsCanonicalP256Signature(encodedSignature string) bool {
	_, s, err := decodeSignature(encodedSignature)
	if err != nil {
		return false
	}
	return s.Cmp(halfOrder()) <= 0
}

func VerifySignature(encodedPublicKey string, payload string, encodedSignature string) error {
	publicKeyBytes, err := base64.StdEncoding.DecodeString(encodedPublicKey)
	if err != nil {
		return ErrInvalidPublicKey
	}
	parsedPublicKey, err := x509.ParsePKIXPublicKey(publicKeyBytes)
	if err != nil {
		return ErrInvalidPublicKey
	}
	publicKey, ok := parsedPublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve.Params().Name != elliptic.P256().Params().Name {
		return ErrInvalidPublicKey
	}

	r, s, err := decodeSignature(encodedSignature)
	if err != nil {
		return err
	}
	if s.Cmp(halfOrder()) > 0 {
		return ErrNonCanonicalSignature
	}
	digest := sha256.Sum256([]byte(payload))
	if !ecdsa.Verify(publicKey, digest[:], r, s) {
		return ErrInvalidSignature
	}
	return nil
}

func ID(e Envelope) string {
	payload, _ := json.Marshal(canonicalIdentity{
		Amount:    e.Amount,
		ChainID:   strings.TrimSpace(e.ChainID),
		Domain:    strings.TrimSpace(e.Domain),
		From:      e.From,
		Memo:      e.Memo,
		Nonce:     e.Nonce,
		PublicKey: e.PublicKey,
		To:        e.To,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func decodeSignature(encodedSignature string) (*big.Int, *big.Int, error) {
	signatureBytes, err := base64.StdEncoding.DecodeString(encodedSignature)
	if err != nil || len(signatureBytes) != 64 {
		return nil, nil, ErrInvalidSignature
	}
	r := new(big.Int).SetBytes(signatureBytes[:32])
	s := new(big.Int).SetBytes(signatureBytes[32:])
	order := elliptic.P256().Params().N
	if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(order) >= 0 || s.Cmp(order) >= 0 {
		return nil, nil, ErrInvalidSignature
	}
	return r, s, nil
}

func normalizeLowS(s *big.Int) *big.Int {
	if s.Cmp(halfOrder()) <= 0 {
		return new(big.Int).Set(s)
	}
	return new(big.Int).Sub(elliptic.P256().Params().N, s)
}

func halfOrder() *big.Int {
	return new(big.Int).Rsh(new(big.Int).Set(elliptic.P256().Params().N), 1)
}

func padP256Int32(value *big.Int) []byte {
	bytes := value.Bytes()
	if len(bytes) >= 32 {
		return bytes[len(bytes)-32:]
	}
	padded := make([]byte, 32)
	copy(padded[32-len(bytes):], bytes)
	return padded
}
