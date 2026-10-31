package lab

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"runtime"
	"sync"
	"time"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/node"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

var ErrFinalityHarnessConfig = errors.New("invalid finalized-through-consensus harness configuration")

type FinalityHarnessConfig struct {
	ShardCount           uint32
	ValidatorCount       uint32
	TransactionsPerBlock uint64
	WarmupBlocks         uint64
	MeasuredBlocks       uint64
	CrossShardBPS        uint32
	BlockCadence         time.Duration
	Workers              int
	CommitSHA            string
	Workload             string
	Topology             string
}

type FinalityHarnessResult struct {
	Report                   FinalityBenchmarkReport `json:"report"`
	GeneratedCrossShard      uint64                  `json:"generated_cross_shard_receipts"`
	ImportedCrossShard       uint64                  `json:"imported_cross_shard_receipts"`
	DrainBlocks              uint64                  `json:"drain_blocks"`
	FinalizedConsensusBlocks uint64                  `json:"finalized_consensus_blocks"`
}

type finalityHarnessSigner struct {
	key     *ecdsa.PrivateKey
	account types.AccountID
	input   object.Object
	shard   uint32
}

type finalityHarnessCluster struct {
	network       types.NetworkID
	native        types.TokenID
	validators    v2consensus.ValidatorSet
	validatorKeys map[types.ValidatorID]*ecdsa.PrivateKey
	runtimes      []*node.Runtime
	stores        []map[uint32]*worldstate.Memory
}

