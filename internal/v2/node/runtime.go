package node

import (
	"errors"
	"runtime"
	"sync"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

var (
	ErrRuntimeConfig           = errors.New("invalid v2 runtime configuration")
	ErrCandidateHeight         = errors.New("invalid v2 candidate height")
	ErrCandidateState          = errors.New("v2 candidate does not match committed state")
	ErrCandidateCert           = errors.New("v2 candidate certificate mismatch")
	ErrStateSimulation         = errors.New("v2 backend does not support state simulation")
	ErrReceiptImport           = errors.New("invalid v2 receipt import")
	ErrRuntimeRecoveryRequired = errors.New("v2 runtime recovery required")
)

type ReceiptImport struct {
	Header          sharding.GlobalHeader
	Certificate     v2consensus.Certificate
	Validators      v2consensus.ValidatorSet
	Commitment      sharding.Commitment
	CommitmentProof merkle.Proof
	Receipt         sharding.CrossShardReceipt
	ReceiptProof    merkle.Proof
}

type ShardBatch struct {
	Transactions []tx.Transaction
	Imports      []ReceiptImport
	DataRoot     types.Hash
}

type shardDelta struct {
	Consumed []types.ObjectID
	Created  []object.Object
}

type Candidate struct {
	Header                   sharding.GlobalHeader
	Commitments              []sharding.Commitment
	Results                  map[uint32][]execution.Result
	Receipts                 map[uint32][]sharding.CrossShardReceipt
	deltas                   map[uint32]shardDelta
	economicObservations     map[uint32]economics.FinalizedShardObservation
	economicPendingStateHash types.Hash
}

type Runtime struct {
	mu                      sync.Mutex
	Network                 types.NetworkID
	NativeToken             types.TokenID
	ValidatorRoot           types.Hash
	ShardCount              uint32
	States                  map[uint32]worldstate.Backend
	Workers                 int
	Height                  uint64
	ParentHash              types.Hash
	economicCollector       *economics.EpochCollector
	economicEngine          *economics.ShadowEpochEngine
	economicEpochLength     uint64
	economicBalances        economics.MonetaryBalanceSnapshot
	pendingEconomic         *economics.ShadowEpochPreview
	globalCommitJournalPath string
	history                 *CertifiedHistory
	anchorStore             *FinalityAnchorStore
	recoveryRequired        bool
}

func NewRuntime(network types.NetworkID, nativeToken types.TokenID, validatorRoot types.Hash, states map[uint32]worldstate.Backend, workers int) (*Runtime, error) {
	if types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(nativeToken)) || types.IsZero32([32]byte(validatorRoot)) || len(states) == 0 {
		return nil, ErrRuntimeConfig
	}
	count := uint32(len(states))
	for shard := uint32(0); shard < count; shard++ {
		if states[shard] == nil {
			return nil, ErrRuntimeConfig
		}
	}
	return &Runtime{Network: network, NativeToken: nativeToken, ValidatorRoot: validatorRoot, ShardCount: count, States: states, Workers: workers}, nil
}

