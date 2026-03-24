package api

import (
	"sort"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

type PeerRuntimeMetricsView struct {
	ConfiguredPeerCount  int                  `json:"configuredPeerCount"`
	ReachablePeerCount   int                  `json:"reachablePeerCount"`
	AdmittedPeerCount    int                  `json:"admittedPeerCount"`
	UnreachablePeerCount int                  `json:"unreachablePeerCount"`
	UnadmittedPeerCount  int                  `json:"unadmittedPeerCount"`
	BySyncState          []ledger.MetricCount `json:"bySyncState"`
}

func buildPeerRuntimeMetrics(peers []PeerView) PeerRuntimeMetricsView {
	view := PeerRuntimeMetricsView{
		ConfiguredPeerCount: len(peers),
		BySyncState:         make([]ledger.MetricCount, 0),
	}
	if len(peers) == 0 {
		return view
	}

	bySyncState := make(map[string]int)
	for _, peer := range peers {
		if peer.Reachable {
			view.ReachablePeerCount++
		} else {
			view.UnreachablePeerCount++
		}
		if peer.Admitted {
			view.AdmittedPeerCount++
		} else {
			view.UnadmittedPeerCount++
		}
		label := peer.SyncState
		if label == "" {
			label = "unknown"
		}
		bySyncState[label]++
	}
	for label, count := range bySyncState {
		view.BySyncState = append(view.BySyncState, ledger.MetricCount{Label: label, Count: count})
	}
	sort.Slice(view.BySyncState, func(i, j int) bool {
		if view.BySyncState[i].Count == view.BySyncState[j].Count {
			return view.BySyncState[i].Label < view.BySyncState[j].Label
		}
		return view.BySyncState[i].Count > view.BySyncState[j].Count
	})
	return view
}

func cloneMetricsTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}
