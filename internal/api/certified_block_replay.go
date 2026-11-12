package api

import (
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

// A successful delivery needs no further replay for this peer and block.
// Failed requests are retried at a bounded rate; the state is deliberately
// ephemeral, so a restarted validator replays its latest durable certified
// block once more. The peer still verifies its signed quorum independently.
const certifiedReplayInterval = 2 * time.Second

type certifiedReplayState struct {
	blockHash    string
	lastAttempt  time.Time
	inFlight     bool
	acknowledged bool
}

func (s *Server) recordCertifiedDelivery(peerURL, hash string, err error) {
	s.certifiedReplayMu.Lock()
	defer s.certifiedReplayMu.Unlock()
	if s.certifiedReplay == nil {
		s.certifiedReplay = make(map[string]certifiedReplayState)
	}
	current := s.certifiedReplay[peerURL]
	// An earlier asynchronous HTTP POST can finish after a newer height was
	// reserved for this peer. Its late ACK (or failure) must never roll the
	// replay cursor back to the old block hash and suppress newer finality.
	if current.blockHash != "" && current.blockHash != hash {
		return
	}
	if current.blockHash == "" {
		current.blockHash = hash
	}
	current.lastAttempt = time.Now()
	current.inFlight = false
	current.acknowledged = err == nil
	s.certifiedReplay[peerURL] = current
}

func (s *Server) reserveCertifiedReplay(peerURL, hash string, now time.Time) bool {
	s.certifiedReplayMu.Lock()
	defer s.certifiedReplayMu.Unlock()
	if s.certifiedReplay == nil {
		s.certifiedReplay = make(map[string]certifiedReplayState)
	}
	current := s.certifiedReplay[peerURL]
	if current.blockHash != hash {
		current = certifiedReplayState{blockHash: hash}
	}
	if current.acknowledged || current.inFlight ||
		(!current.lastAttempt.IsZero() && now.Before(current.lastAttempt.Add(certifiedReplayInterval))) {
		return false
	}
	current.inFlight = true
	current.lastAttempt = now
	s.certifiedReplay[peerURL] = current
	return true
}

// maybeRebroadcastLatestCertifiedBlock is called by the consensus ticker.
// It only considers the latest durable committed block: no unbounded replay
// queue, network call or WAL fsync runs in the 20ms consensus-critical tick.
// After a restart, an already-finalized block is retransmitted at most once
// per reachable peer until a failure requires bounded retry.
func (s *Server) maybeRebroadcastLatestCertifiedBlock() {
	if !s.config.RequireConsensusCertificates {
		return
	}
	transport, ok := s.transport.(certifiedBlockDeliveryTransport)
	if !ok {
		return
	}
	status := s.ledger.Status()
	if status.Height == 0 || status.LatestBlockHash == "" {
		return
	}
	// Read height/hash atomically from the cheap status view before reserving
	// retries; avoid copying a potentially transaction-heavy committed block
	// at every short tick after peers have acknowledged this hash.
	eligible := make([]string, 0)
	now := time.Now()
	for _, peerURL := range s.admittedPeerURLs() {
		if s.reserveCertifiedReplay(peerURL, status.LatestBlockHash, now) {
			eligible = append(eligible, peerURL)
		}
	}
	if len(eligible) == 0 {
		return
	}
	block, ok := s.ledger.BlockAtHeight(status.Height)
	if !ok || block.Hash != status.LatestBlockHash {
		for _, peerURL := range eligible {
			s.recordCertifiedDelivery(peerURL, status.LatestBlockHash, ledger.ErrConsensusCertificateRequired)
		}
		return
	}
	evidence, ok := s.ledger.CertifiedBlockEvidenceAt(status.Height)
	if !ok {
		for _, peerURL := range eligible {
			s.recordCertifiedDelivery(peerURL, block.Hash, ledger.ErrConsensusCertificateRequired)
		}
		return
	}
	delivery := certifiedBlockDelivery{Block: block, Evidence: evidence}
	for _, peerURL := range eligible {
		s.runAsync(func() {
			err := transport.PostCertifiedBlock(peerURL, delivery)
			s.recordCertifiedDelivery(peerURL, block.Hash, err)
			if err != nil {
				s.recordReplicationIncident(peerURL, "certified_replay", block.Hash, err)
				recordPeerLog("certified-block-replay "+peerURL, err)
			}
		})
	}
}
