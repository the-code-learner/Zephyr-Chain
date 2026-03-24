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
	URL                          string                    `json:"url"`
	NodeID                       string                    `json:"nodeId,omitempty"`
	ValidatorAddress             string                    `json:"validatorAddress,omitempty"`
	ExpectedValidator            string                    `json:"expectedValidator,omitempty"`
	Height                       uint64                    `json:"height"`
	LatestBlockHash              string                    `json:"latestBlockHash,omitempty"`
	MempoolSize                  int                       `json:"mempoolSize"`
	BlockProduction              bool                      `json:"blockProduction"`
	IdentityPresent              bool                      `json:"identityPresent"`
	IdentityVerified             bool                      `json:"identityVerified"`
	IdentityError                string                    `json:"identityError,omitempty"`
	Admitted                     bool                      `json:"admitted"`
	AdmissionError               string                    `json:"admissionError,omitempty"`
	HeightDelta                  int64                     `json:"heightDelta"`
	SyncState                    string                    `json:"syncState,omitempty"`
	LastSyncAttemptAt            *time.Time                `json:"lastSyncAttemptAt,omitempty"`
	LastSyncSuccessAt            *time.Time                `json:"lastSyncSuccessAt,omitempty"`
	LastImportErrorCode          string                    `json:"lastImportErrorCode,omitempty"`
	LastImportErrorMessage       string                    `json:"lastImportErrorMessage,omitempty"`
	LastImportFailureAt          *time.Time                `json:"lastImportFailureAt,omitempty"`
	LastImportFailureHeight      uint64                    `json:"lastImportFailureHeight,omitempty"`
	LastImportFailureBlockHash   string                    `json:"lastImportFailureBlockHash,omitempty"`
	LastSnapshotRestoreAt        *time.Time                `json:"lastSnapshotRestoreAt,omitempty"`
	LastSnapshotRestoreHeight    uint64                    `json:"lastSnapshotRestoreHeight,omitempty"`
	LastSnapshotRestoreBlockHash string                    `json:"lastSnapshotRestoreBlockHash,omitempty"`
	LastSnapshotRestoreReason    string                    `json:"lastSnapshotRestoreReason,omitempty"`
	RecentIncidents              []ledger.PeerSyncIncident `json:"recentIncidents,omitempty"`
	LastSeenAt                   *time.Time                `json:"lastSeenAt,omitempty"`
	Reachable                    bool                      `json:"reachable"`
	Error                        string                    `json:"error,omitempty"`
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
		previous, _ := s.peerView(peerURL)
		status, err := s.fetchPeerStatus(peerURL)
		if err != nil {
			view := mergePeerSyncHistory(PeerView{
				URL:               peerURL,
				ExpectedValidator: s.expectedPeerValidator(peerURL),
				Reachable:         false,
				SyncState:         "unreachable",
				Error:             err.Error(),
			}, previous)
			s.recordPeerView(view)
			continue
		}

		now := time.Now().UTC()
		view := mergePeerSyncHistory(s.buildPeerView(peerURL, status, now), previous)
		if view.Admitted {
			localStatus := s.ledger.Status()
			syncState, heightDelta, needsSync := derivePeerSyncState(localStatus, status.Status)
			view.SyncState = syncState
			view.HeightDelta = heightDelta
			if needsSync {
				view.LastSyncAttemptAt = cloneAPITimeValue(now)
				result, syncErr := s.syncFromPeer(peerURL, localStatus.Height, status.Status.Height)
				view = applyPeerSyncResult(view, result)
				if syncErr != nil {
					view.Error = syncErr.Error()
					if view.SyncState == "peer_ahead" || view.SyncState == "diverged" || view.SyncState == "" {
						view.SyncState = "sync_error"
					}
					recordPeerLog(fmt.Sprintf("peer-sync %s", peerURL), syncErr)
				} else {
					completedAt := time.Now().UTC()
					view.LastSyncSuccessAt = cloneAPITimeValue(completedAt)
					if result.UsedSnapshot {
						view.SyncState = "snapshot_restored"
					} else {
						view.SyncState = "aligned"
					}
					localAfter := s.ledger.Status()
					view.HeightDelta = int64(status.Status.Height) - int64(localAfter.Height)
					if view.HeightDelta < 0 {
						view.SyncState = "peer_behind"
					} else if view.HeightDelta > 0 {
						view.SyncState = "peer_ahead"
					}
				}
			}
		} else if view.Reachable {
			view.SyncState = "unadmitted"
		}

		s.recordPeerView(view)
	}
}

