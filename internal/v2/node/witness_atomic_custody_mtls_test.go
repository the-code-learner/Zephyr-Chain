//go:build linux || darwin

package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
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
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{CommonName: "Zephyr custody test CA"},
		NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(time.Hour),
		IsCA: true,
		BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
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
		NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
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

func TestAtomicWitnessCustodyMTLSVerifiedEndToEnd(t *testing.T) {
	ca, caKey, pool := witnessCustodyTestCA(t)
	serverCert := witnessCustodyTestTLSCertificate(t, ca, caKey, 2, false)
	clientCert := witnessCustodyTestTLSCertificate(t, ca, caKey, 3, true)
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-mtls", []byte("network")))
	root := types.HashBytes("custody-mtls", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 74, HeaderHash: types.Hash{8}}
	backend := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	handler, err := NewHTTPWitnessCustodyHandler(backend, network, root, keyA, keyW)
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
	config := &tls.Config{
		RootCAs: pool,
		Certificates: []tls.Certificate{clientCert},
		ServerName: "custody.operator.example.org",
		MinVersion: tls.VersionTLS13,
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
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	j := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}})
	a, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, remote, keyA, keyW)
	if err != nil || a.Revision != 1 || backend.calls != 1 {
		t.Fatalf("real mutual TLS custody round trip failed: attestation=%+v calls=%d err=%v", a, backend.calls, err)
	}
	// A client without a certificate must fail the server-side TLS handshake.
	noCert := &HTTPWitnessAtomicCustodian{
		endpoint: endpoint,
		client: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "custody.operator.example.org", MinVersion: tls.VersionTLS13},
			DialContext: transport.DialContext,
		}},
	}
	if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, noCert, keyA, keyW); err == nil {
		t.Fatal("anonymous TLS client reached protected custody")
	}
	if backend.calls != 1 {
		t.Fatalf("anonymous client reached backend: calls=%d", backend.calls)
	}
}