func (r *Runtime) BuildCandidate(height uint64, batches map[uint32]ShardBatch) (Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recoveryRequired {
		return Candidate{}, ErrRuntimeRecoveryRequired
	}
	if height != r.Height+1 || height == 0 {
		return Candidate{}, ErrCandidateHeight
	}
	candidate := Candidate{
		Results:              make(map[uint32][]execution.Result),
		Receipts:             make(map[uint32][]sharding.CrossShardReceipt),
		deltas:               make(map[uint32]shardDelta),
		economicObservations: make(map[uint32]economics.FinalizedShardObservation),
	}
	// Each shard has independent state. Bound concurrency and join every
	// shard before constructing canonical, ordered commitments and roots.
	outputs := make([]shardBuildOutput, r.ShardCount)
	buildShard := func(shard uint32) {
		out, err := r.buildShardCandidate(height, shard, batches[shard])
		out.err = err
		outputs[shard] = out
	}
	workers := r.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > int(r.ShardCount) {
		workers = int(r.ShardCount)
	}
	if workers <= 1 {
		for shard := uint32(0); shard < r.ShardCount; shard++ {
			buildShard(shard)
			if outputs[shard].err != nil {
				return Candidate{}, outputs[shard].err
			}
		}
	} else {
		jobs := make(chan uint32)
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				for shard := range jobs {
					buildShard(shard)
				}
			}()
		}
		for shard := uint32(0); shard < r.ShardCount; shard++ {
			jobs <- shard
		}
		close(jobs)
		wg.Wait()
	}
	commitments := make([]sharding.Commitment, 0, r.ShardCount)
	dataLeaves := make([]types.Hash, 0, r.ShardCount)
	for shard := uint32(0); shard < r.ShardCount; shard++ {
		out := outputs[shard]
		if out.err != nil {
			return Candidate{}, out.err
		}
		if out.hasTransactions {
			candidate.Results[shard] = out.results
		}
		candidate.Receipts[shard] = out.receipts
		if out.hasDelta {
			candidate.deltas[shard] = out.delta
		}
		if shard == 0 {
			candidate.economicPendingStateHash = out.economicPendingStateHash
		}
		if r.economicCollector != nil {
			candidate.economicObservations[shard] = out.observation
		}
		commitments = append(commitments, out.commitment)
		dataLeaves = append(dataLeaves, out.dataLeaf)
	}
	commitmentRoot, err := sharding.CommitmentRoot(commitments)
	if err != nil {
		return Candidate{}, err
	}
	candidate.Commitments = commitments
	candidate.Header = sharding.GlobalHeader{Version: 2, Network: r.Network, Height: height, ParentHash: r.ParentHash, ShardCommitmentRoot: commitmentRoot, ValidatorRoot: r.ValidatorRoot, NextValidatorRoot: r.ValidatorRoot, DataRoot: merkle.Root(dataLeaves)}
	return candidate, nil
}

type shardBuildOutput struct {
	results                  []execution.Result
	receipts                 []sharding.CrossShardReceipt
	delta                    shardDelta
	commitment               sharding.Commitment
	dataLeaf                 types.Hash
	observation              economics.FinalizedShardObservation
	economicPendingStateHash types.Hash
	hasTransactions          bool
	hasDelta                 bool
	err                      error
}

