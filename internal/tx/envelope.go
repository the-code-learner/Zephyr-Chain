package tx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
)

var (
	ErrMissingFields    = errors.New("missing required transaction fields")
	ErrInvalidAmount    = errors.New("amount must be greater than zero")
	ErrInvalidPayload   = errors.New("payload does not match canonical transaction")
	ErrInvalidPublicKey = errors.New("invalid public key")
	ErrInvalidAddress   = errors.New("from address does not match public key")
	ErrInvalidSignature = errors.New("invalid signature")
)

type Envelope struct {
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
	Amount uint64 `json:"amount"`
	From   string `json:"from"`
	Memo   string `json:"memo"`
	Nonce  uint64 `json:"nonce"`
	To     string `json:"to"`
}

func (e Envelope) CanonicalPayload() string {
	payload, _ := json.Marshal(canonicalPayload{
		Amount: e.Amount,
		From:   e.From,
		Memo:   e.Memo,
		Nonce:  e.Nonce,
		To:     e.To,
	})

	return string(payload)
}

func (e Envelope) ValidateStatic() error {
	if e.From == "" || e.To == "" || e.Payload == "" || e.PublicKey == "" || e.Signature == "" {
		return ErrMissingFields
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

func DeriveAddressFromPublicKey(encodedPublicKey string) (string, error) {
	publicKeyBytes, err := base64.StdEncoding.DecodeString(encodedPublicKey)
	if err != nil {
		return "", ErrInvalidPublicKey
	}

	sum := sha256.Sum256(publicKeyBytes)
	return "zph_" + hex.EncodeToString(sum[:])[:40], nil
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

	signatureBytes, err := base64.StdEncoding.DecodeString(encodedSignature)
	if err != nil || len(signatureBytes) != 64 {
		return ErrInvalidSignature
	}

	r := new(big.Int).SetBytes(signatureBytes[:32])
	s := new(big.Int).SetBytes(signatureBytes[32:])
	digest := sha256.Sum256([]byte(payload))

	if !ecdsa.Verify(publicKey, digest[:], r, s) {
		return ErrInvalidSignature
	}

	return nil
}

func ID(e Envelope) string {
	payload, _ := json.Marshal(struct {
		From      string `json:"from"`
		To        string `json:"to"`
		Amount    uint64 `json:"amount"`
		Nonce     uint64 `json:"nonce"`
		Memo      string `json:"memo"`
		Payload   string `json:"payload"`
		PublicKey string `json:"publicKey"`
		Signature string `json:"signature"`
	}{
		From:      e.From,
		To:        e.To,
		Amount:    e.Amount,
		Nonce:     e.Nonce,
		Memo:      e.Memo,
		Payload:   e.Payload,
		PublicKey: e.PublicKey,
		Signature: e.Signature,
	})

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
