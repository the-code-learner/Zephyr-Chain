package ledger

import (
	"sort"
	"time"
)

type ConsensusRoundHistoryEntry struct {
	Height               uint64      `json:"height"`
	Round                uint64      `json:"round"`
	Active               bool        `json:"active"`
	StartedAt            *time.Time  `json:"startedAt,omitempty"`
	ScheduledProposer    string      `json:"scheduledProposer,omitempty"`
	ProposalPresent      bool        `json:"proposalPresent"`
	ProposalBlockHash    string      `json:"proposalBlockHash,omitempty"`
	ProposalProposer     string      `json:"proposalProposer,omitempty"`
	VoteTallies          []VoteTally `json:"voteTallies"`
	CertificatePresent   bool        `json:"certificatePresent"`
	CertificateBlockHash string      `json:"certificateBlockHash,omitempty"`
}

type ConsensusRoundHistoryView struct {
	Height uint64                       `json:"height"`
	Rounds []ConsensusRoundHistoryEntry `json:"rounds"`
}

func (s *Store) RoundHistory(height uint64) ConsensusRoundHistoryView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return roundHistoryFromState(s.snapshotLocked(), height)
}

func roundHistoryFromState(state persistedState, height uint64) ConsensusRoundHistoryView {
	state = normalizeState(state)
	if height == 0 {
		height = uint64(len(state.Blocks)) + 1
	}

	view := ConsensusRoundHistoryView{
		Height: height,
		Rounds: make([]ConsensusRoundHistoryEntry, 0),
	}
	entries := make(map[uint64]ConsensusRoundHistoryEntry)
	ensureRound := func(round uint64) ConsensusRoundHistoryEntry {
		entry, ok := entries[round]
		if ok {
			return entry
		}
		entry = ConsensusRoundHistoryEntry{
			Height:            height,
			Round:             round,
			ScheduledProposer: proposerForHeightRound(state.ValidatorSnapshot.Validators, height, round),
			VoteTallies:       make([]VoteTally, 0),
		}
		entries[round] = entry
		return entry
	}

	if height == state.RoundState.Height {
		entry := ensureRound(state.RoundState.Round)
		entry.Active = true
		entry.StartedAt = cloneNonZeroTimePointer(state.RoundState.StartedAt)
		entries[state.RoundState.Round] = entry
	}

	for _, proposal := range state.Proposals {
		if proposal.Height != height {
			continue
		}
		entry := ensureRound(proposal.Round)
		entry.ProposalPresent = true
		entry.ProposalBlockHash = proposal.BlockHash
		entry.ProposalProposer = proposal.Proposer
		entries[proposal.Round] = entry
	}
	for _, vote := range state.Votes {
		if vote.Vote.Height != height {
			continue
		}
		ensureRound(vote.Vote.Round)
	}
	for _, certificate := range state.CommitCertificates {
		if certificate.Height != height {
			continue
		}
		entry := ensureRound(certificate.Round)
		entry.CertificatePresent = true
		entry.CertificateBlockHash = certificate.BlockHash
		entries[certificate.Round] = entry
	}

	quorum := quorumVotingPower(totalVotingPower(state.ValidatorSnapshot))
	rounds := make([]uint64, 0, len(entries))
	for round := range entries {
		rounds = append(rounds, round)
	}
	sort.Slice(rounds, func(i, j int) bool {
		return rounds[i] < rounds[j]
	})
	for _, round := range rounds {
		entry := entries[round]
		entry.VoteTallies = voteTalliesForHeightRound(state.Votes, height, round, quorum)
		view.Rounds = append(view.Rounds, entry)
	}

	return view
}
