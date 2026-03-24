package ledger

import (
	"sort"
	"strings"
	"time"
)

type MetricCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type ConsensusActionMetricsView struct {
	TotalCount          int           `json:"totalCount"`
	PendingCount        int           `json:"pendingCount"`
	TotalReplayAttempts int           `json:"totalReplayAttempts"`
	LatestRecordedAt    *time.Time    `json:"latestRecordedAt,omitempty"`
	LatestCompletedAt   *time.Time    `json:"latestCompletedAt,omitempty"`
	ByType              []MetricCount `json:"byType"`
	ByStatus            []MetricCount `json:"byStatus"`
}

type ConsensusDiagnosticMetricsView struct {
	TotalCount       int           `json:"totalCount"`
	LatestObservedAt *time.Time    `json:"latestObservedAt,omitempty"`
	ByKind           []MetricCount `json:"byKind"`
	ByCode           []MetricCount `json:"byCode"`
	BySource         []MetricCount `json:"bySource"`
}

func (s *Store) ConsensusActionMetrics() ConsensusActionMetricsView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return consensusActionMetricsFromState(s.snapshotLocked())
}

func (s *Store) ConsensusDiagnosticMetrics() ConsensusDiagnosticMetricsView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return consensusDiagnosticMetricsFromState(s.snapshotLocked())
}

func consensusActionMetricsFromState(state persistedState) ConsensusActionMetricsView {
	state = normalizeState(state)
	view := ConsensusActionMetricsView{
		ByType:   make([]MetricCount, 0),
		ByStatus: make([]MetricCount, 0),
	}
	if len(state.ConsensusActions) == 0 {
		return view
	}

	byType := make(map[string]int)
	byStatus := make(map[string]int)
	for _, action := range state.ConsensusActions {
		action = cloneConsensusAction(action)
		view.TotalCount++
		if action.Status == ConsensusActionPending {
			view.PendingCount++
		}
		view.TotalReplayAttempts += action.ReplayAttempts
		byType[metricLabel(action.Type, "unknown")]++
		byStatus[metricLabel(action.Status, "unknown")]++
		updateMetricsTime(&view.LatestRecordedAt, action.RecordedAt)
		if action.CompletedAt != nil {
			updateMetricsTime(&view.LatestCompletedAt, *action.CompletedAt)
		}
	}
	view.ByType = metricBucketsFromMap(byType)
	view.ByStatus = metricBucketsFromMap(byStatus)
	return view
}

func consensusDiagnosticMetricsFromState(state persistedState) ConsensusDiagnosticMetricsView {
	state = normalizeState(state)
	view := ConsensusDiagnosticMetricsView{
		ByKind:   make([]MetricCount, 0),
		ByCode:   make([]MetricCount, 0),
		BySource: make([]MetricCount, 0),
	}
	if len(state.ConsensusDiagnostics) == 0 {
		return view
	}

	byKind := make(map[string]int)
	byCode := make(map[string]int)
	bySource := make(map[string]int)
	for _, diagnostic := range state.ConsensusDiagnostics {
		diagnostic = cloneConsensusDiagnostic(diagnostic)
		view.TotalCount++
		byKind[metricLabel(diagnostic.Kind, "unknown")]++
		byCode[metricLabel(diagnostic.Code, "unknown")]++
		bySource[metricLabel(diagnostic.Source, "unknown")]++
		updateMetricsTime(&view.LatestObservedAt, diagnostic.ObservedAt)
	}
	view.ByKind = metricBucketsFromMap(byKind)
	view.ByCode = metricBucketsFromMap(byCode)
	view.BySource = metricBucketsFromMap(bySource)
	return view
}

func metricBucketsFromMap(counts map[string]int) []MetricCount {
	if len(counts) == 0 {
		return make([]MetricCount, 0)
	}
	buckets := make([]MetricCount, 0, len(counts))
	for label, count := range counts {
		buckets = append(buckets, MetricCount{Label: label, Count: count})
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Count == buckets[j].Count {
			return buckets[i].Label < buckets[j].Label
		}
		return buckets[i].Count > buckets[j].Count
	})
	return buckets
}

func metricLabel(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func updateMetricsTime(target **time.Time, candidate time.Time) {
	if candidate.IsZero() {
		return
	}
	candidate = candidate.UTC()
	if *target == nil || candidate.After(**target) {
		*target = cloneNonZeroTimePointer(candidate)
	}
}
