package api

import (
	"sync/atomic"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
)

// consensusOutboundFlights bounds the number of outstanding network
// broadcasts created by the round automation scheduler. An unresponsive
// peer must not block the round timer or create one goroutine per tick.
// The signed original artifacts remain persisted in the consensus WAL;
// after a busy slot clears, the next tick may retry the active round.
type consensusOutboundFlights struct {
	proposal atomic.Bool
	vote     atomic.Bool
}

func (s *Server) consensusBroadcastStillCurrent(height, round uint64) bool {
	view := s.ledger.Consensus()
	return view.NextHeight == height && view.CurrentRound == round
}

func (s *Server) scheduleConsensusProposalBroadcast(proposal consensus.Proposal) bool {
	if len(s.peerURLsSnapshot()) == 0 {
		return false
	}
	select {
	case <-s.stopCh:
		return false
	default:
	}
	if !s.consensusOutbound.proposal.CompareAndSwap(false, true) {
		return false
	}
	s.runAsync(func() {
		defer s.consensusOutbound.proposal.Store(false)
		select {
		case <-s.stopCh:
			return
		default:
		}
		if s.consensusBroadcastStillCurrent(proposal.Height, proposal.Round) {
			s.broadcastProposal(proposal)
		}
	})
	return true
}

func (s *Server) scheduleConsensusVoteBroadcast(vote consensus.Vote) bool {
	if len(s.peerURLsSnapshot()) == 0 {
		return false
	}
	select {
	case <-s.stopCh:
		return false
	default:
	}
	if !s.consensusOutbound.vote.CompareAndSwap(false, true) {
		return false
	}
	s.runAsync(func() {
		defer s.consensusOutbound.vote.Store(false)
		select {
		case <-s.stopCh:
			return
		default:
		}
		if s.consensusBroadcastStillCurrent(vote.Height, vote.Round) {
			s.broadcastVote(vote)
		}
	})
	return true
}
