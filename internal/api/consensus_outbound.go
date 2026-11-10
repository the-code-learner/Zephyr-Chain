package api

import (
	"fmt"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
)

// Bound in-flight deliveries across BOTH message types, including peers
// that never acknowledge a signed POST. A stalled destination must never
// monopolize the one global proposal/vote lane and starve healthy peers.
const maxConcurrentConsensusOutbound = 16

type consensusOutboundFlights struct {
	mu     sync.Mutex
	active map[string]struct{}
}

// Single-flight is per message kind AND destination. Independently healthy
// validators can receive proposals/votes while another endpoint is hung.
// When at capacity, the existing periodic WAL replay retries after slots
// release; no unbounded queue or detached goroutines are created.
func (f *consensusOutboundFlights) reserve(kind, peerURL string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active == nil {
		f.active = make(map[string]struct{})
	}
	key := kind + "|" + peerURL
	if _, exists := f.active[key]; exists || len(f.active) >= maxConcurrentConsensusOutbound {
		return false
	}
	f.active[key] = struct{}{}
	return true
}

func (f *consensusOutboundFlights) release(kind, peerURL string) {
	f.mu.Lock()
	delete(f.active, kind+"|"+peerURL)
	f.mu.Unlock()
}

func (f *consensusOutboundFlights) idle() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.active) == 0
}

func (s *Server) consensusBroadcastStillCurrent(height, round uint64) bool {
	view := s.ledger.Consensus()
	return view.NextHeight == height && view.CurrentRound == round
}

func (s *Server) scheduleConsensusProposalBroadcast(proposal consensus.Proposal) bool {
	queued := false
	for _, peerURL := range s.admittedPeerURLs() {
		select {
		case <-s.stopCh:
			return queued
		default:
		}
		if !s.consensusOutbound.reserve("proposal", peerURL) {
			continue
		}
		queued = true
		peerURL := peerURL
		s.runAsync(func() {
			defer s.consensusOutbound.release("proposal", peerURL)
			select {
			case <-s.stopCh:
				return
			default:
			}
			if !s.consensusBroadcastStillCurrent(proposal.Height, proposal.Round) {
				return
			}
			if err := s.transport.PostProposal(peerURL, proposal); err != nil {
				s.recordReplicationIncident(peerURL, "proposal", proposal.BlockHash, err)
				recordPeerLog(fmt.Sprintf("broadcast-proposal %s", peerURL), err)
			}
		})
	}
	return queued
}

func (s *Server) scheduleConsensusVoteBroadcast(vote consensus.Vote) bool {
	queued := false
	for _, peerURL := range s.admittedPeerURLs() {
		select {
		case <-s.stopCh:
			return queued
		default:
		}
		if !s.consensusOutbound.reserve("vote", peerURL) {
			continue
		}
		queued = true
		peerURL := peerURL
		s.runAsync(func() {
			defer s.consensusOutbound.release("vote", peerURL)
			select {
			case <-s.stopCh:
				return
			default:
			}
			if !s.consensusBroadcastStillCurrent(vote.Height, vote.Round) {
				return
			}
			if err := s.transport.PostVote(peerURL, vote); err != nil {
				s.recordReplicationIncident(peerURL, "vote", vote.BlockHash, err)
				recordPeerLog(fmt.Sprintf("broadcast-vote %s", peerURL), err)
			}
		})
	}
	return queued
}
