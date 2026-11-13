package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

func TestCertifiedCatchupRequiresTypedHTTPConflict(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "actual_conflict", err: peerHTTPStatusError{statusCode: http.StatusConflict}, want: true},
		{name: "wrapped_conflict", err: fmt.Errorf("peer operation: %w", peerHTTPStatusError{statusCode: http.StatusConflict}), want: true},
		{name: "fake_conflict_message", err: errors.New("peer returned status 409"), want: false},
		{name: "wrapped_fake_message", err: fmt.Errorf("peer operation: %w", errors.New("peer returned status 409")), want: false},
		{name: "service_unavailable", err: peerHTTPStatusError{statusCode: http.StatusServiceUnavailable}, want: false},
		{name: "no_error", err: nil, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isCertifiedDeliveryConflict(test.err); got != test.want {
				t.Fatalf("isCertifiedDeliveryConflict(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}

	// The actual HTTP transport must preserve the response code instead of
	// manufacturing a text-only error that would silently disable catchup.
	signer := newConsensusSigner(t)
	source := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "typed-conflict-producer",
		ValidatorPrivateKey:   encodedPrivateKey(t, signer.privateKey),
		EnableBlockProduction: false, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0,
		RequireConsensusCertificates: true,
	})
	transport, ok := source.transport.(certifiedBlockDeliveryTransport)
	if !ok {
		t.Fatal("HTTP transport must support signed certified block delivery")
	}
	var statusCode atomic.Int32
	statusCode.Store(http.StatusConflict)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(statusCode.Load()))
	}))
	defer receiver.Close()
	err := transport.PostCertifiedBlock(receiver.URL, certifiedBlockDelivery{Block: ledger.Block{Height: 1}})
	if !isCertifiedDeliveryConflict(err) {
		t.Fatalf("real HTTP 409 failed to trigger typed certified conflict: %v", err)
	}
	statusCode.Store(http.StatusServiceUnavailable)
	err = transport.PostCertifiedBlock(receiver.URL, certifiedBlockDelivery{Block: ledger.Block{Height: 1}})
	if isCertifiedDeliveryConflict(err) {
		t.Fatalf("HTTP 503 must never trigger certified history catchup: %v", err)
	}
}
