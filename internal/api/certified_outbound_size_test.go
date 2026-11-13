package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// The wire cap is expressed in actual JSON bytes, not the input memo's
// length. Escaping a small raw string can amplify it beyond the HTTP limit.
func TestSignedCertifiedOutboundRejectsOversizedJSONBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer receiver.Close()
	transport := newHTTPPeerTransport(receiver.Client(), "guarded-validator", nil)
	signed, ok := transport.(certifiedBlockDeliveryTransport)
	if !ok {
		t.Fatal("signed certified transport unavailable")
	}
	// JSON escaping encodes every NUL as \\u0000, six wire bytes each.
	// The raw memo is roughly 11MiB but the signed delivery is >64MiB.
	delivery := certifiedBlockDelivery{Block: ledger.Block{
		Height:       1,
		Transactions: []tx.Envelope{{Memo: strings.Repeat("\x00", int(maxCertifiedBlockDeliveryBytes/6)+128)}},
	}}
	if err := signed.PostCertifiedBlock(receiver.URL, delivery); !errors.Is(err, errCertifiedDeliveryTooLarge) {
		t.Fatalf("oversized outbound signed delivery not rejected locally: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("oversized signed delivery reached network: requests=%d", got)
	}
	// The size preflight must not reject a normal packet; without a
	// configured transport signer this reaches the separate proof check.
	delivery.Block.Transactions[0].Memo = "small"
	if err := signed.PostCertifiedBlock(receiver.URL, delivery); !errors.Is(err, errMissingRequestProof) {
		t.Fatalf("normal-size delivery unexpectedly rejected at size guard: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("request without valid identity unexpectedly reached network: %d", got)
	}
}
