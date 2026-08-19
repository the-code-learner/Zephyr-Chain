package node

import (
	"errors"
	"os"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

// EnableGlobalCommitJournal configures the crash-recovery journal used to make
// a QC-authorized multi-shard state transition recoverable across process or I/O
// failure. If a journal already exists, the runtime fails closed until
// RecoverGlobalCommitJournal validates it.
func (r *Runtime) EnableGlobalCommitJournal(path string) error {
	if r == nil || path == "" {
		return ErrRuntimeConfig
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.globalCommitJournalPath != "" {
		return ErrRuntimeConfig
	}
	r.globalCommitJournalPath = path
	if info, err := os.Stat(path); err == nil {
		if info.Size() > 0 {
			r.recoveryRequired = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RecoverGlobalCommitJournal verifies the certified commit intent and repairs a
// PREPARING journal by applying only shards still at their exact pre-root. A
// shard already at its post-root is never applied twice. A COMMITTED journal
// requires every shard to already be at its post-root.
func (r *Runtime) RecoverGlobalCommitJournal(validators v2consensus.ValidatorSet, registry *compute.WorkRegistry, engineConfig economics.ShadowEpochEngineConfig) error {
	if r == nil {
		return ErrGlobalCommitJournal
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.globalCommitJournalPath == "" {
		return ErrGlobalCommitJournal
	}
	intent, err := readGlobalCommitJournal(r.globalCommitJournalPath)
	if err != nil {
		return err
	}
	if intent.Network != r.Network || intent.NativeToken != r.NativeToken || intent.ValidatorRoot != r.ValidatorRoot || intent.ShardCount != r.ShardCount {
		return ErrGlobalCommitJournal
	}
	validatorRoot, err := validators.Root()
	if err != nil || validatorRoot != r.ValidatorRoot || validators.Network != r.Network {
		return ErrGlobalCommitJournal
	}
	if err := validators.VerifyCertificate(intent.Certificate); err != nil {
		return err
	}
	if intent.Certificate.HeaderHash != v2consensus.HeaderConsensusHash(intent.Header) || intent.Certificate.Height != intent.Header.Height {
		return ErrGlobalCommitJournal
	}
	postParent := v2consensus.HeaderConsensusHash(intent.Header)
	atPreAnchor := r.Height == intent.PreHeight && r.ParentHash == intent.PreParentHash
	atPostAnchor := r.Height == intent.Header.Height && r.ParentHash == postParent
	if !atPreAnchor && !atPostAnchor {
		return ErrGlobalCommitJournal
	}

	commitments := make(map[uint32]types.Hash, intent.ShardCount)
	for _, commitment := range intent.Commitments {
		commitments[commitment.ShardID] = commitment.StateRoot
	}
	deltas := make(map[uint32]journalShardDelta, len(intent.Deltas))
	for _, delta := range intent.Deltas {
		deltas[delta.ShardID] = delta
	}

	for shard := uint32(0); shard < r.ShardCount; shard++ {
		postRoot, ok := commitments[shard]
		if !ok {
			return ErrGlobalCommitJournal
		}
		current := r.States[shard].Root()
		delta, changed := deltas[shard]
		if !changed {
			if current != postRoot {
				return ErrGlobalCommitJournal
			}
			continue
		}
		if current == delta.PostRoot {
			continue
		}
		if intent.Status == globalCommitCommitted || current != delta.PreRoot || postRoot != delta.PostRoot {
			return ErrGlobalCommitJournal
		}
		simulator, ok := r.States[shard].(worldstate.Simulator)
		if !ok {
			return ErrStateSimulation
		}
		previewRoot, err := simulator.Simulate(delta.Consumed, delta.Created)
		if err != nil || previewRoot != delta.PostRoot {
			return ErrGlobalCommitJournal
		}
		appliedRoot, err := r.States[shard].Apply(delta.Consumed, delta.Created)
		if err != nil || appliedRoot != delta.PostRoot {
			r.recoveryRequired = true
			if err != nil {
				return errors.Join(ErrRuntimeRecoveryRequired, err)
			}
			return errors.Join(ErrRuntimeRecoveryRequired, ErrGlobalCommitJournal)
		}
	}

	if err := r.restoreJournalEconomics(intent.EconomicCheckpoint, registry, engineConfig, intent.Header.Height, postParent); err != nil {
		return err
	}
	if intent.Status != globalCommitCommitted {
		intent.Status = globalCommitCommitted
		if err := writeGlobalCommitJournal(r.globalCommitJournalPath, intent); err != nil {
			r.recoveryRequired = true
			return errors.Join(ErrRuntimeRecoveryRequired, err)
		}
	}
	r.Height = intent.Header.Height
	r.ParentHash = postParent
	r.recoveryRequired = false
	return nil
}

func (r *Runtime) restoreJournalEconomics(raw []byte, registry *compute.WorkRegistry, engineConfig economics.ShadowEpochEngineConfig, height uint64, parent types.Hash) error {
	if len(raw) == 0 {
		if r.economicCollector != nil || r.economicEngine != nil || r.pendingEconomic != nil {
			return ErrGlobalCommitJournal
		}
		return nil
	}
	decoded, err := decodeEconomicCheckpoint(raw, registry, engineConfig)
	if err != nil {
		return err
	}
	if decoded.network != r.Network || decoded.nativeToken != r.NativeToken || decoded.validatorRoot != r.ValidatorRoot ||
		decoded.shardCount != r.ShardCount || decoded.height != height || decoded.parentHash != parent || decoded.collector.LastHeight() != height {
		return ErrGlobalCommitJournal
	}
	r.economicCollector = decoded.collector
	r.economicEngine = decoded.engine
	r.pendingEconomic = decoded.pending
	r.economicEpochLength = decoded.epochLength
	r.economicBalances = decoded.balances
	return nil
}

func (r *Runtime) postCommitEconomicCheckpoint(collector *economics.EpochCollector, engine *economics.ShadowEpochEngine, pending *economics.ShadowEpochPreview, balances economics.MonetaryBalanceSnapshot, height uint64, parent types.Hash) ([]byte, error) {
	if collector == nil {
		return nil, nil
	}
	temporary := Runtime{
		Network: r.Network, NativeToken: r.NativeToken, ValidatorRoot: r.ValidatorRoot,
		ShardCount: r.ShardCount, Height: height, ParentHash: parent,
		economicCollector: collector, economicEngine: engine,
		economicEpochLength: r.economicEpochLength, economicBalances: balances, pendingEconomic: pending,
	}
	return temporary.economicCheckpointBytesLocked()
}
