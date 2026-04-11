package ledger

import (
	"sort"
	"strings"
	"time"
)

var peerSyncSummaryHorizonDefinitions = []struct {
	Window   string
	Duration time.Duration
}{
	{Window: "5m", Duration: 5 * time.Minute},
	{Window: "15m", Duration: 15 * time.Minute},
	{Window: "1h", Duration: 1 * time.Hour},
	{Window: "6h", Duration: 6 * time.Hour},
	{Window: "24h", Duration: 24 * time.Hour},
}

type PeerSyncStateSummary struct {
	State             string     `json:"state"`
	IncidentCount     int        `json:"incidentCount"`
	AffectedPeerCount int        `json:"affectedPeerCount"`
	TotalOccurrences  int        `json:"totalOccurrences"`
	LatestObservedAt  *time.Time `json:"latestObservedAt,omitempty"`
}

type PeerSyncReasonSummary struct {
	Reason            string     `json:"reason"`
	IncidentCount     int        `json:"incidentCount"`
	AffectedPeerCount int        `json:"affectedPeerCount"`
	TotalOccurrences  int        `json:"totalOccurrences"`
	LatestObservedAt  *time.Time `json:"latestObservedAt,omitempty"`
}

type PeerSyncErrorCodeSummary struct {
	ErrorCode         string     `json:"errorCode"`
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
	IncidentCount     int                        `json:"incidentCount"`
	AffectedPeerCount int                        `json:"affectedPeerCount"`
	TotalOccurrences  int                        `json:"totalOccurrences"`
	LatestObservedAt  *time.Time                 `json:"latestObservedAt,omitempty"`
	States            []PeerSyncStateSummary     `json:"states"`
	Reasons           []PeerSyncReasonSummary    `json:"reasons"`
	ErrorCodes        []PeerSyncErrorCodeSummary `json:"errorCodes"`
	Peers             []PeerSyncPeerSummary      `json:"peers"`
	Horizons          []PeerSyncHorizonSummary   `json:"horizons"`
}

type PeerSyncHorizonSummary struct {
	Window            string                     `json:"window"`
	WindowSeconds     int64                      `json:"windowSeconds"`
	IncidentCount     int                        `json:"incidentCount"`
	AffectedPeerCount int                        `json:"affectedPeerCount"`
	TotalOccurrences  int                        `json:"totalOccurrences"`
	LatestObservedAt  *time.Time                 `json:"latestObservedAt,omitempty"`
	States            []PeerSyncStateSummary     `json:"states"`
	Reasons           []PeerSyncReasonSummary    `json:"reasons"`
	ErrorCodes        []PeerSyncErrorCodeSummary `json:"errorCodes"`
}

func (s *Store) PeerSyncSummary() PeerSyncSummaryView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return peerSyncSummaryFromStateAt(s.snapshotLocked(), time.Now().UTC())
}

func (s *Store) PeerSyncPeerSummary(peerURL string) PeerSyncPeerSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return peerSyncPeerSummaryFromState(s.snapshotLocked(), peerURL)
}

func peerSyncSummaryFromState(state persistedState) PeerSyncSummaryView {
	return peerSyncSummaryFromStateAt(state, time.Now().UTC())
}

func peerSyncSummaryFromStateAt(state persistedState, now time.Time) PeerSyncSummaryView {
	state = normalizeState(state)
	now = normalizePeerSyncSummaryTime(now)
	view := peerSyncSummaryFromIncidents(state.PeerSyncIncidents)
	view.Horizons = buildPeerSyncHorizonSummaries(state.PeerSyncIncidents, now)
	return view
}

