package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

type PeerView struct {
	URL             string     `json:"url"`
	NodeID          string     `json:"nodeId,omitempty"`
	Height          uint64     `json:"height"`
	LatestBlockHash string     `json:"latestBlockHash,omitempty"`
	MempoolSize     int        `json:"mempoolSize"`
	BlockProduction bool       `json:"blockProduction"`
	LastSeenAt      *time.Time `json:"lastSeenAt,omitempty"`
	Reachable       bool       `json:"reachable"`
	Error           string     `json:"error,omitempty"`
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
		view := PeerView{
			URL:             peerURL,
			NodeID:          status.NodeID,
			Height:          status.Status.Height,
			LatestBlockHash: status.Status.LatestBlockHash,
			MempoolSize:     status.Status.MempoolSize,
			BlockProduction: status.BlockProduction,
			LastSeenAt:      &now,
			Reachable:       true,
		}
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

		s.recordPeerView(view)
	}
}

func (s *Server) syncFromPeer(peerURL string, localHeight uint64, remoteHeight uint64) error {
	for height := localHeight + 1; height <= remoteHeight; height++ {
		block, err := s.fetchPeerBlock(peerURL, height)
		if err != nil {
			return s.restoreSnapshotFromPeer(peerURL)
		}
		if err := s.ledger.ImportBlock(block); err != nil {
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
	return s.ledger.Restore(snapshot)
}

func (s *Server) broadcastTransaction(envelope tx.Envelope) {
	for _, peerURL := range s.config.PeerURLs {
		if err := s.postJSON(peerURL+"/v1/transactions", envelope); err != nil {
			recordPeerLog(fmt.Sprintf("broadcast-transaction %s", peerURL), err)
		}
	}
}

func (s *Server) broadcastBlock(block ledger.Block) {
	for _, peerURL := range s.config.PeerURLs {
		if err := s.postJSON(peerURL+"/v1/internal/blocks", block); err != nil {
			recordPeerLog(fmt.Sprintf("broadcast-block %s", peerURL), err)
		}
	}
}

func (s *Server) broadcastFaucet(request FaucetRequest) {
	for _, peerURL := range s.config.PeerURLs {
		if err := s.postJSON(peerURL+"/v1/dev/faucet", request); err != nil {
			recordPeerLog(fmt.Sprintf("broadcast-faucet %s", peerURL), err)
		}
	}
}
func (s *Server) fetchPeerStatus(peerURL string) (StatusResponse, error) {
	response, err := s.httpClient.Get(peerURL + "/v1/status")
	if err != nil {
		return StatusResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return StatusResponse{}, fmt.Errorf("peer returned status %d", response.StatusCode)
	}

	var payload StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return StatusResponse{}, err
	}
	return payload, nil
}

func (s *Server) fetchPeerBlock(peerURL string, height uint64) (ledger.Block, error) {
	response, err := s.httpClient.Get(fmt.Sprintf("%s/v1/blocks/%d", peerURL, height))
	if err != nil {
		return ledger.Block{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ledger.Block{}, fmt.Errorf("peer returned status %d", response.StatusCode)
	}

	var payload BlockAtHeightResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ledger.Block{}, err
	}
	return payload.Block, nil
}

func (s *Server) fetchPeerSnapshot(peerURL string) (ledger.Snapshot, error) {
	response, err := s.httpClient.Get(peerURL + "/v1/internal/snapshot")
	if err != nil {
		return ledger.Snapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ledger.Snapshot{}, fmt.Errorf("peer returned status %d", response.StatusCode)
	}

	var payload SnapshotResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ledger.Snapshot{}, err
	}
	return payload.Snapshot, nil
}

func (s *Server) postJSON(target string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(sourceNodeHeader, s.nodeID)

	response, err := s.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 && response.StatusCode != http.StatusConflict && response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusOK {
		return fmt.Errorf("peer returned status %d", response.StatusCode)
	}
	return nil
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
		peers = append(peers, PeerView{URL: peerURL})
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


