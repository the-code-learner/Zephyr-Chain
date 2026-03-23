package tx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"
)

func TestEnvelopeValidateStaticAcceptsWalletCompatibleSignature(t *testing.T) {
	envelope := signedEnvelope(t, 25, 1, "note")

	if err := envelope.ValidateStatic(); err != nil {
		t.Fatalf("expected valid envelope, got error: %v", err)
	}

	if !strings.HasPrefix(envelope.From, "zph_") {
		t.Fatalf("expected zephyr-style address, got %s", envelope.From)
	}
}

func TestEnvelopeValidateStaticRejectsPayloadMismatch(t *testing.T) {
	envelope := signedEnvelope(t, 25, 1, "note")
	envelope.Payload = "{}"

	if err := envelope.ValidateStatic(); err != ErrInvalidPayload {
		t.Fatalf("expected invalid payload error, got %v", err)
	}
}

func TestEnvelopeValidateStaticRejectsAddressMismatch(t *testing.T) {
	envelope := signedEnvelope(t, 25, 1, "note")
	envelope.From = "zph_not_the_real_sender"

	if err := envelope.ValidateStatic(); err != ErrInvalidAddress {
		t.Fatalf("expected invalid address error, got %v", err)
	}
}

func signedEnvelope(t *testing.T, amount uint64, nonce uint64, memo string) Envelope {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	encodedPublicKey := base64.StdEncoding.EncodeToString(publicKeyBytes)
	address, err := DeriveAddressFromPublicKey(encodedPublicKey)
	if err != nil {
		t.Fatalf("derive address: %v", err)
	}

	envelope := Envelope{
		From:      address,
		To:        "zph_receiver",
		Amount:    amount,
		Nonce:     nonce,
		Memo:      memo,
		PublicKey: encodedPublicKey,
	}
	envelope.Payload = envelope.CanonicalPayload()
	envelope.Signature = signPayload(t, privateKey, envelope.Payload)

	return envelope
}

func signPayload(t *testing.T, privateKey *ecdsa.PrivateKey, payload string) string {
	t.Helper()

	digest := sha256.Sum256([]byte(payload))
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}

	signature := append(pad32(r), pad32(s)...)
	return base64.StdEncoding.EncodeToString(signature)
}

func pad32(value *big.Int) []byte {
	bytes := value.Bytes()
	if len(bytes) >= 32 {
		return bytes[len(bytes)-32:]
	}

	padded := make([]byte, 32)
	copy(padded[32-len(bytes):], bytes)
	return padded
}