func (r *Runtime) buildShardCandidate(height uint64, shard uint32, batch ShardBatch) (shardBuildOutput, error) {
	out := shardBuildOutput{}
	store := r.States[shard]
	currentRoot := store.Root()
	delta := shardDelta{}
	results := make([]execution.Result, 0, len(batch.Transactions))
	if len(batch.Transactions) > 0 {
		for _, transaction := range batch.Transactions {
			if transaction.ShardID != shard || transaction.StateRoot != currentRoot {
				return shardBuildOutput{}, ErrCandidateState
			}
		}
		executor := execution.BatchExecutor{Engine: execution.Engine{Network: r.Network, NativeToken: r.NativeToken, ShardCount: r.ShardCount, Height: height}, Workers: r.Workers}
		var err error
		results, err = executor.ExecuteBatch(batch.Transactions)
		if err != nil {
			return shardBuildOutput{}, err
		}
		for _, result := range results {
			delta.Consumed = append(delta.Consumed, result.Consumed...)
			delta.Created = append(delta.Created, result.Created...)
		}
		out.hasTransactions = true
		out.results = results
	}
	for _, receiptImport := range batch.Imports {
		if err := r.validateReceiptImport(shard, receiptImport); err != nil {
			return shardBuildOutput{}, err
		}
		destinationObject, err := receiptImport.Receipt.DestinationObject()
		if err != nil {
			return shardBuildOutput{}, ErrReceiptImport
		}
		marker, err := sharding.ReceiptMarker(receiptImport.Receipt)
		if err != nil {
			return shardBuildOutput{}, ErrReceiptImport
		}
		if _, exists := store.GetObject(destinationObject.ID); exists {
			return shardBuildOutput{}, sharding.ErrReceiptReplay
		}
		if _, exists := store.GetObject(marker.ID); exists {
			return shardBuildOutput{}, sharding.ErrReceiptReplay
		}
		delta.Created = append(delta.Created, destinationObject, marker)
	}

	if shard == 0 && r.pendingEconomic != nil {
		stateHash, err := r.pendingEconomic.State.Hash()
		if err != nil {
			return shardBuildOutput{}, err
		}
		if err := appendEconomicStateDelta(&delta, *r.pendingEconomic); err != nil {
			return shardBuildOutput{}, err
		}
		out.economicPendingStateHash = stateHash
	}

	newRoot := currentRoot
	if len(delta.Consumed) > 0 || len(delta.Created) > 0 {
		simulator, ok := store.(worldstate.Simulator)
		if !ok {
			return shardBuildOutput{}, ErrStateSimulation
		}
		var err error
		newRoot, err = simulator.Simulate(delta.Consumed, delta.Created)
		if err != nil {
			return shardBuildOutput{}, err
		}
		out.hasDelta = true
		out.delta = delta
	}
	receipts := make([]sharding.CrossShardReceipt, 0)
	for _, result := range results {
		for _, outbound := range result.Outbound {
			receipts = append(receipts, sharding.CrossShardReceipt{SourceShard: shard, DestinationShard: outbound.DestinationShard, SourceHeight: height, TransactionID: result.TxID, OutputIndex: outbound.OutputIndex, Output: outbound.Output, SourceStateRoot: newRoot})
		}
	}
	receiptRoot, err := (sharding.ReceiptBatch{Receipts: receipts}).Root()
	if err != nil {
		return shardBuildOutput{}, err
	}
	out.receipts = receipts
	dataRoot := batch.DataRoot
	if types.IsZero32([32]byte(dataRoot)) {
		dataRoot = merkle.Root(nil)
	}
	out.commitment = sharding.Commitment{ShardID: shard, StateRoot: newRoot, ReceiptRoot: receiptRoot, DataRoot: dataRoot}
	out.dataLeaf = merkle.Leaf("shard-data-root", dataRoot[:])

	if r.economicCollector != nil {
		imports := make([]sharding.CrossShardReceipt, 0, len(batch.Imports))
		for _, receiptImport := range batch.Imports {
			imports = append(imports, receiptImport.Receipt)
		}
		out.observation = economics.FinalizedShardObservation{
			Transactions: append([]tx.Transaction(nil), batch.Transactions...),
			Results:      append([]execution.Result(nil), results...),
			Imports:      imports,
			Exports:      append([]sharding.CrossShardReceipt(nil), receipts...),
		}
	}
	return out, nil
}

func appendEconomicStateDelta(delta *shardDelta, preview economics.ShadowEpochPreview) error {
	if delta == nil || len(preview.Created) != 1 || preview.Created[0].ID != economics.MonetaryStateObjectID(preview.State.Network) {
		return ErrCandidateState
	}
	monetaryID := preview.Created[0].ID
	for _, id := range delta.Consumed {
		if id == monetaryID {
			return ErrCandidateState
		}
	}
	for _, created := range delta.Created {
		if created.ID == monetaryID {
			return ErrCandidateState
		}
	}
	delta.Consumed = append(delta.Consumed, preview.Consumed...)
	delta.Created = append(delta.Created, preview.Created...)
	return nil
}

