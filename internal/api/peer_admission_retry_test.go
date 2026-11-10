package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A peer which is unreachable during the first proof-bearing status check
// must be recoverable even if periodic block/peer synchronization is disabled.
// Recovery does not weaken validator pins: a NEW signed status identity must
// be verified before the peer can be included in a broadcast.
func TestPeerAdmissionRetryRecoversVerifiedPeerWithoutPeerSync(t *testing.T) {
	signer := newConsensusSigner(t)
	peer := newTestServer(t, Config{
		DataDir:             t.TempDir(),
		NodeID:              "peer-b",
		ValidatorPrivateKey: encodedPrivateKey(t, signer.privateKey),
		BlockInterval:       0,
		SyncInterval:        0,
		EnablePeerSync:      false,
	})
	var online atomic.Bool
	var statusCalls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			statusCalls.Add(1)
			if !online.Load() {
				http.Error(w, "offline", http.StatusServiceUnavailable)
				return
			}
		}
		peer.Handler().ServeHTTP(w, r)
	}))
	defer remote.Close()
	caller := newTestServer(t, Config{
		DataDir:               t.TempDir(),
		NodeID:                "peer-a",
		PeerURLs:              []string{remote.URL},
		PeerValidatorBindings: map[string]string{remote.URL: signer.address},
		RequirePeerIdentity:   true,
		BlockInterval:         0,
		SyncInterval:          0,
		EnablePeerSync:        false,
	})
	if peers := caller.admittedPeerURLs(); len(peers) != 0 {
		t.Fatalf("offline peer admitted: %v", peers)
	}
	previous, ok := caller.peerView(remote.URL)
	if !ok || previous.Reachable || previous.LastAdmissionAttemptAt == nil {
		t.Fatalf("first failed admission probe was not recorded: %+v", previous)
	}
	if peers := caller.admittedPeerURLs(); len(peers) != 0 || statusCalls.Load() != 1 {
		t.Fatalf("offline probe retried without backoff: peers=%v calls=%d", peers, statusCalls.Load())
	}

	online.Store(true)
	// Force eligibility by setting only the last probe timestamp. The
	// production interval stays at two seconds; no sleeps in the test.
	caller.peerMu.Lock()
	view := caller.peerViews[remote.URL]
	old := time.Now().UTC().Add(-2 * peerAdmissionRetryInterval)
	view.LastAdmissionAttemptAt = &old
	caller.peerViews[remote.URL] = view
	caller.peerMu.Unlock()

	// Concurrent broadcasts after the backoff must perform only one
	// fresh signed admission probe instead of flooding a recovering peer.
	const parallel = 12
	var wg sync.WaitGroup
	begin := make(chan struct{})
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-begin
			_ = caller.admittedPeerURLs()
		}()
	}
	close(begin)
	wg.Wait()
	if statusCalls.Load() != 2 {
		t.Fatalf("recovering peer was stampeded: status calls=%d, want 2", statusCalls.Load())
	}
	accepted := caller.admittedPeerURLs()
	if len(accepted) != 1 || accepted[0] != remote.URL {
		t.Fatalf("peer failed authenticated recovery: %v", accepted)
	}
	recovered, _ := caller.peerView(remote.URL)
	if !recovered.Reachable || !recovered.IdentityVerified || !recovered.Admitted ||
		recovered.ValidatorAddress != signer.address || recovered.LastAdmissionAttemptAt == nil {
		t.Fatalf("retry bypassed pinned validator admission: %+v", recovered)
	}
}

// A reachable peer with a genuine signed identity for the WRONG validator
// must stay excluded even when a caller tries to force an old retry timestamp.
// Only transport-unreachable peers are auto-reprobed; a failed identity
// binding requires explicit operator action or the peer-sync control path.
func TestPeerAdmissionRetryDoesNotReauthorizeWrongValidator(t *testing.T) {
	remoteSigner := newConsensusSigner(t)
	pinnedSigner := newConsensusSigner(t)
	remoteServer := newTestServer(t, Config{
		DataDir:             t.TempDir(),
		NodeID:              "unapproved-peer",
		ValidatorPrivateKey: encodedPrivateKey(t, remoteSigner.privateKey),
		BlockInterval:       0,
		SyncInterval:        0,
		EnablePeerSync:      false,
	})
	var statusCalls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			statusCalls.Add(1)
		}
		remoteServer.Handler().ServeHTTP(w, r)
	}))
	defer remote.Close()
	caller := newTestServer(t, Config{
		DataDir:               t.TempDir(),
		PeerURLs:              []string{remote.URL},
		PeerValidatorBindings: map[string]string{remote.URL: pinnedSigner.address},
		RequirePeerIdentity:   true,
		BlockInterval:         0,
		SyncInterval:          0,
		EnablePeerSync:        false,
	})
	if peers := caller.admittedPeerURLs(); len(peers) != 0 {
		t.Fatalf("wrong validator admitted: %v", peers)
	}
	view, _ := caller.peerView(remote.URL)
	if !view.Reachable || !view.IdentityVerified || view.Admitted {
		t.Fatalf("wrong validator admission evidence missing: %+v", view)
	}
	caller.peerMu.Lock()
	stored := caller.peerViews[remote.URL]
	old := time.Now().UTC().Add(-2 * peerAdmissionRetryInterval)
	stored.LastAdmissionAttemptAt = &old
	caller.peerViews[remote.URL] = stored
	caller.peerMu.Unlock()
	if peers := caller.admittedPeerURLs(); len(peers) != 0 || statusCalls.Load() != 1 {
		t.Fatalf("reachable wrong-key peer was silently re-probed or admitted: peers=%v calls=%d", peers, statusCalls.Load())
	}
}