func peerSyncSummaryFromIncidents(incidents []PeerSyncIncident) PeerSyncSummaryView {
	view := PeerSyncSummaryView{
		States:     make([]PeerSyncStateSummary, 0),
		Reasons:    make([]PeerSyncReasonSummary, 0),
		ErrorCodes: make([]PeerSyncErrorCodeSummary, 0),
		Peers:      make([]PeerSyncPeerSummary, 0),
		Horizons:   make([]PeerSyncHorizonSummary, 0),
	}
	if len(incidents) == 0 {
		return view
	}

	type stateAggregate struct {
		summary PeerSyncStateSummary
		peers   map[string]struct{}
	}

	type reasonAggregate struct {
		summary PeerSyncReasonSummary
		peers   map[string]struct{}
	}

	type errorCodeAggregate struct {
		summary PeerSyncErrorCodeSummary
		peers   map[string]struct{}
	}

	stateSummaries := make(map[string]*stateAggregate)
	reasonSummaries := make(map[string]*reasonAggregate)
	errorCodeSummaries := make(map[string]*errorCodeAggregate)
	peerSummaries := make(map[string]*PeerSyncPeerSummary)

	for _, incident := range incidents {
		incident = normalizePeerSyncIncident(incident)
		view.IncidentCount++
		view.TotalOccurrences += incident.Occurrences
		updateLatestObservedAt(&view.LatestObservedAt, incident.LastObservedAt)

		stateKey := peerSyncSummaryLabel(incident.State)
		stateSummaryAggregate, ok := stateSummaries[stateKey]
		if !ok {
			stateSummaryAggregate = &stateAggregate{
				summary: PeerSyncStateSummary{State: stateKey},
				peers:   make(map[string]struct{}),
			}
			stateSummaries[stateKey] = stateSummaryAggregate
		}
		stateSummaryAggregate.summary.IncidentCount++
		stateSummaryAggregate.summary.TotalOccurrences += incident.Occurrences
		updateLatestObservedAt(&stateSummaryAggregate.summary.LatestObservedAt, incident.LastObservedAt)
		if incident.PeerURL != "" {
			stateSummaryAggregate.peers[incident.PeerURL] = struct{}{}
		}

		reasonKey := peerSyncSummaryLabel(incident.Reason)
		reasonSummaryAggregate, ok := reasonSummaries[reasonKey]
		if !ok {
			reasonSummaryAggregate = &reasonAggregate{
				summary: PeerSyncReasonSummary{Reason: reasonKey},
				peers:   make(map[string]struct{}),
			}
			reasonSummaries[reasonKey] = reasonSummaryAggregate
		}
		reasonSummaryAggregate.summary.IncidentCount++
		reasonSummaryAggregate.summary.TotalOccurrences += incident.Occurrences
		updateLatestObservedAt(&reasonSummaryAggregate.summary.LatestObservedAt, incident.LastObservedAt)
		if incident.PeerURL != "" {
			reasonSummaryAggregate.peers[incident.PeerURL] = struct{}{}
		}

		errorCodeKey := peerSyncSummaryLabel(incident.ErrorCode)
		errorCodeSummaryAggregate, ok := errorCodeSummaries[errorCodeKey]
		if !ok {
			errorCodeSummaryAggregate = &errorCodeAggregate{
				summary: PeerSyncErrorCodeSummary{ErrorCode: errorCodeKey},
				peers:   make(map[string]struct{}),
			}
			errorCodeSummaries[errorCodeKey] = errorCodeSummaryAggregate
		}
		errorCodeSummaryAggregate.summary.IncidentCount++
		errorCodeSummaryAggregate.summary.TotalOccurrences += incident.Occurrences
		updateLatestObservedAt(&errorCodeSummaryAggregate.summary.LatestObservedAt, incident.LastObservedAt)
		if incident.PeerURL != "" {
			errorCodeSummaryAggregate.peers[incident.PeerURL] = struct{}{}
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
	for _, aggregate := range reasonSummaries {
		aggregate.summary.AffectedPeerCount = len(aggregate.peers)
		view.Reasons = append(view.Reasons, aggregate.summary)
	}
	for _, aggregate := range errorCodeSummaries {
		aggregate.summary.AffectedPeerCount = len(aggregate.peers)
		view.ErrorCodes = append(view.ErrorCodes, aggregate.summary)
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
	sort.Slice(view.Reasons, func(i, j int) bool {
		if view.Reasons[i].TotalOccurrences == view.Reasons[j].TotalOccurrences {
			return view.Reasons[i].Reason < view.Reasons[j].Reason
		}
		return view.Reasons[i].TotalOccurrences > view.Reasons[j].TotalOccurrences
	})
	sort.Slice(view.ErrorCodes, func(i, j int) bool {
		if view.ErrorCodes[i].TotalOccurrences == view.ErrorCodes[j].TotalOccurrences {
			return view.ErrorCodes[i].ErrorCode < view.ErrorCodes[j].ErrorCode
		}
		return view.ErrorCodes[i].TotalOccurrences > view.ErrorCodes[j].TotalOccurrences
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

func buildPeerSyncHorizonSummaries(incidents []PeerSyncIncident, now time.Time) []PeerSyncHorizonSummary {
	horizons := make([]PeerSyncHorizonSummary, 0, len(peerSyncSummaryHorizonDefinitions))
	for _, definition := range peerSyncSummaryHorizonDefinitions {
		cutoff := now.Add(-definition.Duration)
		filtered := filterPeerSyncIncidentsByLatestObservedAt(incidents, cutoff)
		summary := peerSyncSummaryFromIncidents(filtered)
		horizons = append(horizons, PeerSyncHorizonSummary{
			Window:            definition.Window,
			WindowSeconds:     int64(definition.Duration.Seconds()),
			IncidentCount:     summary.IncidentCount,
			AffectedPeerCount: summary.AffectedPeerCount,
			TotalOccurrences:  summary.TotalOccurrences,
			LatestObservedAt:  cloneNonZeroTimePointerOrNil(summary.LatestObservedAt),
			States:            append([]PeerSyncStateSummary(nil), summary.States...),
			Reasons:           append([]PeerSyncReasonSummary(nil), summary.Reasons...),
			ErrorCodes:        append([]PeerSyncErrorCodeSummary(nil), summary.ErrorCodes...),
		})
	}
	return horizons
}

func filterPeerSyncIncidentsByLatestObservedAt(incidents []PeerSyncIncident, cutoff time.Time) []PeerSyncIncident {
	if len(incidents) == 0 {
		return nil
	}
	filtered := make([]PeerSyncIncident, 0, len(incidents))
	for _, incident := range incidents {
		incident = normalizePeerSyncIncident(incident)
		if incident.LastObservedAt.Before(cutoff) {
			continue
		}
		filtered = append(filtered, incident)
	}
	return filtered
}

func normalizePeerSyncSummaryTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func cloneNonZeroTimePointerOrNil(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return cloneNonZeroTimePointer(*value)
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

func peerSyncSummaryLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
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