func (s *Server) syncFromPeer(peerURL string, localHeight uint64, remoteHeight uint64) (peerSyncResult, error) {
	result := peerSyncResult{Attempted: true}
	for height := localHeight + 1; height <= remoteHeight; height++ {
		block, err := s.fetchPeerBlock(peerURL, height)
		if err != nil {
			restore, restoreErr := s.restoreSnapshotFromPeer(peerURL, "fetch_fallback")
			if restore.Applied {
				result.UsedSnapshot = true
				result.SnapshotRestoreAt = cloneAPITimeValue(restore.RestoredAt)
				result.SnapshotRestoreHeight = restore.Height
				result.SnapshotRestoreBlockHash = restore.BlockHash
				result.SnapshotRestoreReason = restore.Reason
			}
			if restoreErr != nil {
				return result, restoreErr
			}
			if !restore.Applied {
				return result, fmt.Errorf("peer snapshot from %s is older than local state", peerURL)
			}
			return result, nil
		}
		if err := s.ledger.ImportBlockWithOptions(block, s.config.RequireConsensusCertificates); err != nil {
			now := time.Now().UTC()
			result.ImportErrorCode = consensusDiagnosticCode(err)
			result.ImportErrorMessage = err.Error()
			result.ImportFailureAt = cloneAPITimeValue(now)
			result.ImportFailureHeight = block.Height
			result.ImportFailureBlockHash = block.Hash
			s.recordBlockImportFailure("peer_sync", block, err, peerURL)
			restore, restoreErr := s.restoreSnapshotFromPeer(peerURL, "import_repair")
			if restore.Applied {
				result.UsedSnapshot = true
				result.SnapshotRestoreAt = cloneAPITimeValue(restore.RestoredAt)
				result.SnapshotRestoreHeight = restore.Height
				result.SnapshotRestoreBlockHash = restore.BlockHash
				result.SnapshotRestoreReason = restore.Reason
			}
			if restoreErr != nil {
				return result, restoreErr
			}
			if !restore.Applied {
				return result, fmt.Errorf("peer snapshot from %s is older than local state", peerURL)
			}
			return result, nil
		}
	}

	if remoteHeight == localHeight {
		peerStatus, err := s.fetchPeerStatus(peerURL)
		if err == nil {
			localStatus := s.ledger.Status()
			if peerStatus.Status.MempoolSize > localStatus.MempoolSize ||
				(peerStatus.Status.Height > 0 && peerStatus.Status.LatestBlockHash != localStatus.LatestBlockHash) {
				restore, restoreErr := s.restoreSnapshotFromPeer(peerURL, "peer_diverged")
				if restore.Applied {
					result.UsedSnapshot = true
					result.SnapshotRestoreAt = cloneAPITimeValue(restore.RestoredAt)
					result.SnapshotRestoreHeight = restore.Height
					result.SnapshotRestoreBlockHash = restore.BlockHash
					result.SnapshotRestoreReason = restore.Reason
				}
				if restoreErr != nil {
					return result, restoreErr
				}
				if !restore.Applied {
					return result, fmt.Errorf("peer snapshot from %s is older than local state", peerURL)
				}
				return result, nil
			}
		}
	}

	return result, nil
}

func (s *Server) restoreSnapshotFromPeer(peerURL string, reason string) (peerSnapshotRestoreResult, error) {
	snapshot, err := s.fetchPeerSnapshot(peerURL)
	if err != nil {
		return peerSnapshotRestoreResult{}, err
	}
	if uint64(len(snapshot.Blocks)) < s.ledger.Status().Height {
		return peerSnapshotRestoreResult{Reason: reason}, nil
	}
	now := time.Now().UTC()
	if err := s.ledger.RestoreFromPeerSnapshot(snapshot, now); err != nil {
		return peerSnapshotRestoreResult{}, err
	}
	s.recordSnapshotRestore(peerURL, snapshot, now)
	result := peerSnapshotRestoreResult{
		Applied:    true,
		RestoredAt: now,
		Height:     uint64(len(snapshot.Blocks)),
		Reason:     reason,
	}
	if len(snapshot.Blocks) > 0 {
		result.BlockHash = snapshot.Blocks[len(snapshot.Blocks)-1].Hash
	}
	return result, nil
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
	s.peerViews[view.URL] = view
	s.peerMu.Unlock()

	s.recordPeerIncident(view)
}

func (s *Server) peerSnapshot() []PeerView {
	s.peerMu.RLock()
	defer s.peerMu.RUnlock()

	peers := make([]PeerView, 0, len(s.config.PeerURLs))
	for _, peerURL := range s.config.PeerURLs {
		if view, ok := s.peerViews[peerURL]; ok {
			peers = append(peers, s.enrichPeerView(view))
			continue
		}
		peers = append(peers, s.enrichPeerView(PeerView{URL: peerURL, ExpectedValidator: s.expectedPeerValidator(peerURL)}))
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
