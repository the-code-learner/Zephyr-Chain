package api

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

// Only one durable slow-delivery incident per peer+message type in this
// interval. Without a throttle, diagnostics themselves could amplify WAL
// pressure during partial network outages and worsen consensus liveness.
const consensusSlowDeliveryIncidentInterval = 30 * time.Second
const consensusSlowDeliveryMaxPeers = 256

type consensusLatencyRecorder struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (r *consensusLatencyRecorder) allow(peerURL, kind string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		r.last = make(map[string]time.Time)
	}
	key := kind + "|" + peerURL
	if previous, exists := r.last[key]; exists &&
		now.Before(previous.Add(consensusSlowDeliveryIncidentInterval)) {
		return false
	}
	// Keep this purely observational cache bounded even if an operator
	// rotates configured peers rapidly. Existing entries are never a
	// permission or a validator identity trust source.
	if len(r.last) >= consensusSlowDeliveryMaxPeers {
		for key, observed := range r.last {
			if !now.Before(observed.Add(consensusSlowDeliveryIncidentInterval)) {
				delete(r.last, key)
			}
		}
		if len(r.last) >= consensusSlowDeliveryMaxPeers {
			return false
		}
	}
	r.last[key] = now
	return true
}

func (s *Server) consensusSlowDeliveryThreshold() time.Duration {
	threshold := s.config.ConsensusRoundTimeout / 2
	if threshold <= 0 {
		threshold = time.Second
	}
	if threshold < 100*time.Millisecond {
		threshold = 100 * time.Millisecond
	}
	return threshold
}

// A slow but accepted signed proposal/vote POST was previously invisible
// in failure-only replication diagnostics. Record a bounded, non-error
// observation with precise duration while keeping the synchronous delivery
// order, QC rules and peer admission unchanged. No new retry or timeout.
func (s *Server) recordSlowConsensusDelivery(peerURL, kind, blockHash string, elapsed time.Duration) {
	if s == nil || (kind != "proposal" && kind != "vote") ||
		strings.TrimSpace(peerURL) == "" {
		return
	}
	threshold := s.consensusSlowDeliveryThreshold()
	if elapsed < threshold {
		return
	}
	now := time.Now().UTC()
	if !s.consensusLatency.allow(peerURL, kind, now) {
		return
	}
	s.recordExplicitPeerIncident(ledger.PeerSyncIncident{
		PeerURL:         peerURL,
		State:           "replication_slow",
		Reason:          kind,
		LocalHeight:     s.ledger.Status().Height,
		BlockHash:       strings.TrimSpace(blockHash),
		ErrorCode:       "slow_accepted_delivery",
		ErrorMessage:    fmt.Sprintf("accepted signed %s POST took %s (threshold %s)", kind, elapsed.Round(time.Millisecond), threshold),
		FirstObservedAt: now,
		LastObservedAt:  now,
	})
}