func (r *Runtime) validateReceiptImport(destinationShard uint32, receiptImport ReceiptImport) error {
	if receiptImport.Header.Network != r.Network || receiptImport.Validators.Network != r.Network || receiptImport.Certificate.Network != r.Network || receiptImport.Receipt.DestinationShard != destinationShard || receiptImport.Header.Height > r.Height || receiptImport.Header.Height != receiptImport.Receipt.SourceHeight {
		return ErrReceiptImport
	}
	validatorRoot, err := receiptImport.Validators.Root()
	if err != nil || validatorRoot != receiptImport.Header.ValidatorRoot {
		return ErrReceiptImport
	}
	if receiptImport.Header.CertificateHash != receiptImport.Certificate.Hash() || receiptImport.Certificate.HeaderHash != v2consensus.HeaderConsensusHash(receiptImport.Header) || receiptImport.Certificate.Height != receiptImport.Header.Height {
		return ErrReceiptImport
	}
	if err := receiptImport.Validators.VerifyCertificate(receiptImport.Certificate); err != nil {
		return ErrReceiptImport
	}
	if err := sharding.VerifyFinalizedReceipt(receiptImport.Header, receiptImport.Commitment, receiptImport.CommitmentProof, receiptImport.Receipt, receiptImport.ReceiptProof); err != nil {
		return ErrReceiptImport
	}
	return nil
}

