//go:build linux || darwin

package node

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Exercises the complete untrusted network -> pinned quorum -> durable
// local journal reconciliation boundary with a real TLS HTTP test server.
// No test-only transport trust is sufficient to authorize checkpoint bytes.
func TestWitnessHTTPSQuorumRecoveryEndToEnd(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-https-e2e", []byte("network")))
	root := types.HashBytes("witness-https-e2e", []byte("root"))
	keys, private := witnessTestKeys()
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 23, HeaderHash: types.Hash{4}}
	valid := signedWitnessTestBundle(t, cp, keys, private, 3)
	conflict := cp
	conflict.HeaderHash[0]++
	conflicting := signedWitnessTestBundle(t, conflict, keys, private, 3)

	payload := valid
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	source, err := NewHTTPWitnessFloorSource(server.URL + "/checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	// The test server has ephemeral private TLS roots. Do not weaken TLS
	// verification in production: test-only transport certificate trust.
	testClient := server.Client()
	source.client = testClient
	defer testClient.CloseIdleConnections()
	if _, ok := testClient.Transport.(*http.Transport); !ok {
		t.Fatal("TLS test transport unavailable")
	}
	_ = tls.VersionTLS12

	remote, err := NewQuorumAuthenticatedWitnessFloor(source, keys, network, root)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "intent")
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	j, err := OpenWitnessSigningIntentJournal(path, network, root, true, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.ReserveFinalizedIntent(cp); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileWitnessIntentWithExternalFloor(j, remote); err != nil {
		t.Fatalf("valid signed TLS quorum failed to reconcile: %v", err)
	}
	payload = conflicting
	if err := ReconcileWitnessIntentWithExternalFloor(j, remote); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("valid but contradictory quorum accepted: %v", err)
	}
	payload = []byte("{}")
	if err := ReconcileWitnessIntentWithExternalFloor(j, remote); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("unsigned HTTP checkpoint accepted: %v", err)
	}
	payload = valid
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenWitnessSigningIntentJournal(path, network, root, false, verifier)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := ReconcileWitnessIntentWithExternalFloor(j, remote); err != nil {
		t.Fatalf("valid quorum rejected after journal restart: %v", err)
	}
}