func RunFinalityHarness(config FinalityHarnessConfig) (FinalityHarnessResult, error) {
	if err := validateFinalityHarnessConfig(config); err != nil {
		return FinalityHarnessResult{}, err
	}
	if config.Workload == "" {
		config.Workload = "native-transfer-v2-lab"
	}
	if config.Topology == "" {
		config.Topology = "single-process-in-memory"
	}
	if config.Workers <= 0 {
		config.Workers = runtime.GOMAXPROCS(0)
	}

	cluster, signers, recipients, err := newFinalityHarnessCluster(config)
	if err != nil {
		return FinalityHarnessResult{}, err
	}

	totalTrafficBlocks := config.WarmupBlocks + config.MeasuredBlocks
	crossPerBlock := crossShardTransactionsPerBlock(config.TransactionsPerBlock, config.CrossShardBPS)
	pendingImports := make(map[uint32][]node.ReceiptImport, config.ShardCount)
	allReceipts := make([]sharding.CrossShardReceipt, 0)
	var measuredStart time.Time
	var measuredEnd time.Time
	warmupFinalized := uint64(0)
	samples := make([]FinalitySample, 0, config.MeasuredBlocks*config.TransactionsPerBlock)
	var generatedCrossShard uint64
	var importedCrossShard uint64
	var finalizedBlocks uint64
	var nextSubmission time.Time

	for blockIndex := uint64(0); blockIndex < totalTrafficBlocks; blockIndex++ {
		if !nextSubmission.IsZero() {
			if wait := time.Until(nextSubmission); wait > 0 {
				time.Sleep(wait)
			}
		}
		height := blockIndex + 1
		batches := make(map[uint32]node.ShardBatch, config.ShardCount)
		for shard, imports := range pendingImports {
			if len(imports) > 0 {
				batches[shard] = node.ShardBatch{Imports: append([]node.ReceiptImport(nil), imports...)}
				importedCrossShard += uint64(len(imports))
			}
		}
		pendingImports = make(map[uint32][]node.ReceiptImport, config.ShardCount)

		start := blockIndex * config.TransactionsPerBlock
		for offset := uint64(0); offset < config.TransactionsPerBlock; offset++ {
			signer := signers[start+offset]
			isCrossShard := offset < crossPerBlock
			destinationShard := signer.shard
			if isCrossShard {
				destinationShard = (signer.shard + 1) % config.ShardCount
			}
			recipient := recipients[destinationShard]
			transaction, err := buildFinalityHarnessTransaction(cluster, signer, recipient, height, offset)
			if err != nil {
				return FinalityHarnessResult{}, err
			}
			batch := batches[signer.shard]
			batch.Transactions = append(batch.Transactions, transaction)
			batches[signer.shard] = batch
		}

		submittedAt := time.Now()
		nextSubmission = submittedAt.Add(config.BlockCadence)
		candidates, finalizedHeader, certificate, err := cluster.finalizeHeight(height, batches)
		if err != nil {
			return FinalityHarnessResult{}, err
		}
		finalizedAt := time.Now()
		finalizedBlocks++

		imports, receipts, err := finalizedReceiptImports(candidates[0], finalizedHeader, certificate, cluster.validators)
		if err != nil {
			return FinalityHarnessResult{}, err
		}
		for shard, values := range imports {
			pendingImports[shard] = append(pendingImports[shard], values...)
		}
		allReceipts = append(allReceipts, receipts...)
		generatedCrossShard += uint64(len(receipts))

		if blockIndex < config.WarmupBlocks {
			warmupFinalized += config.TransactionsPerBlock
			continue
		}
		if measuredStart.IsZero() {
			measuredStart = submittedAt
		}
		measuredEnd = finalizedAt
		for i := uint64(0); i < config.TransactionsPerBlock; i++ {
			samples = append(samples, FinalitySample{SubmittedAt: submittedAt, FinalizedAt: finalizedAt})
		}
	}

	drainBlocks := uint64(0)
	for countPendingImports(pendingImports) > 0 {
		drainBlocks++
		height := totalTrafficBlocks + drainBlocks
		batches := make(map[uint32]node.ShardBatch, config.ShardCount)
		for shard, imports := range pendingImports {
			if len(imports) > 0 {
				batches[shard] = node.ShardBatch{Imports: append([]node.ReceiptImport(nil), imports...)}
				importedCrossShard += uint64(len(imports))
			}
		}
		pendingImports = make(map[uint32][]node.ReceiptImport, config.ShardCount)
		candidates, finalizedHeader, certificate, err := cluster.finalizeHeight(height, batches)
		if err != nil {
			return FinalityHarnessResult{}, err
		}
		finalizedBlocks++
		imports, receipts, err := finalizedReceiptImports(candidates[0], finalizedHeader, certificate, cluster.validators)
		if err != nil {
			return FinalityHarnessResult{}, err
		}
		for shard, values := range imports {
			pendingImports[shard] = append(pendingImports[shard], values...)
		}
		generatedCrossShard += uint64(len(receipts))
		if len(receipts) > 0 {
			return FinalityHarnessResult{}, ErrInvalidFinalityBenchmark
		}
	}

	if generatedCrossShard != importedCrossShard {
		return FinalityHarnessResult{}, ErrInvalidFinalityBenchmark
	}
	if err := verifyImportedReceipts(cluster, allReceipts); err != nil {
		return FinalityHarnessResult{}, err
	}

	report, err := BuildFinalityBenchmarkReport(FinalityBenchmarkInput{
		Config: FinalityBenchmarkConfig{
			ShardCount:        config.ShardCount,
			ValidatorCount:    config.ValidatorCount,
			TransactionsBlock: config.TransactionsPerBlock,
			BlockCadenceMS:    uint64(config.BlockCadence / time.Millisecond),
			CrossShardBPS:     config.CrossShardBPS,
			Workload:          config.Workload,
			Topology:          config.Topology,
		},
		Environment:     FinalityBenchmarkEnvironment{CommitSHA: config.CommitSHA},
		WarmupFinalized: warmupFinalized,
		MeasuredStart:   measuredStart,
		MeasuredEnd:     measuredEnd,
		Samples:         samples,
	})
	if err != nil {
		return FinalityHarnessResult{}, err
	}
	return FinalityHarnessResult{
		Report:                   report,
		GeneratedCrossShard:      generatedCrossShard,
		ImportedCrossShard:       importedCrossShard,
		DrainBlocks:              drainBlocks,
		FinalizedConsensusBlocks: finalizedBlocks,
	}, nil
}

func validateFinalityHarnessConfig(config FinalityHarnessConfig) error {
	if config.ShardCount == 0 || config.ValidatorCount < 4 || config.TransactionsPerBlock == 0 ||
		config.MeasuredBlocks == 0 || config.CrossShardBPS > 10_000 || config.BlockCadence < time.Millisecond ||
		config.BlockCadence%time.Millisecond != 0 || config.Workers < 0 {
		return ErrFinalityHarnessConfig
	}
	if config.CrossShardBPS > 0 && config.ShardCount < 2 {
		return ErrFinalityHarnessConfig
	}
	if config.TransactionsPerBlock > math.MaxUint64/uint64(maxUint32(config.CrossShardBPS, 1)) {
		return ErrFinalityHarnessConfig
	}
	product := config.TransactionsPerBlock * uint64(config.CrossShardBPS)
	if product%10_000 != 0 {
		return ErrFinalityHarnessConfig
	}
	if config.WarmupBlocks > math.MaxUint64-config.MeasuredBlocks ||
		(config.WarmupBlocks+config.MeasuredBlocks) > math.MaxUint64/config.TransactionsPerBlock {
		return ErrFinalityHarnessConfig
	}
	return nil
}