func (r *Runtime) Commit(candidate Candidate, certificate v2consensus.Certificate, validators v2consensus.ValidatorSet) (sharding.GlobalHeader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recoveryRequired {
		return sharding.GlobalHeader{}, ErrRuntimeRecoveryRequired
	}
	if candidate.Header.Height != r.Height+1 || candidate.Header.ParentHash != r.ParentHash || candidate.Header.Network != r.Network {
		return sharding.GlobalHeader{}, ErrCandidateState
	}
	validatorRoot, err := validators.Root()
	if err != nil || validatorRoot != candidate.Header.ValidatorRoot || validatorRoot != r.ValidatorRoot {
		return sharding.GlobalHeader{}, ErrCandidateCert
	}
	if certificate.HeaderHash != v2consensus.HeaderConsensusHash(candidate.Header) || certificate.Height != candidate.Header.Height || certificate.Network != r.Network {
		return sharding.GlobalHeader{}, ErrCandidateCert
	}
	if err := validators.VerifyCertificate(certificate); err != nil {
		return sharding.GlobalHeader{}, err
	}

	pendingApplied := r.pendingEconomic != nil
	if pendingApplied {
		expected, err := r.pendingEconomic.State.Hash()
		if err != nil || candidate.economicPendingStateHash != expected {
			return sharding.GlobalHeader{}, ErrCandidateState
		}
	} else if !types.IsZero32([32]byte(candidate.economicPendingStateHash)) {
		return sharding.GlobalHeader{}, ErrCandidateState
	}

	commitments, shards, err := r.preflightCandidateState(candidate)
	if err != nil {
		return sharding.GlobalHeader{}, err
	}

	var economicPreview *economics.EpochCollector
	if r.economicCollector != nil {
		if len(candidate.economicObservations) != int(r.ShardCount) {
			return sharding.GlobalHeader{}, ErrCandidateState
		}
		economicPreview, err = r.economicCollector.PreviewFinalizedBlock(candidate.Header.Height, candidate.economicObservations)
		if err != nil {
			return sharding.GlobalHeader{}, err
		}
	}

	var enginePreview *economics.ShadowEpochEngine
	if r.economicEngine != nil {
		enginePreview = r.economicEngine.Clone()
		if enginePreview == nil {
			return sharding.GlobalHeader{}, ErrRuntimeConfig
		}
		if pendingApplied {
			if err := enginePreview.Accept(*r.pendingEconomic); err != nil {
				return sharding.GlobalHeader{}, err
			}
		}
	} else if pendingApplied {
		return sharding.GlobalHeader{}, ErrRuntimeConfig
	}

	var nextPending *economics.ShadowEpochPreview
	nextBalances := r.economicBalances
	if r.economicEngine != nil && r.economicEpochLength > 0 && candidate.Header.Height%r.economicEpochLength == 0 {
		if economicPreview == nil {
			return sharding.GlobalHeader{}, ErrRuntimeConfig
		}
		metrics, verifiedWork, err := economicPreview.FinalizeEpoch()
		if err != nil {
			return sharding.GlobalHeader{}, err
		}
		aggregate, err := economics.AggregateEpochMetrics(metrics)
		if err != nil || nextBalances.TotalSupply < aggregate.BurnedFees {
			return sharding.GlobalHeader{}, ErrRuntimeConfig
		}
		nextBalances.TotalSupply -= aggregate.BurnedFees
		preview, err := enginePreview.PreviewCloseEpoch(metrics, verifiedWork, nextBalances)
		if err != nil {
			return sharding.GlobalHeader{}, err
		}
		if err := economicPreview.AdvanceEpoch(preview.State.Epoch + 1); err != nil {
			return sharding.GlobalHeader{}, err
		}
		nextPending = &preview
	}

	postPending := r.pendingEconomic
	if nextPending != nil {
		postPending = nextPending
	} else if pendingApplied {
		postPending = nil
	}
	finalized := candidate.Header
	finalized.CertificateHash = certificate.Hash()
	postParent := v2consensus.HeaderConsensusHash(finalized)

	var journalIntent *globalCommitIntent
	if r.globalCommitJournalPath != "" {
		economicCheckpoint, err := r.postCommitEconomicCheckpoint(economicPreview, enginePreview, postPending, nextBalances, finalized.Height, postParent)
		if err != nil {
			return sharding.GlobalHeader{}, err
		}
		intent, err := r.buildGlobalCommitIntent(candidate, certificate, commitments, economicCheckpoint)
		if err != nil {
			return sharding.GlobalHeader{}, err
		}
		if err := writeGlobalCommitJournal(r.globalCommitJournalPath, intent); err != nil {
			r.recoveryRequired = true
			return sharding.GlobalHeader{}, errors.Join(ErrRuntimeRecoveryRequired, err)
		}
		journalIntent = &intent
	}

	// Preserve the entire certified ancestry before mutating any shard.
	// An interrupted/partial history append is a hard recovery boundary.
	if r.history != nil {
		if journalIntent == nil {
			r.recoveryRequired = true
			return sharding.GlobalHeader{}, ErrRuntimeRecoveryRequired
		}
		if err := r.history.Append(*journalIntent, validators); err != nil {
			r.recoveryRequired = true
			return sharding.GlobalHeader{}, errors.Join(ErrRuntimeRecoveryRequired, err)
		}
	}

	for _, shardValue := range shards {
		shard := uint32(shardValue)
		delta := candidate.deltas[shard]
		root, err := r.States[shard].Apply(delta.Consumed, delta.Created)
		if err != nil {
			r.recoveryRequired = true
			return sharding.GlobalHeader{}, errors.Join(ErrRuntimeRecoveryRequired, err)
		}
		if root != commitments[shard].StateRoot {
			r.recoveryRequired = true
			return sharding.GlobalHeader{}, errors.Join(ErrRuntimeRecoveryRequired, ErrCandidateState)
		}
	}

	if journalIntent != nil {
		journalIntent.Status = globalCommitCommitted
		if err := writeGlobalCommitJournal(r.globalCommitJournalPath, *journalIntent); err != nil {
			r.recoveryRequired = true
			return sharding.GlobalHeader{}, errors.Join(ErrRuntimeRecoveryRequired, err)
		}
	}

	// The independently held high-water mark must be durable before a
	// successful commit is returned. Failure after WAL state writes is a
	// fail-closed recovery-required state, not an unanchored success.
	if r.anchorStore != nil {
		if err := r.anchorStore.advance(FinalityCheckpoint{
			Network: r.Network, ValidatorRoot: r.ValidatorRoot,
			Height: finalized.Height, HeaderHash: postParent,
		}); err != nil {
			r.recoveryRequired = true
			return sharding.GlobalHeader{}, errors.Join(ErrRuntimeRecoveryRequired, ErrFinalityAnchor, err)
		}
	}

	if economicPreview != nil {
		r.economicCollector = economicPreview
	}
	if enginePreview != nil {
		r.economicEngine = enginePreview
	}
	r.pendingEconomic = postPending
	r.economicBalances = nextBalances
	r.Height = finalized.Height
	r.ParentHash = postParent
	return finalized, nil
}
