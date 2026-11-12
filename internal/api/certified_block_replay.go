package api

import (
	"time"
)

// A successful delivery needs no further replay for this peer and block.
// Failed requests are retried at a bounded rate; the state is deliberately
// ephemeral, so a restarted validator replays its latest durable certified
// block once more. The peer still verifies its signed quorum independently.
const certifiedReplayInterval = 2 * time.Second

type certifiedReplayState struct {
	blockHash    string
	lastAttempt time.Time
	inFlight    bool
	acknowledged bool
}

func (s *Server) recordCertifiedDelivery(peerURL, hash string, err error) {
	s.certifiedReplayMu.Lock()
	defer s.certifiedReplayMu.Unlock()
	if s.certifiedReplay == nil {
		s.certifiedReplay = make(map[string]certifiedReplayState)
	}
	current := s.certifiedReplay[peerURL]
	if current.blockHash != hash {
		current = certifiedReplayState{blockHash: hash}
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
	height := s.ledger.Status().Height
	if height == 0 {
		return
	}
	block, ok := s.ledger.BlockAtHeight(height)
	if !ok {
		return
	}
	evidence, ok := s.ledger.CertifiedBlockEvidenceAt(height)
	if !ok {
		return
	}
	delivery := certifiedBlockDelivery{Block: block, Evidence: evidence}
	for _, peerURL := range s.admittedPeerURLs() {
		if !s.reserveCertifiedReplay(peerURL, block.Hash, time.Now()) {
			continue
		}
		peerURL := peerURL
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
