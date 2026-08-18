package tx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
)

func TestEnvelopeValidateStaticAcceptsWalletCompatibleSignature(t *testing.T) {
	envelope := signedEnvelope(t, 25, 1, "note")

	if err := envelope.ValidateStatic(); err != nil {
		t.Fatalf("expected valid envelope, got error: %v", err)
	}
	if !strings.HasPrefix(envelope.From, "zph_") {
		t.Fatalf("expected zephyr-style address, got %s", envelope.From)
	}
	if !IsCanonicalP256Signature(envelope.Signature) {
		t.Fatal("expected signer to emit a low-S signature")
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

func TestEnvelopeRequiresExplicitChainAndDomain(t *testing.T) {
	envelope := signedEnvelope(t, 25, 1, "note")
	envelope.ChainID = ""
	if err := envelope.ValidateStatic(); !errors.Is(err, ErrMissingFields) {
		t.Fatalf("expected missing chain to fail closed, got %v", err)
	}

	envelope = signedEnvelope(t, 25, 1, "note")
	envelope.Domain = ""
	if err := envelope.ValidateStatic(); !errors.Is(err, ErrMissingFields) {
		t.Fatalf("expected missing domain to fail closed, got %v", err)
	}
}

func TestEnvelopeRejectsCrossChainReplay(t *testing.T) {
	envelope := signedEnvelope(t, 25, 1, "note")
	if err := envelope.ValidateForChain("zephyr-testnet-1"); !errors.Is(err, ErrInvalidChainID) {
		t.Fatalf("expected cross-chain replay rejection, got %v", err)
	}
}

func TestEnvelopeRejectsHighSSignatureButKeepsStableTransactionID(t *testing.T) {
	envelope := signedEnvelope(t, 25, 1, "note")
	malleated := envelope
	malleated.Signature = highSEquivalent(t, envelope.Signature)

	if ID(envelope) != ID(malleated) {
		t.Fatal("transaction ID must not depend on signature representation")
	}
	if err := malleated.ValidateStatic(); !errors.Is(err, ErrNonCanonicalSignature) {
		t.Fatalf("expected high-S signature rejection, got %v", err)
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
		ChainID:   protocol.DefaultChainID,
		Domain:    protocol.TransactionDomain,
		From:      address,
		To:        "zph_receiver",
		Amount:    amount,
		Nonce:     nonce,
		Memo:      memo,
		PublicKey: encodedPublicKey,
	}
	envelope.Payload = envelope.CanonicalPayload()
	envelope.Signature, err = SignPayload(privateKey, envelope.Payload)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	return envelope
}

func highSEquivalent(t *testing.T, encodedSignature string) string {
	t.Helper()
	r, s, err := decodeSignature(encodedSignature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	highS := new(big.Int).Sub(elliptic.P256().Params().N, s)
	if highS.Cmp(halfOrder()) <= 0 {
		t.Fatal("expected high-S equivalent")
	}
	raw := append(padP256Int32(r), padP256Int32(highS)...)
	return base64.StdEncoding.EncodeToString(raw)
}
