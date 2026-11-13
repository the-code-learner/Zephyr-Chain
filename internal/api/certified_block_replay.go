package api

import (
	"fmt"
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
			err := s.deliverCertifiedReplay(peerURL, delivery, transport)
			s.recordCertifiedDelivery(peerURL, block.Hash, err)
			if err != nil {
				s.recordReplicationIncident(peerURL, "certified_replay", block.Hash, err)
				recordPeerLog("certified-block-replay "+peerURL, err)
			}
		})
	}
}

// maxCertifiedCatchupBlocks bounds the signed data a single failed latest
// block delivery may cause the proposer to retransmit. Large gaps require
// multiple retry intervals; there is no unbounded block history loop.
const maxCertifiedCatchupBlocks uint64 = 8

// deliverCertifiedReplay repairs sequential missing heights only when a
// certified latest-block POST was explicitly refused with HTTP 409.
// A newly fetched peer status is checked through the normal authenticated
// admission path and used only to choose *which locally signed blocks* to
// resend; the recipient still independently validates each QC and state root.
func (s *Server) deliverCertifiedReplay(peerURL string, latest certifiedBlockDelivery, transport certifiedBlockDeliveryTransport) error {
	err := transport.PostCertifiedBlock(peerURL, latest)
	if err == nil || err.Error() != "peer returned status 409" {
		return err
	}
	view, admitted := s.fetchPeerAdmissionView(peerURL)
	if !admitted || !view.Reachable || view.Height >= latest.Block.Height {
		return err
	}
	// A peer status is useful for choosing a replay range, but its reported
	// height/hash are NOT authenticated finality evidence. Never stream a
	// locally certified history onto a conflicting peer tip or trust a
	// fabricated nonzero height to skip a missing predecessor.
	if view.Height == 0 {
		if view.LatestBlockHash != "" {
			return fmt.Errorf("certified catchup peer claims nonempty genesis tip")
		}
	} else {
		ancestor, ok := s.ledger.BlockAtHeight(view.Height)
		if !ok || view.LatestBlockHash == "" || ancestor.Hash != view.LatestBlockHash {
			return fmt.Errorf("certified catchup peer tip does not match local finalized ancestry at height %d", view.Height)
		}
	}
	start := view.Height + 1
	end := latest.Block.Height
	if end-start >= maxCertifiedCatchupBlocks {
		end = start + maxCertifiedCatchupBlocks - 1
	}
	for height := start; height <= end; height++ {
		block, ok := s.ledger.BlockAtHeight(height)
		if !ok || block.Height != height {
			return fmt.Errorf("certified catchup missing locally committed block at height %d", height)
		}
		evidence, ok := s.ledger.CertifiedBlockEvidenceAt(height)
		if !ok {
			return fmt.Errorf("certified catchup missing signed quorum at height %d", height)
		}
		if sendErr := transport.PostCertifiedBlock(peerURL, certifiedBlockDelivery{Block: block, Evidence: evidence}); sendErr != nil {
			return sendErr
		}
	}
	if end < latest.Block.Height {
		return fmt.Errorf("certified catchup bounded at height %d; next retry required", end)
	}
	return nil
}
