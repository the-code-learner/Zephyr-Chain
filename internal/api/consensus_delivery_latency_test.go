package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
)

// The transport remains synchronous: this test only adds evidence for
// accepted-but-slow peer replies, so it cannot change BFT message ordering.
func TestSlowConsensusDeliveryDiagnosticsBoundedAndFailClosed(t *testing.T) {
	signer := newConsensusSigner(t)
	var mode atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(sourceSignatureHeader) == "" {
			http.Error(w, "unsigned transport identity", http.StatusForbidden)
			return
		}
		switch mode.Load() {
		case 1, 2, 3:
			time.Sleep(135 * time.Millisecond)
		}
		if mode.Load() == 2 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	s := newTestServer(t, Config{
		DataDir:                   t.TempDir(),
		NodeID:                    "slow-consensus-diagnostic",
		ValidatorPrivateKey:       encodedPrivateKey(t, signer.privateKey),
		PeerURLs:                  []string{peer.URL},
		BlockInterval:             0,
		SyncInterval:              0,
		ConsensusRoundTimeout:     200 * time.Millisecond,
		EnablePeerSync:            false,
		EnableConsensusAutomation: false,
	})
	t.Cleanup(func() {
		s.Close()
		peer.Close()
	})
	if threshold := s.consensusSlowDeliveryThreshold(); threshold != 100*time.Millisecond {
		t.Fatalf("unexpected slow delivery threshold: %v", threshold)
	}
	proposal := consensus.Proposal{Height: 1, Round: 0, BlockHash: "slow-proposal"}
	vote := consensus.Vote{Height: 1, Round: 0, BlockHash: "slow-vote"}
	count := func(state, reason string) int {
		t.Helper()
		found := 0
		for _, incident := range s.ledger.PeerSyncIncidents(peer.URL, 20) {
			if incident.State == state && incident.Reason == reason {
				found++
			}
		}
		return found
	}

	// Fast accepted delivery does not write a diagnostic WAL entry.
	s.broadcastProposal(proposal)
	if got := count("replication_slow", "proposal"); got != 0 {
		t.Fatalf("fast accepted POST reported slow: %d", got)
	}

	mode.Store(1)
	s.broadcastProposal(proposal)
	if got := count("replication_slow", "proposal"); got != 1 {
		t.Fatalf("slow but accepted signed proposal did not emit evidence: %d", got)
	}
	var observed bool
	for _, incident := range s.ledger.PeerSyncIncidents(peer.URL, 20) {
		if incident.State != "replication_slow" || incident.Reason != "proposal" {
			continue
		}
		if incident.ErrorCode != "slow_accepted_delivery" ||
			!strings.Contains(incident.ErrorMessage, "accepted signed proposal POST took") {
			t.Fatalf("slow accepted evidence missing message and stable code: %+v", incident)
		}
		observed = true
	}
	if !observed {
		t.Fatal("no actionable slow-delivery evidence")
	}

	// An otherwise busy peer must not cause a new full WAL fsync on
	// every tick: at most one diagnostic per peer/message type/30s.
	s.broadcastProposal(proposal)
	if got := count("replication_slow", "proposal"); got != 1 {
		t.Fatalf("slow-delivery rate limit failed: %d", got)
	}

	// A slow HTTP 409 is still an explicit REJECTED consensus message,
	// never falsely recorded as an accepted-but-slow delivery.
	mode.Store(2)
	s.broadcastVote(vote)
	if got := count("replication_slow", "vote"); got != 0 {
		t.Fatalf("rejected vote reported as accepted: %d", got)
	}
	if got := count("replication_blocked", "vote"); got != 1 {
		t.Fatalf("rejected vote lost replication incident: %d", got)
	}

	// Separate types have independent throttle keys.
	mode.Store(3)
	s.broadcastVote(vote)
	if got := count("replication_slow", "vote"); got != 1 {
		t.Fatalf("independent slow accepted vote not recorded: %d", got)
	}
	if s.consensusSlowDeliveryThreshold() != 100*time.Millisecond {
		t.Fatal("the transport diagnostic unexpectedly modified consensus timeout")
	}
}

func TestSlowConsensusDeliveryRecorderBoundedUnderPeerChurn(t *testing.T) {
	var r consensusLatencyRecorder
	now := time.Now().UTC()
	for i := 0; i < consensusSlowDeliveryMaxPeers; i++ {
		key := string(rune(0x1000 + i))
		if !r.allow(key, "vote", now) {
			t.Fatalf("entry %d unexpectedly rejected under capacity", i)
		}
		if r.allow(key, "vote", now.Add(time.Second)) {
			t.Fatal("accepted repeated observation within 30-second interval")
		}
	}
	if r.allow("new-peer", "vote", now) {
		t.Fatal("unbounded slow-delivery diagnostics cache")
	}
	if !r.allow("new-peer", "vote", now.Add(consensusSlowDeliveryIncidentInterval+time.Second)) {
		t.Fatal("expired diagnostic throttle did not release capacity")
	}
}
