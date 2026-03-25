package ledger

import "time"

type ChainThroughputWindowView struct {
	Window                      string  `json:"window"`
	WindowSeconds               int64   `json:"windowSeconds"`
	BlockCount                  int     `json:"blockCount"`
	TransactionCount            int     `json:"transactionCount"`
	BlocksPerSecond             float64 `json:"blocksPerSecond"`
	TransactionsPerSecond       float64 `json:"transactionsPerSecond"`
	AverageTransactionsPerBlock float64 `json:"averageTransactionsPerBlock"`
}

type ChainThroughputMetricsView struct {
	TotalBlockCount            int                         `json:"totalBlockCount"`
	TotalTransactionCount      int                         `json:"totalTransactionCount"`
	LatestBlockAt              *time.Time                  `json:"latestBlockAt,omitempty"`
	LatestBlockIntervalSeconds float64                     `json:"latestBlockIntervalSeconds,omitempty"`
	Windows                    []ChainThroughputWindowView `json:"windows"`
}

type throughputWindowDefinition struct {
	Label    string
	Duration time.Duration
}

var chainThroughputWindows = []throughputWindowDefinition{
	{Label: "1m", Duration: time.Minute},
	{Label: "5m", Duration: 5 * time.Minute},
	{Label: "15m", Duration: 15 * time.Minute},
}

func (s *Store) ChainThroughputMetrics(now time.Time) ChainThroughputMetricsView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return chainThroughputMetricsFromState(s.snapshotLocked(), now)
}

func chainThroughputMetricsFromState(state persistedState, now time.Time) ChainThroughputMetricsView {
	state = normalizeState(state)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	view := ChainThroughputMetricsView{
		Windows: make([]ChainThroughputWindowView, 0, len(chainThroughputWindows)),
	}
	for _, definition := range chainThroughputWindows {
		view.Windows = append(view.Windows, ChainThroughputWindowView{
			Window:        definition.Label,
			WindowSeconds: int64(definition.Duration.Seconds()),
		})
	}
	if len(state.Blocks) == 0 {
		return view
	}

	for _, block := range state.Blocks {
		block = cloneBlock(block)
		view.TotalBlockCount++
		view.TotalTransactionCount += blockMetricTransactionCount(block)
	}

	latest := cloneBlock(state.Blocks[len(state.Blocks)-1])
	if !latest.ProducedAt.IsZero() {
		view.LatestBlockAt = cloneNonZeroTimePointer(latest.ProducedAt)
	}
	if len(state.Blocks) >= 2 {
		previous := cloneBlock(state.Blocks[len(state.Blocks)-2])
		if !latest.ProducedAt.IsZero() && !previous.ProducedAt.IsZero() && latest.ProducedAt.After(previous.ProducedAt) {
			view.LatestBlockIntervalSeconds = latest.ProducedAt.Sub(previous.ProducedAt).Seconds()
		}
	}

	for index, definition := range chainThroughputWindows {
		windowStart := now.Add(-definition.Duration)
		window := view.Windows[index]
		for _, block := range state.Blocks {
			block = cloneBlock(block)
			if block.ProducedAt.IsZero() || block.ProducedAt.Before(windowStart) || block.ProducedAt.After(now) {
				continue
			}
			window.BlockCount++
			window.TransactionCount += blockMetricTransactionCount(block)
		}
		if definition.Duration > 0 {
			window.BlocksPerSecond = float64(window.BlockCount) / definition.Duration.Seconds()
			window.TransactionsPerSecond = float64(window.TransactionCount) / definition.Duration.Seconds()
		}
		if window.BlockCount > 0 {
			window.AverageTransactionsPerBlock = float64(window.TransactionCount) / float64(window.BlockCount)
		}
		view.Windows[index] = window
	}

	return view
}

func blockMetricTransactionCount(block Block) int {
	switch {
	case block.TransactionCount > 0:
		return block.TransactionCount
	case len(block.Transactions) > 0:
		return len(block.Transactions)
	case len(block.TransactionIDs) > 0:
		return len(block.TransactionIDs)
	default:
		return 0
	}
}
