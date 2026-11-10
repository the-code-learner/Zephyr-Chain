package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// An admitted peer endpoint is not permitted to redirect a signed request
// proof to another host. The test intentionally supplies a default client
// that WOULD follow redirects; newHTTPPeerTransport must override its policy
// without modifying the caller-owned client.
func TestHTTPPeerTransportRejectsSignedRedirects(t *testing.T) {
	signer := newConsensusSigner(t)
	config := DefaultConfig()
	config.NodeID = "redirect-proof-sender"
	config.ValidatorPrivateKey = encodedPrivateKey(t, signer.privateKey)
	identity, _, err := newTransportIdentitySigner(config)
	if err != nil || identity == nil {
		t.Fatalf("build signed peer identity: %v", err)
	}
	var forwarded atomic.Int32
	offsite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer offsite.Close()

	var redirectCode atomic.Int32
	redirectCode.Store(http.StatusTemporaryRedirect)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" && r.Header.Get(sourceSignatureHeader) == "" {
			http.Error(w, "missing request proof", http.StatusForbidden)
			return
		}
		w.Header().Set("Location", offsite.URL+"/unapproved-peer")
		w.WriteHeader(int(redirectCode.Load()))
	}))
	defer source.Close()

	inputClient := &http.Client{Timeout: 3 * time.Second}
	transport := newHTTPPeerTransport(inputClient, config.NodeID, identity)
	if inputClient.CheckRedirect != nil {
		t.Fatal("transport constructor modified caller-owned client")
	}
	proposal := consensus.Proposal{Height: 1, Round: 1}
	vote := consensus.Vote{Height: 1, Round: 1}
	for _, code := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		redirectCode.Store(int32(code))
		t.Run(fmt.Sprintf("status-%d", code), func(t *testing.T) {
			check := func(action string, err error) {
				t.Helper()
				if err == nil || err.Error() != fmt.Sprintf("peer returned status %d", code) {
					t.Fatalf("%s silently accepted peer redirect %d: %v", action, code, err)
				}
			}
			check("proposal", transport.PostProposal(source.URL, proposal))
			check("vote", transport.PostVote(source.URL, vote))
			check("transaction", transport.PostTransaction(source.URL, tx.Envelope{}))
			_, err := transport.FetchBlockEvidence(source.URL, 1)
			check("signed block evidence", err)
			_, err = transport.FetchSnapshot(source.URL)
			check("signed snapshot", err)
			_, err = transport.FetchStatus(source.URL)
			check("peer status", err)
			if got := forwarded.Load(); got != 0 {
				t.Fatalf("unapproved host received %d redirected requests", got)
			}
		})
	}

	// Retain normal final acknowledgements (200/201/202/204); never make
	// a 3xx response equivalent to a signed successful delivery.
	redirectCode.Store(http.StatusAccepted)
	if err := transport.PostProposal(source.URL, proposal); err != nil {
		t.Fatalf("valid consensus 202 rejected: %v", err)
	}
	if err := transport.PostTransaction(source.URL, tx.Envelope{}); err != nil {
		t.Fatalf("valid transaction 202 rejected: %v", err)
	}
}

func TestServerPeerClientIgnoresAmbientProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://unapproved-proxy.example.invalid:3128")
	t.Setenv("HTTPS_PROXY", "http://unapproved-proxy.example.invalid:3128")
	server := newTestServer(t, Config{
		DataDir:        t.TempDir(),
		BlockInterval:  0,
		SyncInterval:   0,
		EnablePeerSync: false,
	})
	transport, ok := server.httpClient.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("server peer client inherited an ambient HTTP(S)_PROXY")
	}
	if server.httpClient.CheckRedirect == nil {
		t.Fatal("server peer client can follow an unapproved redirect")
	}
	if server.httpClient.Timeout != 5*time.Second {
		t.Fatalf("server peer timeout changed: %v", server.httpClient.Timeout)
	}
	peer, ok := server.transport.(*httpPeerTransport)
	if !ok || peer.client.CheckRedirect == nil {
		t.Fatal("signed peer transport lacks a redirect-denial policy")
	}
}
