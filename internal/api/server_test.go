package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestHandleFaucetAndAccount(t *testing.T) {
	server := NewServer()

	faucetBody := bytes.NewBufferString(`{"address":"zph_test","amount":125}`)
	faucetRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/faucet", faucetBody)
	faucetRecorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(faucetRecorder, faucetRequest)
	if faucetRecorder.Code != http.StatusOK {
		t.Fatalf("expected faucet status 200, got %d", faucetRecorder.Code)
	}

	accountRequest := httptest.NewRequest(http.MethodGet, "/v1/accounts/zph_test", nil)
	accountRecorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(accountRecorder, accountRequest)
	if accountRecorder.Code != http.StatusOK {
		t.Fatalf("expected account status 200, got %d", accountRecorder.Code)
	}

	var response AccountResponse
	if err := json.NewDecoder(accountRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode account response: %v", err)
	}

	if response.Account.Balance != 125 {
		t.Fatalf("expected account balance 125, got %d", response.Account.Balance)
	}
}

func TestHandleBroadcastTransactionRejectsInvalidSignature(t *testing.T) {
	server := NewServer()
	envelope := signedEnvelope(t, 25, 1, "hello")
	server.ledger.Credit(envelope.From, 100)
	envelope.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))

	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestHandleBroadcastTransactionAcceptsFundedTransaction(t *testing.T) {
	server := NewServer()
	envelope := signedEnvelope(t, 25, 1, "hello")
	server.ledger.Credit(envelope.From, 100)

	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", recorder.Code)
	}
}

func signedEnvelope(t *testing.T, amount uint64, nonce uint64, memo string) tx.Envelope {
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
	address, err := tx.DeriveAddressFromPublicKey(encodedPublicKey)
	if err != nil {
		t.Fatalf("derive address: %v", err)
	}

	envelope := tx.Envelope{
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
