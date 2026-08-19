package node

import (
	"errors"
	"sort"
	"sync"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

var (
	ErrRuntimeConfig   = errors.New("invalid v2 runtime configuration")
	ErrCandidateHeight = errors.New("invalid v2 candidate height")
	ErrCandidateState  = errors.New("v2 candidate does not match committed state")
	ErrCandidateCert   = errors.New("v2 candidate certificate mismatch")
	ErrStateSimulation = errors.New("v2 backend does not support state simulation")
	ErrReceiptImport   = errors.New("invalid v2 receipt import")
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
	Header      sharding.GlobalHeader
	Commitments []sharding.Commitment
	Results     map[uint32][]execution.Result
	Receipts    map[uint32][]sharding.CrossShardReceipt
	deltas      map[uint32]shardDelta
}

type Runtime struct {
	mu            sync.Mutex
	Network       types.NetworkID
	NativeToken   types.TokenID
	ValidatorRoot types.Hash
	ShardCount    uint32
	States        map[uint32]worldstate.Backend
	Workers       int
	Height        uint64
	ParentHash    types.Hash
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

// BuildCandidate executes and simulates every shard against committed state.
// It never mutates the backing state stores. Receipt imports become destination
// objects plus durable anti-replay markers, but are not spendable until a later
// block because all transactions in this candidate target the pre-state root.
func (r *Runtime) BuildCandidate(height uint64, batches map[uint32]ShardBatch) (Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if height != r.Height+1 || height == 0 {
		return Candidate{}, ErrCandidateHeight
	}
	candidate := Candidate{
		Results:  make(map[uint32][]execution.Result),
		Receipts: make(map[uint32][]sharding.CrossShardReceipt),
		deltas:   make(map[uint32]shardDelta),
	}
	commitments := make([]sharding.Commitment, 0, r.ShardCount)
	dataLeaves := make([]types.Hash, 0, r.ShardCount)

	for shard := uint32(0); shard < r.ShardCount; shard++ {
		store := r.States[shard]
		batch := batches[shard]
		currentRoot := store.Root()
		delta := shardDelta{}
		results := make([]execution.Result, 0, len(batch.Transactions))

		if len(batch.Transactions) > 0 {
			for _, transaction := range batch.Transactions {
				if transaction.ShardID != shard || transaction.StateRoot != currentRoot {
					return Candidate{}, ErrCandidateState
				}
			}
			executor := execution.BatchExecutor{Engine: execution.Engine{Network: r.Network, NativeToken: r.NativeToken, ShardCount: r.ShardCount}, Workers: r.Workers}
			var err error
			results, err = executor.ExecuteBatch(batch.Transactions)
			if err != nil {
				return Candidate{}, err
			}
			for _, result := range results {
				delta.Consumed = append(delta.Consumed, result.Consumed...)
				delta.Created = append(delta.Created, result.Created...)
			}
			candidate.Results[shard] = results
		}

		for _, receiptImport := range batch.Imports {
			if err := r.validateReceiptImport(shard, receiptImport); err != nil {
				return Candidate{}, err
			}
			destinationObject, err := receiptImport.Receipt.DestinationObject()
			if err != nil {
				return Candidate{}, ErrReceiptImport
			}
			marker, err := sharding.ReceiptMarker(receiptImport.Receipt)
			if err != nil {
				return Candidate{}, ErrReceiptImport
			}
			if _, exists := store.GetObject(destinationObject.ID); exists {
				return Candidate{}, sharding.ErrReceiptReplay
			}
			if _, exists := store.GetObject(marker.ID); exists {
				return Candidate{}, sharding.ErrReceiptReplay
			}
			delta.Created = append(delta.Created, destinationObject, marker)
		}

		newRoot := currentRoot
		if len(delta.Consumed) > 0 || len(delta.Created) > 0 {
			simulator, ok := store.(worldstate.Simulator)
			if !ok {
				return Candidate{}, ErrStateSimulation
			}
			var err error
			newRoot, err = simulator.Simulate(delta.Consumed, delta.Created)
			if err != nil {
				return Candidate{}, err
			}
			candidate.deltas[shard] = delta
		}

		receipts := make([]sharding.CrossShardReceipt, 0)
		for _, result := range results {
			for _, outbound := range result.Outbound {
				receipts = append(receipts, sharding.CrossShardReceipt{
					SourceShard: shard, DestinationShard: outbound.DestinationShard,
					SourceHeight: height, TransactionID: result.TxID, OutputIndex: outbound.OutputIndex,
					Output: outbound.Output, SourceStateRoot: newRoot,
				})
			}
		}
		receiptRoot, err := (sharding.ReceiptBatch{Receipts: receipts}).Root()
		if err != nil {
			return Candidate{}, err
		}
		candidate.Receipts[shard] = receipts

		dataRoot := batch.DataRoot
		if types.IsZero32([32]byte(dataRoot)) {
			dataRoot = merkle.Root(nil)
		}
		commitments = append(commitments, sharding.Commitment{ShardID: shard, StateRoot: newRoot, ReceiptRoot: receiptRoot, DataRoot: dataRoot})
		dataLeaves = append(dataLeaves, merkle.Leaf("shard-data-root", dataRoot[:]))
	}
	commitmentRoot, err := sharding.CommitmentRoot(commitments)
	if err != nil {
		return Candidate{}, err
	}
	candidate.Commitments = commitments
	candidate.Header = sharding.GlobalHeader{
		Version: 2, Network: r.Network, Height: height, ParentHash: r.ParentHash,
		ShardCommitmentRoot: commitmentRoot, ValidatorRoot: r.ValidatorRoot,
		DataRoot: merkle.Root(dataLeaves),
	}
	return candidate, nil
}

func (r *Runtime) validateReceiptImport(destinationShard uint32, receiptImport ReceiptImport) error {
	if receiptImport.Header.Network != r.Network || receiptImport.Validators.Network != r.Network ||
		receiptImport.Certificate.Network != r.Network || receiptImport.Receipt.DestinationShard != destinationShard ||
		receiptImport.Header.Height > r.Height || receiptImport.Header.Height != receiptImport.Receipt.SourceHeight {
		return ErrReceiptImport
	}
	if receiptImport.Header.CertificateHash != receiptImport.Certificate.Hash() ||
		receiptImport.Certificate.HeaderHash != v2consensus.HeaderConsensusHash(receiptImport.Header) ||
		receiptImport.Certificate.Height != receiptImport.Header.Height {
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

// Commit applies a previously simulated candidate only after a valid quorum
// certificate for its consensus hash is supplied.
func (r *Runtime) Commit(candidate Candidate, certificate v2consensus.Certificate, validators v2consensus.ValidatorSet) (sharding.GlobalHeader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if candidate.Header.Height != r.Height+1 || candidate.Header.ParentHash != r.ParentHash || candidate.Header.Network != r.Network {
		return sharding.GlobalHeader{}, ErrCandidateState
	}
	if certificate.HeaderHash != v2consensus.HeaderConsensusHash(candidate.Header) || certificate.Height != candidate.Header.Height || certificate.Network != r.Network {
		return sharding.GlobalHeader{}, ErrCandidateCert
	}
	if err := validators.VerifyCertificate(certificate); err != nil {
		return sharding.GlobalHeader{}, err
	}
	commitments := make(map[uint32]sharding.Commitment, len(candidate.Commitments))
	for _, commitment := range candidate.Commitments {
		commitments[commitment.ShardID] = commitment
	}
	shards := make([]int, 0, len(candidate.deltas))
	for shard := range candidate.deltas {
		shards = append(shards, int(shard))
	}
	sort.Ints(shards)
	for _, shardValue := range shards {
		shard := uint32(shardValue)
		delta := candidate.deltas[shard]
		root, err := r.States[shard].Apply(delta.Consumed, delta.Created)
		if err != nil {
			return sharding.GlobalHeader{}, err
		}
		if root != commitments[shard].StateRoot {
			return sharding.GlobalHeader{}, ErrCandidateState
		}
	}
	finalized := candidate.Header
	finalized.CertificateHash = certificate.Hash()
	r.Height = finalized.Height
	r.ParentHash = v2consensus.HeaderConsensusHash(finalized)
	return finalized, nil
}
