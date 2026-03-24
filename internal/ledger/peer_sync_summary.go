package ledger

import (
	"sort"
	"strings"
	"time"
)

type PeerSyncStateSummary struct {
	State             string     `json:"state"`
	IncidentCount     int        `json:"incidentCount"`
	AffectedPeerCount int        `json:"affectedPeerCount"`
	TotalOccurrences  int        `json:"totalOccurrences"`
	LatestObservedAt  *time.Time `json:"latestObservedAt,omitempty"`
}

type PeerSyncPeerSummary struct {
	PeerURL          string     `json:"peerUrl"`
	IncidentCount    int        `json:"incidentCount"`
	TotalOccurrences int        `json:"totalOccurrences"`
	LatestState      string     `json:"latestState,omitempty"`
	LatestReason     string     `json:"latestReason,omitempty"`
	LatestErrorCode  string     `json:"latestErrorCode,omitempty"`
	LatestBlockHash  string     `json:"latestBlockHash,omitempty"`
	LatestObservedAt *time.Time `json:"latestObservedAt,omitempty"`
}

type PeerSyncSummaryView struct {
	IncidentCount     int                    `json:"incidentCount"`
	AffectedPeerCount int                    `json:"affectedPeerCount"`
	TotalOccurrences  int                    `json:"totalOccurrences"`
	LatestObservedAt  *time.Time             `json:"latestObservedAt,omitempty"`
	States            []PeerSyncStateSummary `json:"states"`
	Peers             []PeerSyncPeerSummary  `json:"peers"`
}

func (s *Store) PeerSyncSummary() PeerSyncSummaryView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return peerSyncSummaryFromState(s.snapshotLocked())
}

func (s *Store) PeerSyncPeerSummary(peerURL string) PeerSyncPeerSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return peerSyncPeerSummaryFromState(s.snapshotLocked(), peerURL)
}

func peerSyncSummaryFromState(state persistedState) PeerSyncSummaryView {
	state = normalizeState(state)
	view := PeerSyncSummaryView{
		States: make([]PeerSyncStateSummary, 0),
		Peers:  make([]PeerSyncPeerSummary, 0),
	}
	if len(state.PeerSyncIncidents) == 0 {
		return view
	}

	type stateAggregate struct {
		summary PeerSyncStateSummary
		peers   map[string]struct{}
	}

	stateSummaries := make(map[string]*stateAggregate)
	peerSummaries := make(map[string]*PeerSyncPeerSummary)

	for _, incident := range state.PeerSyncIncidents {
		incident = normalizePeerSyncIncident(incident)
		view.IncidentCount++
		view.TotalOccurrences += incident.Occurrences
		updateLatestObservedAt(&view.LatestObservedAt, incident.LastObservedAt)

		stateKey := strings.TrimSpace(incident.State)
		if stateKey == "" {
			stateKey = "unknown"
		}
		aggregate, ok := stateSummaries[stateKey]
		if !ok {
			aggregate = &stateAggregate{
				summary: PeerSyncStateSummary{State: stateKey},
				peers:   make(map[string]struct{}),
			}
			stateSummaries[stateKey] = aggregate
		}
		aggregate.summary.IncidentCount++
		aggregate.summary.TotalOccurrences += incident.Occurrences
		updateLatestObservedAt(&aggregate.summary.LatestObservedAt, incident.LastObservedAt)
		if incident.PeerURL != "" {
			aggregate.peers[incident.PeerURL] = struct{}{}
		}

		peerSummary, ok := peerSummaries[incident.PeerURL]
		if !ok {
			peerSummary = &PeerSyncPeerSummary{PeerURL: incident.PeerURL}
			peerSummaries[incident.PeerURL] = peerSummary
		}
		peerSummary.IncidentCount++
		peerSummary.TotalOccurrences += incident.Occurrences
		if peerSummary.LatestObservedAt == nil || incident.LastObservedAt.After(*peerSummary.LatestObservedAt) || incident.LastObservedAt.Equal(*peerSummary.LatestObservedAt) {
			peerSummary.LatestObservedAt = cloneNonZeroTimePointer(incident.LastObservedAt)
			peerSummary.LatestState = incident.State
			peerSummary.LatestReason = incident.Reason
			peerSummary.LatestErrorCode = incident.ErrorCode
			peerSummary.LatestBlockHash = incident.BlockHash
		}
	}

	view.AffectedPeerCount = len(peerSummaries)
	for _, aggregate := range stateSummaries {
		aggregate.summary.AffectedPeerCount = len(aggregate.peers)
		view.States = append(view.States, aggregate.summary)
	}
	for _, peerSummary := range peerSummaries {
		view.Peers = append(view.Peers, *peerSummary)
	}

	sort.Slice(view.States, func(i, j int) bool {
		if view.States[i].TotalOccurrences == view.States[j].TotalOccurrences {
			return view.States[i].State < view.States[j].State
		}
		return view.States[i].TotalOccurrences > view.States[j].TotalOccurrences
	})
	sort.Slice(view.Peers, func(i, j int) bool {
		left := view.Peers[i].LatestObservedAt
		right := view.Peers[j].LatestObservedAt
		switch {
		case left == nil && right == nil:
			return view.Peers[i].PeerURL < view.Peers[j].PeerURL
		case left == nil:
			return false
		case right == nil:
			return true
		case left.Equal(*right):
			return view.Peers[i].PeerURL < view.Peers[j].PeerURL
		default:
			return left.After(*right)
		}
	})
	return view
}

func peerSyncPeerSummaryFromState(state persistedState, peerURL string) PeerSyncPeerSummary {
	state = normalizeState(state)
	peerURL = strings.TrimSpace(peerURL)
	if peerURL == "" {
		return PeerSyncPeerSummary{}
	}
	for _, summary := range peerSyncSummaryFromState(state).Peers {
		if summary.PeerURL == peerURL {
			return summary
		}
	}
	return PeerSyncPeerSummary{PeerURL: peerURL}
}

func updateLatestObservedAt(target **time.Time, candidate time.Time) {
	if candidate.IsZero() {
		return
	}
	candidate = candidate.UTC()
	if *target == nil || candidate.After(**target) {
		*target = cloneNonZeroTimePointer(candidate)
	}
}
