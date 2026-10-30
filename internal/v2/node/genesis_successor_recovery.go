package node

import (
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// RecoverCertifiedGenesisSuccessor is a deliberately narrow, single-height
// recovery gate for observers. It proves that the *latest* global commit
// journal has an immediately genesis-anchored, quorum-certified height 1
// and that all predecessor state roots match independently derived genesis
// roots. It then invokes the existing QC-verified, idempotent journal
// recovery to reconcile PREPARING/COMMITTED shard writes.
//
// It does NOT verify a multi-height finalized history: global commit journal
// currently retains only the latest intent. A latest height >=2, an unknown
// validator-set transition or any ambiguous predecessor fails closed.
func (r *Runtime) RecoverCertifiedGenesisSuccessor(validators v2consensus.ValidatorSet, genesisRoots map[uint32]types.Hash) error {
	if r == nil {
		return ErrGlobalCommitJournal
	}
	r.mu.Lock()
	if r.globalCommitJournalPath == "" || r.Height != 0 ||
		r.ParentHash != (types.Hash{}) || len(genesisRoots) != int(r.ShardCount) {
		r.mu.Unlock()
		return ErrGlobalCommitJournal
	}
	path, network, native, validatorRoot, shards := r.globalCommitJournalPath, r.Network, r.NativeToken, r.ValidatorRoot, r.ShardCount
	r.mu.Unlock()
	intent, err := readGlobalCommitJournal(path)
	if err != nil {
		return err
	}
	if intent.PreHeight != 0 || intent.PreParentHash != (types.Hash{}) ||
		intent.Header.Height != 1 || intent.Header.ParentHash != (types.Hash{}) ||
		intent.Network != network || intent.NativeToken != native ||
		intent.ValidatorRoot != validatorRoot || intent.ShardCount != shards ||
		intent.Header.EffectiveNextValidatorRoot() != validatorRoot {
		return ErrGlobalCommitJournal
	}
	currentRoot, err := validators.Root()
	if err != nil || validators.Network != network || currentRoot != validatorRoot ||
		validators.VerifyCertificate(intent.Certificate) != nil {
		return ErrGlobalCommitJournal
	}
	deltas := make(map[uint32]journalShardDelta, len(intent.Deltas))
	for _, delta := range intent.Deltas {
		deltas[delta.ShardID] = delta
	}
	commitments := make(map[uint32]types.Hash, len(intent.Commitments))
	for _, c := range intent.Commitments {
		commitments[c.ShardID] = c.StateRoot
	}
	for shard := uint32(0); shard < shards; shard++ {
		root, ok := genesisRoots[shard]
		if !ok || types.IsZero32([32]byte(root)) {
			return ErrGlobalCommitJournal
		}
		if delta, changed := deltas[shard]; changed {
			if delta.PreRoot != root || delta.PostRoot != commitments[shard] {
				return ErrGlobalCommitJournal
			}
		} else if commitments[shard] != root {
			return ErrGlobalCommitJournal
		}
	}
	// Preserve the existing journal's QC, partial-apply and state-root checks.
	// No collector/economic engine is activated by this observer-only path.
	if err := r.RecoverGlobalCommitJournal(validators, nil, economics.ShadowEpochEngineConfig{}); err != nil {
		return err
	}
	if r.Height != 1 || r.ParentHash != v2consensus.HeaderConsensusHash(intent.Header) || r.RecoveryRequired() {
		return ErrGlobalCommitJournal
	}
	return nil
}
