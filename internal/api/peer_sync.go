package api

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

type PeerView struct {
	URL               string     `json:"url"`
	NodeID            string     `json:"nodeId,omitempty"`
	ValidatorAddress  string     `json:"validatorAddress,omitempty"`
	ExpectedValidator string     `json:"expectedValidator,omitempty"`
	Height            uint64     `json:"height"`
	LatestBlockHash   string     `json:"latestBlockHash,omitempty"`
	MempoolSize       int        `json:"mempoolSize"`
	BlockProduction   bool       `json:"blockProduction"`
	IdentityPresent   bool       `json:"identityPresent"`
	IdentityVerified  bool       `json:"identityVerified"`
	IdentityError     string     `json:"identityError,omitempty"`
	Admitted          bool       `json:"admitted"`
	AdmissionError    string     `json:"admissionError,omitempty"`
	LastSeenAt        *time.Time `json:"lastSeenAt,omitempty"`
	Reachable         bool       `json:"reachable"`
	Error             string     `json:"error,omitempty"`
}

type PeersResponse struct {
	Peers []PeerView `json:"peers"`
}

func (s *Server) startPeerSync() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.config.SyncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.syncPeers()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *Server) syncPeers() {
	for _, peerURL := range s.config.PeerURLs {
		status, err := s.fetchPeerStatus(peerURL)
		if err != nil {
			s.recordPeerView(PeerView{URL: peerURL, Reachable: false, Error: err.Error()})
			continue
		}

		now := time.Now().UTC()
		view := s.buildPeerView(peerURL, status, now)
		if view.Admitted {
			localStatus := s.ledger.Status()
			needsSync := status.Status.Height > localStatus.Height
			if status.Status.Height == localStatus.Height {
				needsSync = status.Status.MempoolSize > localStatus.MempoolSize ||
					(status.Status.Height > 0 && status.Status.LatestBlockHash != localStatus.LatestBlockHash)
			}

			if needsSync {
				if err := s.syncFromPeer(peerURL, localStatus.Height, status.Status.Height); err != nil {
					view.Error = err.Error()
					recordPeerLog(fmt.Sprintf("peer-sync %s", peerURL), err)
				}
			}
		}

		s.recordPeerView(view)
	}
}

func (s *Server) syncFromPeer(peerURL string, localHeight uint64, remoteHeight uint64) error {
	for height := localHeight + 1; height <= remoteHeight; height++ {
		block, err := s.fetchPeerBlock(peerURL, height)
		if err != nil {
			return s.restoreSnapshotFromPeer(peerURL)
		}
		if err := s.ledger.ImportBlockWithOptions(block, s.config.RequireConsensusCertificates); err != nil {
			s.recordBlockImportFailure("peer_sync", block, err, peerURL)
			return s.restoreSnapshotFromPeer(peerURL)
		}
	}

	if remoteHeight == localHeight {
		peerStatus, err := s.fetchPeerStatus(peerURL)
		if err == nil {
			localStatus := s.ledger.Status()
			if peerStatus.Status.MempoolSize > localStatus.MempoolSize ||
				(peerStatus.Status.Height > 0 && peerStatus.Status.LatestBlockHash != localStatus.LatestBlockHash) {
				return s.restoreSnapshotFromPeer(peerURL)
			}
		}
	}

	return nil
}

func (s *Server) restoreSnapshotFromPeer(peerURL string) error {
	snapshot, err := s.fetchPeerSnapshot(peerURL)
	if err != nil {
		return err
	}
	if uint64(len(snapshot.Blocks)) < s.ledger.Status().Height {
		return nil
	}
	now := time.Now().UTC()
	if err := s.ledger.RestoreFromPeerSnapshot(snapshot, now); err != nil {
		return err
	}
	s.recordSnapshotRestore(peerURL, snapshot, now)
	return nil
}

func (s *Server) broadcastTransaction(envelope tx.Envelope) {
	for _, peerURL := range s.admittedPeerURLs() {
		if err := s.transport.PostTransaction(peerURL, envelope); err != nil {
			recordPeerLog(fmt.Sprintf("broadcast-transaction %s", peerURL), err)
		}
	}
}

func (s *Server) broadcastBlock(block ledger.Block) {
	for _, peerURL := range s.admittedPeerURLs() {
		if err := s.transport.PostBlock(peerURL, block); err != nil {
			recordPeerLog(fmt.Sprintf("broadcast-block %s", peerURL), err)
		}
	}
}

func (s *Server) broadcastFaucet(request FaucetRequest) {
	for _, peerURL := range s.admittedPeerURLs() {
		if err := s.transport.PostFaucet(peerURL, request); err != nil {
			recordPeerLog(fmt.Sprintf("broadcast-faucet %s", peerURL), err)
		}
	}
}

func (s *Server) broadcastProposal(proposal consensus.Proposal) {
	for _, peerURL := range s.admittedPeerURLs() {
		if err := s.transport.PostProposal(peerURL, proposal); err != nil {
			recordPeerLog(fmt.Sprintf("broadcast-proposal %s", peerURL), err)
		}
	}
}

func (s *Server) broadcastVote(vote consensus.Vote) {
	for _, peerURL := range s.admittedPeerURLs() {
		if err := s.transport.PostVote(peerURL, vote); err != nil {
			recordPeerLog(fmt.Sprintf("broadcast-vote %s", peerURL), err)
		}
	}
}

func (s *Server) fetchPeerStatus(peerURL string) (StatusResponse, error) {
	return s.transport.FetchStatus(peerURL)
}

func (s *Server) fetchPeerBlock(peerURL string, height uint64) (ledger.Block, error) {
	return s.transport.FetchBlock(peerURL, height)
}

func (s *Server) fetchPeerSnapshot(peerURL string) (ledger.Snapshot, error) {
	return s.transport.FetchSnapshot(peerURL)
}

func (s *Server) recordPeerView(view PeerView) {
	s.peerMu.Lock()
	defer s.peerMu.Unlock()
	s.peerViews[view.URL] = view
}

func (s *Server) peerSnapshot() []PeerView {
	s.peerMu.RLock()
	defer s.peerMu.RUnlock()

	peers := make([]PeerView, 0, len(s.config.PeerURLs))
	for _, peerURL := range s.config.PeerURLs {
		if view, ok := s.peerViews[peerURL]; ok {
			peers = append(peers, view)
			continue
		}
		peers = append(peers, PeerView{URL: peerURL, ExpectedValidator: s.expectedPeerValidator(peerURL)})
	}
	return peers
}

func normalizePeerURLs(peers []string) []string {
	normalized := make([]string, 0, len(peers))
	seen := make(map[string]struct{})
	for _, peer := range peers {
		peer = strings.TrimSpace(peer)
		peer = strings.TrimRight(peer, "/")
		if peer == "" {
			continue
		}
		if _, exists := seen[peer]; exists {
			continue
		}
		seen[peer] = struct{}{}
		normalized = append(normalized, peer)
	}
	return normalized
}

func recordPeerLog(scope string, err error) {
	log.Printf("zephyr %s: %v", scope, err)
}
