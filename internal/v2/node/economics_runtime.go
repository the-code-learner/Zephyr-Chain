package node

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
)

// EnableShadowEconomics attaches finalized economic telemetry to the v2 runtime.
// It is intentionally genesis-only for now: attaching a collector after blocks
// have finalized would require authenticated historical replay first.
func (r *Runtime) EnableShadowEconomics(collector *economics.EpochCollector) error {
	if r == nil || collector == nil {
		return ErrRuntimeConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Height != 0 || r.economicCollector != nil || collector.ShardCount() != r.ShardCount || collector.NativeTokenID() != r.NativeToken {
		return ErrRuntimeConfig
	}
	r.economicCollector = collector
	return nil
}

// EconomicEpochSnapshot returns the current shadow epoch inputs derived only
// from successfully finalized Runtime.Commit calls.
func (r *Runtime) EconomicEpochSnapshot() ([]economics.ShardEpochMetrics, []compute.VerifiedWork, error) {
	if r == nil {
		return nil, nil, ErrRuntimeConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.economicCollector == nil {
		return nil, nil, ErrRuntimeConfig
	}
	return r.economicCollector.FinalizeEpoch()
}
