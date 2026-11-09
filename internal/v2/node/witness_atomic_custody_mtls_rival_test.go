//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Two independently authenticated TLS clients can submit conflicting QC
// claims, but a single durable custody anchor must authorize at most one.
// The distinct certs are signed by one ephemeral test CA: this is a protocol
// regression, NOT a geographically independent operator deployment or HSM.
func TestAtomicWitnessCustodyMTLSTwoClientsCannotEquivocate(t *testing.T) {
	ca, caKey, pool := witnessCustodyTestCA(t)
	serverCert := witnessCustodyTestTLSCertificate(t, ca, caKey, 20, false)
	clientCertA := witnessCustodyTestTLSCertificate(t, ca, caKey, 21, true)
	clientCertB := witnessCustodyTestTLSCertificate(t, ca, caKey, 22, true)
	leafA, err := x509.ParseCertificate(clientCertA.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	leafB, err := x509.ParseCertificate(clientCertB.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	authority, witness := witnessAtomicTestKeys()
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	network := types.NetworkID(types.HashBytes("mtls-rival-clients", []byte("network")))
	root := types.HashBytes("mtls-rival-clients", []byte("root"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 80, HeaderHash: types.Hash{1}}
	conflict := first
	conflict.HeaderHash = types.Hash{2}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "exclusive-remote-custody")
	signingProvider := &localCustodyTestSigner{witness: witness, authority: authority}
	// The QC verifier is intentionally permissive in this adversarial test.
	// The custody reservation must still prevent same-height equivocation.
	verifier := witnessFaultyCustodyVerifier{}
	open := func(create bool) *LocalWitnessCustodyBackend {
		t.Helper()
		backend, err := OpenLocalWitnessCustodyBackend(path, network, root, create, verifier, signingProvider, keyW, keyA)
		if err != nil {
			t.Fatal(err)
		}
		return backend
	}
	backend := open(true)
	defer func() { _ = backend.Close() }()
	handler, err := NewPinnedHTTPWitnessCustodyHandler(backend, network, root, keyA, keyW,
		[][32]byte{sha256.Sum256(leafA.RawSubjectPublicKeyInfo), sha256.Sum256(leafB.RawSubjectPublicKeyInfo)})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs: pool,
		MinVersion: tls.VersionTLS13,
	}
	server.StartTLS()
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "https://custody.operator.example.org:" + port + witnessCustodyHandlerPath
	newRemote := func(cert tls.Certificate) *HTTPWitnessAtomicCustodian {
		t.Helper()
		remote, err := NewHTTPWitnessAtomicCustodianWithTLS(endpoint, &tls.Config{
			RootCAs: pool,
			Certificates: []tls.Certificate{cert},
			ServerName: "custody.operator.example.org",
			MinVersion: tls.VersionTLS13,
		})
		if err != nil {
			t.Fatal(err)
		}
		remote.client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}
		return remote
	}
	clientA := newRemote(clientCertA)
	clientB := newRemote(clientCertB)
	journalA := witnessFenceTestJournal(t, first,
		witnessIntentTestVerifier{allowed: map[uint64]types.Hash{first.Height: first.HeaderHash}})
	journalB := witnessFenceTestJournal(t, first,
		witnessIntentTestVerifier{allowed: map[uint64]types.Hash{conflict.Height: conflict.HeaderHash}})
	var wg sync.WaitGroup
	var results [2]WitnessAtomicCustodyAttestation
	var errs [2]error
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0], errs[0] = ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), journalA, first, clientA, keyA, keyW)
	}()
	go func() {
		defer wg.Done()
		results[1], errs[1] = ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), journalB, conflict, clientB, keyA, keyW)
	}()
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("zero or two contradictory TLS attestations succeeded: %v / %v", errs[0], errs[1])
	}
	winner, loser := first, conflict
	if errs[0] != nil {
		winner, loser = conflict, first
	}
	for i, result := range results {
		if errs[i] == nil {
			expected := first
			if i == 1 {
				expected = conflict
			}
			if err := VerifyWitnessAtomicCustodyAttestation(result, expected, result.Challenge, keyA, keyW); err != nil {
				t.Fatalf("winner produced unauthenticated evidence: %v", err)
			}
		} else if !errors.Is(errs[i], ErrWitnessSigningIntent) {
			t.Fatalf("rejected client returned wrong error: %v", errs[i])
		}
	}
	current, err := backend.Current()
	if err != nil || current != winner {
		t.Fatalf("remote custody disagrees with sole signing decision: %+v %v", current, err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	backend = open(false)
	// Even after reopening, the other certified client cannot elicit a
	// conflicting signature from the same single-writer reference ledger.
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), loser, [32]byte{9}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("conflicting checkpoint was signed after restart: %v", err)
	}
	if current, err = backend.Current(); err != nil || current != winner {
		t.Fatalf("restart lost the custody decision: %+v %v", current, err)
	}
}
