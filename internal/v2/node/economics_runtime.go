package node

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
)

// EnableShadowEconomics attaches finalized economic telemetry to the v2 runtime.
// It is intentionally genesis-only for now: attaching a collector after blocks
// have finalized would require authenticated historical replay first. The node
// owns an independent deep copy so callers cannot mutate telemetry outside the
// runtime synchronization boundary.
func (r *Runtime) EnableShadowEconomics(collector *economics.EpochCollector) error {
	if r == nil || collector == nil {
		return ErrRuntimeConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Height != 0 || r.economicCollector != nil || collector.ShardCount() != r.ShardCount || collector.NativeTokenID() != r.NativeToken {
		return ErrRuntimeConfig
	}
	owned := collector.Clone()
	if owned == nil {
		return ErrRuntimeConfig
	}
	r.economicCollector = owned
	return nil
}

// EnableShadowEconomicEpochs enables automatic shadow epoch closure. The
// closed epoch is evaluated only after its boundary block finalizes; the
// resulting MonetaryEpochState is then inserted into the first consensus
// candidate of the next epoch. No live minting occurs.
//
// Epoch length is deliberately at least two blocks so accepting the previous
// epoch object and closing the current epoch are separate consensus heights.
func (r *Runtime) EnableShadowEconomicEpochs(engine *economics.ShadowEpochEngine, epochLength uint64, balances economics.MonetaryBalanceSnapshot) error {
	if r == nil || engine == nil {
		return ErrRuntimeConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Height != 0 || r.economicCollector == nil || r.economicEngine != nil || r.pendingEconomic != nil ||
		r.economicCollector.Epoch() != 1 || engine.Network != r.Network || epochLength < 2 ||
		balances.TotalSupply == 0 || balances.StakedSupply > balances.TotalSupply ||
		balances.ProtocolReserve > balances.TotalSupply || balances.BaseFee == 0 {
		return ErrRuntimeConfig
	}
	owned := engine.Clone()
	if owned == nil {
		return ErrRuntimeConfig
	}
	r.economicEngine = owned
	r.economicEpochLength = epochLength
	r.economicBalances = balances
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

// PendingEconomicState returns the epoch state waiting to be committed through
// the next normal v2 candidate, if any.
func (r *Runtime) PendingEconomicState() (economics.MonetaryEpochState, bool) {
	if r == nil {
		return economics.MonetaryEpochState{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingEconomic == nil {
		return economics.MonetaryEpochState{}, false
	}
	return r.pendingEconomic.State, true
}

// FinalizedEconomicState returns the last shadow monetary state accepted by a
// consensus-finalized candidate.
func (r *Runtime) FinalizedEconomicState() (economics.MonetaryEpochState, bool) {
	if r == nil {
		return economics.MonetaryEpochState{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.economicEngine == nil {
		return economics.MonetaryEpochState{}, false
	}
	return r.economicEngine.PreviousState()
}
