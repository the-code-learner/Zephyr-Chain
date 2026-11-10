//go:build linux || darwin

package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func witnessCustodyTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Zephyr custody test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return cert, key, pool
}

func witnessCustodyTestTLSCertificate(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, serial int64, client bool) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if client {
		template.Subject = pkix.Name{CommonName: "Zephyr custody test client"}
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		template.DNSNames = []string{"custody.operator.example.org"}
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}
}

// The server handler runs on a separate goroutine. Protect the test-only
// fixture's call counter explicitly, independent of HTTP response timing.
type witnessSynchronizedCustodyFixture struct {
	mu    sync.Mutex
	inner *witnessAtomicCustodianFixture
}

func (f *witnessSynchronizedCustodyFixture) ReserveAndSignWitnessCheckpoint(ctx context.Context, cp FinalityCheckpoint, challenge [32]byte) (WitnessAtomicCustodyAttestation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inner.ReserveAndSignWitnessCheckpoint(ctx, cp, challenge)
}

func (f *witnessSynchronizedCustodyFixture) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inner.calls
}

func TestAtomicWitnessCustodyMTLSVerifiedEndToEnd(t *testing.T) {
	ca, caKey, pool := witnessCustodyTestCA(t)
	serverCert := witnessCustodyTestTLSCertificate(t, ca, caKey, 2, false)
	clientCert := witnessCustodyTestTLSCertificate(t, ca, caKey, 3, true)
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-mtls", []byte("network")))
	root := types.HashBytes("custody-mtls", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 74, HeaderHash: types.Hash{8}}
	backend := &witnessSynchronizedCustodyFixture{inner: &witnessAtomicCustodianFixture{authority: authority, witness: witness}}
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	approvedLeaf, err := x509.ParseCertificate(clientCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	approvedPin := sha256.Sum256(approvedLeaf.RawSubjectPublicKeyInfo)
	for _, invalid := range [][][32]byte{
		nil, {{0}}, {approvedPin, approvedPin},
	} {
		if _, err := NewPinnedHTTPWitnessCustodyHandler(backend, network, root, keyA, keyW, invalid); err == nil {
			t.Fatal("invalid custody client pin policy accepted")
		}
	}
	handler, err := NewPinnedHTTPWitnessCustodyHandler(backend, network, root, keyA, keyW, [][32]byte{approvedPin})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}
	server.StartTLS()
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "https://custody.operator.example.org:" + port + witnessCustodyHandlerPath
	config := &tls.Config{
		RootCAs:      pool,
		Certificates: []tls.Certificate{clientCert},
		ServerName:   "custody.operator.example.org",
		MinVersion:   tls.VersionTLS13,
	}
	for _, invalid := range []*tls.Config{
		nil,
		{RootCAs: pool},
		{RootCAs: pool, Certificates: []tls.Certificate{clientCert}, InsecureSkipVerify: true},
		{RootCAs: pool, Certificates: []tls.Certificate{clientCert}, ServerName: "other.example.org"},
	} {
		if _, err := NewHTTPWitnessAtomicCustodianWithTLS(endpoint, invalid); err == nil {
			t.Fatal("insecure client TLS configuration accepted")
		}
	}
	remote, err := NewHTTPWitnessAtomicCustodianWithTLS(endpoint, config)
	if err != nil {
		t.Fatal(err)
	}
	// Route the synthetic public hostname to the local test listener while
	// retaining its DNS name for real certificate/hostname verification.
	transport := remote.client.Transport.(*http.Transport)
	if !transport.DisableCompression || transport.Proxy != nil {
		t.Fatal("mTLS custody transport negotiated compression or inherited an ambient proxy")
	}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	j := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}})
	a, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, remote, keyA, keyW)
	if err != nil || a.Revision != 1 || backend.calls() != 1 {
		t.Fatalf("real mutual TLS custody round trip failed: attestation=%+v calls=%d err=%v", a, backend.calls(), err)
	}
	// A client without a certificate must fail the server-side TLS handshake.
	noCert := &HTTPWitnessAtomicCustodian{
		endpoint: endpoint,
		client: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "custody.operator.example.org", MinVersion: tls.VersionTLS13},
			DialContext:     transport.DialContext,
		}},
	}
	if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, noCert, keyA, keyW); err == nil {
		t.Fatal("anonymous TLS client reached protected custody")
	}
	if backend.calls() != 1 {
		t.Fatalf("anonymous client reached backend: calls=%d", backend.calls())
	}
	// This second client was signed by the SAME trusted mTLS CA. Its TLS
	// handshake succeeds, but the handler must reject it BEFORE key custody.
	rogueCert := witnessCustodyTestTLSCertificate(t, ca, caKey, 4, true)
	rogueConfig := config.Clone()
	rogueConfig.Certificates = []tls.Certificate{rogueCert}
	rogue, err := NewHTTPWitnessAtomicCustodianWithTLS(endpoint, rogueConfig)
	if err != nil {
		t.Fatal(err)
	}
	rogue.client.Transport.(*http.Transport).DialContext = transport.DialContext
	if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, rogue, keyA, keyW); err == nil {
		t.Fatal("CA-trusted but unpinned client reached protected custody")
	}
	if backend.calls() != 1 {
		t.Fatalf("unauthorized CA-trusted client invoked signer: calls=%d", backend.calls())
	}
}