func crossShardTransactionsPerBlock(transactions uint64, bps uint32) uint64 {
	return transactions * uint64(bps) / 10_000
}

func newFinalityHarnessCluster(config FinalityHarnessConfig) (*finalityHarnessCluster, []finalityHarnessSigner, map[uint32]types.AccountID, error) {
	network := types.NetworkID(types.HashBytes("network", []byte("v2-finality-harness")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	validators, validatorKeys, err := deterministicValidators(network, int(config.ValidatorCount))
	if err != nil {
		return nil, nil, nil, err
	}
	validatorRoot, err := validators.Root()
	if err != nil {
		return nil, nil, nil, err
	}

	totalTransactions := (config.WarmupBlocks + config.MeasuredBlocks) * config.TransactionsPerBlock
	signers := make([]finalityHarnessSigner, totalTransactions)
	initialByShard := make(map[uint32][]object.Object, config.ShardCount)
	for i := uint64(0); i < totalTransactions; i++ {
		shard := uint32(i % uint64(config.ShardCount))
		key, account, err := deterministicAccountOnShard("finality-harness-sender", i, shard, config.ShardCount)
		if err != nil {
			return nil, nil, nil, err
		}
		out, err := object.NewCoinOutput(account, native, 100)
		if err != nil {
			return nil, nil, nil, err
		}
		seed := make([]byte, 8)
		binary.BigEndian.PutUint64(seed, i+1)
		input := object.Object{
			ID:      types.ObjectIDForShard(types.HashBytes("v2-finality-harness-input", seed), 0, shard),
			Version: 1,
			Owner:   account,
			Kind:    out.Kind,
			Data:    out.Data,
		}
		signers[i] = finalityHarnessSigner{key: key, account: account, input: input, shard: shard}
		initialByShard[shard] = append(initialByShard[shard], input)
	}

	recipients := make(map[uint32]types.AccountID, config.ShardCount)
	for shard := uint32(0); shard < config.ShardCount; shard++ {
		_, account, err := deterministicAccountOnShard("finality-harness-recipient", uint64(shard), shard, config.ShardCount)
		if err != nil {
			return nil, nil, nil, err
		}
		recipients[shard] = account
	}

	stores := make([]map[uint32]*worldstate.Memory, config.ValidatorCount)
	runtimes := make([]*node.Runtime, config.ValidatorCount)
	for validatorIndex := range stores {
		states := make(map[uint32]worldstate.Backend, config.ShardCount)
		stores[validatorIndex] = make(map[uint32]*worldstate.Memory, config.ShardCount)
		for shard := uint32(0); shard < config.ShardCount; shard++ {
			store := worldstate.NewMemory()
			if len(initialByShard[shard]) > 0 {
				if _, err := store.Apply(nil, initialByShard[shard]); err != nil {
					return nil, nil, nil, err
				}
			}
			stores[validatorIndex][shard] = store
			states[shard] = store
		}
		runtimeNode, err := node.NewRuntime(network, native, validatorRoot, states, config.Workers)
		if err != nil {
			return nil, nil, nil, err
		}
		runtimes[validatorIndex] = runtimeNode
	}
	return &finalityHarnessCluster{
		network: network, native: native, validators: validators, validatorKeys: validatorKeys,
		runtimes: runtimes, stores: stores,
	}, signers, recipients, nil
}

func buildFinalityHarnessTransaction(
	cluster *finalityHarnessCluster,
	signer finalityHarnessSigner,
	recipient types.AccountID,
	height uint64,
	offset uint64,
) (tx.Transaction, error) {
	if cluster == nil || len(cluster.stores) == 0 {
		return tx.Transaction{}, ErrFinalityHarnessConfig
	}
	store := cluster.stores[0][signer.shard]
	if store == nil {
		return tx.Transaction{}, ErrFinalityHarnessConfig
	}
	root := store.Root()
	witness, proof, ok := store.Proof(signer.input.ID)
	if !ok {
		return tx.Transaction{}, ErrInvalidFinalityBenchmark
	}
	witnessHash := witness.Hash()
	payment, err := object.NewCoinOutput(recipient, cluster.native, 25)
	if err != nil {
		return tx.Transaction{}, err
	}
	change, err := object.NewCoinOutput(signer.account, cluster.native, 74)
	if err != nil {
		return tx.Transaction{}, err
	}
	transaction := tx.Transaction{
		Version: tx.Version, Network: cluster.network, ShardID: signer.shard, StateRoot: root,
		Inputs:  []tx.InputRef{{ObjectID: signer.input.ID, Version: signer.input.Version, ObjectHash: witnessHash}},
		Outputs: []object.OutputSpec{payment, change}, Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Fee: 1, Witnesses: []tx.Witness{{Object: witness, Proof: proof}},
	}
	binary.BigEndian.PutUint64(transaction.Salt[:8], height)
	binary.BigEndian.PutUint64(transaction.Salt[8:16], offset+1)
	if err := transaction.Sign(signer.key); err != nil {
		return tx.Transaction{}, err
	}
	return transaction, nil
}

func (c *finalityHarnessCluster) finalizeHeight(
	height uint64,
	batches map[uint32]node.ShardBatch,
) ([]node.Candidate, sharding.GlobalHeader, v2consensus.Certificate, error) {
	if c == nil || len(c.runtimes) == 0 {
		return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, ErrInvalidFinalityBenchmark
	}
	// Independent validator replicas run concurrently on real networks.
	// A sequential loop made this single-process lab benchmark measure N
	// serial executions rather than the consensus critical path.
	candidates := make([]node.Candidate, len(c.runtimes))
	candidateErrors := make([]error, len(c.runtimes))
	var builds sync.WaitGroup
	for i, runtimeNode := range c.runtimes {
		builds.Add(1)
		go func(i int, runtimeNode *node.Runtime) {
			defer builds.Done()
			candidates[i], candidateErrors[i] = runtimeNode.BuildCandidate(height, batches)
		}(i, runtimeNode)
	}
	builds.Wait()
	for i, err := range candidateErrors {
		if err != nil {
			return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, err
		}
		if i > 0 && v2consensus.HeaderConsensusHash(candidates[i].Header) != v2consensus.HeaderConsensusHash(candidates[0].Header) {
			return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, ErrInvalidFinalityBenchmark
		}
	}
	proposer, err := c.validators.Proposer(height, 0)
	if err != nil {
		return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, err
	}
	proposal, err := v2consensus.SignProposal(c.validatorKeys[proposer.ID], candidates[0].Header, 0)
	if err != nil {
		return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, err
	}
	quorum := (2*len(c.validators.Validators))/3 + 1
	if quorum > len(c.runtimes) {
		return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, ErrFinalityHarnessConfig
	}
	headerHash := v2consensus.HeaderConsensusHash(candidates[0].Header)
	votes := make([]v2consensus.Vote, 0, quorum)
	for i := 0; i < quorum; i++ {
		if err := node.VerifyProposalAgainstCandidate(candidates[i], proposal, c.validators); err != nil {
			return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, err
		}
		validator := c.validators.Validators[i]
		vote, err := v2consensus.SignVote(c.validatorKeys[validator.ID], c.network, height, 0, headerHash)
		if err != nil {
			return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, err
		}
		votes = append(votes, vote)
	}
	certificate, err := c.validators.BuildCertificate(proposal, votes)
	if err != nil {
		return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, err
	}
	// Each replica verifies the same QC and commits its own independent
	// state; join before observing any committed header or issuing imports.
	headers := make([]sharding.GlobalHeader, len(c.runtimes))
	commitErrors := make([]error, len(c.runtimes))
	var commits sync.WaitGroup
	for i, runtimeNode := range c.runtimes {
		commits.Add(1)
		go func(i int, runtimeNode *node.Runtime) {
			defer commits.Done()
			headers[i], commitErrors[i] = runtimeNode.Commit(candidates[i], certificate, c.validators)
		}(i, runtimeNode)
	}
	commits.Wait()
	finalized := headers[0]
	for i, err := range commitErrors {
		if err != nil {
			return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, err
		}
		if i > 0 && headers[i].Hash() != finalized.Hash() {
			return nil, sharding.GlobalHeader{}, v2consensus.Certificate{}, ErrInvalidFinalityBenchmark
		}
	}
	return candidates, finalized, certificate, nil
}

func finalizedReceiptImports(
	candidate node.Candidate,
	finalized sharding.GlobalHeader,
	certificate v2consensus.Certificate,
	validators v2consensus.ValidatorSet,
) (map[uint32][]node.ReceiptImport, []sharding.CrossShardReceipt, error) {
	imports := make(map[uint32][]node.ReceiptImport)
	all := make([]sharding.CrossShardReceipt, 0)
	for _, sourceCommitment := range candidate.Commitments {
		sourceShard := sourceCommitment.ShardID
		receipts := candidate.Receipts[sourceShard]
		if len(receipts) == 0 {
			continue
		}
		commitment, commitmentProof, err := sharding.CommitmentProof(candidate.Commitments, sourceShard)
		if err != nil {
			return nil, nil, err
		}
		batch := sharding.ReceiptBatch{Receipts: receipts}
		for _, receipt := range receipts {
			receiptProof, err := batch.Proof(receipt)
			if err != nil {
				return nil, nil, err
			}
			imports[receipt.DestinationShard] = append(imports[receipt.DestinationShard], node.ReceiptImport{
				Header: finalized, Certificate: certificate, Validators: validators,
				Commitment: commitment, CommitmentProof: commitmentProof,
				Receipt: receipt, ReceiptProof: receiptProof,
			})
			all = append(all, receipt)
		}
	}
	return imports, all, nil
}

func verifyImportedReceipts(cluster *finalityHarnessCluster, receipts []sharding.CrossShardReceipt) error {
	if cluster == nil || len(cluster.stores) == 0 {
		return ErrInvalidFinalityBenchmark
	}
	for _, receipt := range receipts {
		destination, err := receipt.DestinationObject()
		if err != nil {
			return err
		}
		marker, err := sharding.ReceiptMarker(receipt)
		if err != nil {
			return err
		}
		store := cluster.stores[0][receipt.DestinationShard]
		if store == nil {
			return ErrInvalidFinalityBenchmark
		}
		if _, ok := store.GetObject(destination.ID); !ok {
			return ErrInvalidFinalityBenchmark
		}
		if _, ok := store.GetObject(marker.ID); !ok {
			return ErrInvalidFinalityBenchmark
		}
	}
	return nil
}

func countPendingImports(imports map[uint32][]node.ReceiptImport) int {
	total := 0
	for _, values := range imports {
		total += len(values)
	}
	return total
}

func deterministicValidators(
	network types.NetworkID,
	count int,
) (v2consensus.ValidatorSet, map[types.ValidatorID]*ecdsa.PrivateKey, error) {
	if count < 1 {
		return v2consensus.ValidatorSet{}, nil, ErrFinalityHarnessConfig
	}
	set := v2consensus.ValidatorSet{Network: network, Validators: make([]v2consensus.Validator, 0, count)}
	keys := make(map[types.ValidatorID]*ecdsa.PrivateKey, count)
	for i := 0; i < count; i++ {
		key, err := deterministicP256Key("finality-harness-validator", uint64(i))
		if err != nil {
			return v2consensus.ValidatorSet{}, nil, err
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		id := types.ValidatorIDFromPublicKey(pub)
		if _, duplicate := keys[id]; duplicate {
			return v2consensus.ValidatorSet{}, nil, ErrInvalidFinalityBenchmark
		}
		set.Validators = append(set.Validators, v2consensus.Validator{ID: id, PublicKey: pub, Power: 10_000})
		keys[id] = key
	}
	return set, keys, nil
}

func deterministicAccountOnShard(
	domain string,
	seed uint64,
	shard uint32,
	shardCount uint32,
) (*ecdsa.PrivateKey, types.AccountID, error) {
	if shardCount == 0 || shard >= shardCount {
		return nil, types.AccountID{}, ErrFinalityHarnessConfig
	}
	for attempt := uint64(0); attempt < 1_000_000; attempt++ {
		key, err := deterministicP256Key(domain, seed*1_000_003+attempt)
		if err != nil {
			return nil, types.AccountID{}, err
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		account := types.AccountIDFromPublicKey(pub)
		if types.AccountShard(account, shardCount) == shard {
			return key, account, nil
		}
	}
	return nil, types.AccountID{}, fmt.Errorf("%w: could not derive account for shard %d/%d", ErrFinalityHarnessConfig, shard, shardCount)
}

func deterministicP256Key(domain string, index uint64) (*ecdsa.PrivateKey, error) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], index)
	hash := types.HashBytes(domain, raw[:])
	curve := elliptic.P256()
	n := curve.Params().N
	d := new(big.Int).SetBytes(hash[:])
	d.Mod(d, new(big.Int).Sub(n, big.NewInt(1)))
	d.Add(d, big.NewInt(1))
	x, y := curve.ScalarBaseMult(d.Bytes())
	if x == nil || y == nil {
		return nil, ErrFinalityHarnessConfig
	}
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: d}, nil
}

func maxUint32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
