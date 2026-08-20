package economics

import (
	"errors"
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrFinalizedEconomics = errors.New("invalid finalized Zephyr economic observation")

// EpochCollectorConfig contains only deterministic protocol/shadow-policy
// inputs. Initial circulating supply and opening backlog are prior finalized
// state, not mutable operator telemetry.
type EpochCollectorConfig struct {
	Epoch                    uint64
	ShardCount               uint32
	NativeToken              types.TokenID
	InitialCirculatingSupply map[uint32]uint64
	OpeningComputeBacklog    map[uint32]uint64
	ResourceCapacityPerBlock map[uint32]uint64
	VelocityPolicy           VelocityPolicy
	FeePolicy                FeePolicy
	WorkRegistry             *compute.WorkRegistry
}

type FinalizedShardObservation struct {
	Transactions            []tx.Transaction
	Results                 []execution.Result
	Imports                 []sharding.CrossShardReceipt
	DataBytes               uint64
	ComputeCapacityUnits    uint64
	ComputeCapacityReliable bool
}

type shardEpochAccumulator struct {
	fees                  FeeAllocation
	operations            uint64
	resourceUsed          uint64
	resourceCapacity      uint64
	openingBacklog        uint64
	newDemand             uint64
	fulfilled             uint64
	expired               uint64
	closingBacklog        uint64
	computeSupply         uint64
	computeSupplyReliable bool
	velocity              *VelocityAccumulator
}

type EpochCollector struct {
	config       EpochCollectorConfig
	shards       map[uint32]*shardEpochAccumulator
	supply       map[uint32]uint64
	verifiedWork []compute.VerifiedWork
	lastHeight   uint64
}

func NewEpochCollector(config EpochCollectorConfig) (*EpochCollector, error) {
	if config.Epoch == 0 || config.ShardCount == 0 || types.IsZero32([32]byte(config.NativeToken)) ||
		config.FeePolicy != CompatibilityFeePolicy() {
		return nil, ErrFinalizedEconomics
	}
	if _, err := NewVelocityAccumulator(config.VelocityPolicy); err != nil {
		return nil, err
	}
	collector := &EpochCollector{
		config: config,
		shards: make(map[uint32]*shardEpochAccumulator, config.ShardCount),
		supply: make(map[uint32]uint64, config.ShardCount),
	}
	for shard := uint32(0); shard < config.ShardCount; shard++ {
		capacity := config.ResourceCapacityPerBlock[shard]
		if capacity == 0 {
			return nil, ErrFinalizedEconomics
		}
		velocity, err := NewVelocityAccumulator(config.VelocityPolicy)
		if err != nil {
			return nil, err
		}
		opening := config.OpeningComputeBacklog[shard]
		collector.shards[shard] = &shardEpochAccumulator{
			openingBacklog:        opening,
			closingBacklog:        opening,
			computeSupplyReliable: true,
			velocity:              velocity,
		}
		collector.supply[shard] = config.InitialCirculatingSupply[shard]
	}
	return collector, nil
}

// ObserveFinalizedBlock applies a full global-block observation atomically.
// Telemetry changes only after normal consensus finality and never mutates
// world state or mints ZPH.
func (c *EpochCollector) ObserveFinalizedBlock(height uint64, observations map[uint32]FinalizedShardObservation) error {
	if c == nil {
		return ErrFinalizedEconomics
	}
	preview := c.clone()
	if preview == nil {
		return ErrFinalizedEconomics
	}
	if err := preview.observeFinalizedBlock(height, observations); err != nil {
		return err
	}
	*c = *preview
	return nil
}

func (c *EpochCollector) observeFinalizedBlock(height uint64, observations map[uint32]FinalizedShardObservation) error {
	if height == 0 || height <= c.lastHeight {
		return ErrFinalizedEconomics
	}
	for shard := uint32(0); shard < c.config.ShardCount; shard++ {
		observation := observations[shard]
		if len(observation.Transactions) != len(observation.Results) {
			return ErrFinalizedEconomics
		}
		acc := c.shards[shard]
		if err := addTo(&acc.resourceCapacity, c.config.ResourceCapacityPerBlock[shard]); err != nil {
			return err
		}
		if err := addTo(&acc.computeSupply, observation.ComputeCapacityUnits); err != nil {
			return err
		}
		acc.computeSupplyReliable = acc.computeSupplyReliable && observation.ComputeCapacityReliable

		for i := range observation.Transactions {
			transaction := observation.Transactions[i]
			result := observation.Results[i]
			if transaction.ShardID != shard || result.TxID != transaction.ID() {
				return ErrFinalizedEconomics
			}
			if err := c.observeFinalizedTransaction(height, shard, transaction, result, acc); err != nil {
				return err
			}
		}
		for _, receipt := range observation.Imports {
			if receipt.DestinationShard != shard || receipt.SourceShard >= c.config.ShardCount || receipt.SourceShard == receipt.DestinationShard {
				return ErrFinalizedEconomics
			}
			if err := c.observeImportedReceipt(receipt); err != nil {
				return err
			}
			if err := addTo(&acc.operations, 1); err != nil {
				return err
			}
			if err := addTo(&acc.resourceUsed, 2); err != nil {
				return err
			}
		}
		if observation.DataBytes > 0 {
			if observation.DataBytes > math.MaxUint64-1023 {
				return ErrFinalizedEconomics
			}
			dataUnits := (observation.DataBytes + 1023) / 1024
			if err := addTo(&acc.resourceUsed, dataUnits); err != nil {
				return err
			}
		}
		if acc.resourceUsed > acc.resourceCapacity {
			return ErrFinalizedEconomics
		}
	}
	c.lastHeight = height
	return nil
}

func (c *EpochCollector) observeFinalizedTransaction(height uint64, shard uint32, transaction tx.Transaction, result execution.Result, acc *shardEpochAccumulator) error {
	allocation, err := SplitFee(transaction.Fee, c.config.FeePolicy)
	if err != nil || allocation.Validators != 0 || allocation.Reserve != 0 {
		return ErrFinalizedEconomics
	}
	if err := addTo(&acc.fees.Total, allocation.Total); err != nil {
		return err
	}
	if err := addTo(&acc.fees.Burn, allocation.Burn); err != nil {
		return err
	}
	if c.supply[shard] < allocation.Burn {
		return ErrFinalizedEconomics
	}
	c.supply[shard] -= allocation.Burn
	if err := addTo(&acc.operations, uint64(len(transaction.Operations))); err != nil {
		return err
	}
	resourceUnits, err := finalizedTransactionResourceUnits(transaction, result)
	if err != nil {
		return err
	}
	if err := addTo(&acc.resourceUsed, resourceUnits); err != nil {
		return err
	}

	consumed := make(map[types.ObjectID]struct{}, len(result.Consumed))
	for _, id := range result.Consumed {
		consumed[id] = struct{}{}
	}
	for _, witness := range transaction.Witnesses {
		if _, wasConsumed := consumed[witness.Object.ID]; !wasConsumed || witness.Object.Kind != object.KindCoin {
			continue
		}
		coin, err := object.ParseCoin(witness.Object.Data)
		if err != nil {
			return err
		}
		if coin.Token == c.config.NativeToken {
			if err := acc.velocity.ObserveCoin(coin, height); err != nil {
				return err
			}
		}
	}
	return c.observeComputeLifecycle(transaction, result, acc)
}

func finalizedTransactionResourceUnits(transaction tx.Transaction, result execution.Result) (uint64, error) {
	intentBytes := uint64(len(transaction.IntentBytes()))
	units := uint64(1) + (intentBytes+1023)/1024
	parts := []uint64{
		uint64(len(transaction.Inputs)),
		uint64(len(result.Consumed)),
		uint64(len(result.Created)),
		uint64(len(result.Outbound)),
	}
	for _, value := range parts {
		if math.MaxUint64-units < value {
			return 0, ErrFinalizedEconomics
		}
		units += value
	}
	return units, nil
}

func (c *EpochCollector) observeImportedReceipt(receipt sharding.CrossShardReceipt) error {
	if receipt.Output.Kind != object.KindCoin {
		return nil
	}
	coin, err := object.ParseCoin(receipt.Output.Data)
	if err != nil {
		return err
	}
	if coin.Token != c.config.NativeToken {
		return nil
	}
	if c.supply[receipt.SourceShard] < coin.Amount || math.MaxUint64-c.supply[receipt.DestinationShard] < coin.Amount {
		return ErrFinalizedEconomics
	}
	c.supply[receipt.SourceShard] -= coin.Amount
	c.supply[receipt.DestinationShard] += coin.Amount
	return nil
}

func (c *EpochCollector) observeComputeLifecycle(transaction tx.Transaction, result execution.Result, acc *shardEpochAccumulator) error {
	if c.config.WorkRegistry == nil || len(transaction.Operations) != 1 {
		return nil
	}
	op := transaction.Operations[0]
	switch op.Kind {
	case tx.OpComputeJob:
		job, err := compute.ParseJob(op.Payload)
		if err != nil {
			return err
		}
		spec, ok := c.config.WorkRegistry.Resolve(job.WorkloadHash)
		if !ok {
			return nil
		}
		if err := addTo(&acc.newDemand, spec.Units); err != nil {
			return err
		}
		return addTo(&acc.closingBacklog, spec.Units)
	case tx.OpComputeFinalize, tx.OpComputeResolveReplicated:
		record, ok, err := computeJobWitness(transaction)
		if err != nil || !ok {
			return ErrFinalizedEconomics
		}
		spec, registered := c.config.WorkRegistry.Resolve(record.Job.WorkloadHash)
		if !registered {
			return nil
		}
		receipt, ok, err := settlementReceiptForJob(result.Created, record.ID)
		if err != nil || !ok {
			return ErrFinalizedEconomics
		}
		verified, err := compute.ObserveFinalizedSettlement(record, receipt, c.config.WorkRegistry)
		if err != nil || verified.Units != spec.Units {
			return ErrFinalizedEconomics
		}
		if acc.closingBacklog < spec.Units {
			return ErrFinalizedEconomics
		}
		acc.closingBacklog -= spec.Units
		if err := addTo(&acc.fulfilled, spec.Units); err != nil {
			return err
		}
		c.verifiedWork = append(c.verifiedWork, verified)
	case tx.OpComputeExpire:
		record, ok, err := computeJobWitness(transaction)
		if err != nil || !ok {
			return ErrFinalizedEconomics
		}
		spec, registered := c.config.WorkRegistry.Resolve(record.Job.WorkloadHash)
		if !registered {
			return nil
		}
		if acc.closingBacklog < spec.Units {
			return ErrFinalizedEconomics
		}
		acc.closingBacklog -= spec.Units
		return addTo(&acc.expired, spec.Units)
	}
	return nil
}

func computeJobWitness(transaction tx.Transaction) (compute.OnChainJob, bool, error) {
	var record compute.OnChainJob
	found := false
	for _, witness := range transaction.Witnesses {
		if witness.Object.Kind != object.KindComputeJob {
			continue
		}
		if found {
			return compute.OnChainJob{}, false, ErrFinalizedEconomics
		}
		parsed, err := compute.ParseOnChainJob(witness.Object.Data)
		if err != nil {
			return compute.OnChainJob{}, false, err
		}
		record, found = parsed, true
	}
	return record, found, nil
}

func settlementReceiptForJob(created []object.Object, jobID types.JobID) (compute.SettlementReceipt, bool, error) {
	for _, createdObject := range created {
		if createdObject.Kind != object.KindSystem {
			continue
		}
		receipt, err := compute.ParseSettlementReceipt(createdObject.Data)
		if err != nil {
			continue
		}
		if receipt.JobID == jobID {
			return receipt, true, nil
		}
	}
	return compute.SettlementReceipt{}, false, nil
}

func (c *EpochCollector) FinalizeEpoch() ([]ShardEpochMetrics, []compute.VerifiedWork, error) {
	if c == nil {
		return nil, nil, ErrFinalizedEconomics
	}
	metrics := make([]ShardEpochMetrics, 0, c.config.ShardCount)
	for shard := uint32(0); shard < c.config.ShardCount; shard++ {
		acc := c.shards[shard]
		velocity := VelocitySnapshot{}
		if c.supply[shard] > 0 {
			var err error
			velocity, err = acc.velocity.Finalize(c.supply[shard])
			if err != nil {
				return nil, nil, err
			}
		}
		metric := ShardEpochMetrics{
			Version: EpochMetricsVersion, Epoch: c.config.Epoch, ShardID: shard,
			ChargedFees: acc.fees.Total, BurnedFees: acc.fees.Burn,
			ValidatorFees: acc.fees.Validators, ReserveFees: acc.fees.Reserve,
			FinalizedOperations: acc.operations, ResourceUsed: acc.resourceUsed, ResourceCapacity: acc.resourceCapacity,
			CirculatingNativeSupply: c.supply[shard], AgeWeightedVelocityBps: velocity.AgeWeightedVelocityBps,
			EscrowBackedComputeDemand: acc.newDemand, VerifiedComputeSupply: acc.computeSupply,
			ComputeSupplyReliable: acc.computeSupplyReliable, OpeningComputeBacklog: acc.openingBacklog,
			ComputeFulfilled: acc.fulfilled, ComputeExpired: acc.expired, ComputeBacklog: acc.closingBacklog,
		}
		if err := metric.Validate(); err != nil {
			return nil, nil, err
		}
		metrics = append(metrics, metric)
	}
	return metrics, append([]compute.VerifiedWork(nil), c.verifiedWork...), nil
}

func (c *EpochCollector) AdvanceEpoch(next uint64) error {
	if c == nil || next != c.config.Epoch+1 {
		return ErrFinalizedEconomics
	}
	c.config.Epoch = next
	c.verifiedWork = nil
	for shard := uint32(0); shard < c.config.ShardCount; shard++ {
		prior := c.shards[shard]
		velocity, err := NewVelocityAccumulator(c.config.VelocityPolicy)
		if err != nil {
			return err
		}
		c.shards[shard] = &shardEpochAccumulator{
			openingBacklog:        prior.closingBacklog,
			closingBacklog:        prior.closingBacklog,
			computeSupplyReliable: true,
			velocity:              velocity,
		}
	}
	return nil
}

func (c *EpochCollector) CirculatingSupply(shard uint32) (uint64, bool) {
	if c == nil || shard >= c.config.ShardCount {
		return 0, false
	}
	return c.supply[shard], true
}

func (c *EpochCollector) clone() *EpochCollector {
	if c == nil {
		return nil
	}
	out := &EpochCollector{
		config:       c.config,
		shards:       make(map[uint32]*shardEpochAccumulator, len(c.shards)),
		supply:       make(map[uint32]uint64, len(c.supply)),
		verifiedWork: append([]compute.VerifiedWork(nil), c.verifiedWork...),
		lastHeight:   c.lastHeight,
	}
	for shard, amount := range c.supply {
		out.supply[shard] = amount
	}
	for shard, source := range c.shards {
		copyAccumulator := *source
		if source.velocity != nil {
			velocityCopy := *source.velocity
			velocityCopy.weightedValueBps.Set(&source.velocity.weightedValueBps)
			copyAccumulator.velocity = &velocityCopy
		}
		out.shards[shard] = &copyAccumulator
	}
	return out
}

func addTo(target *uint64, value uint64) error {
	if target == nil || math.MaxUint64-*target < value {
		return ErrFinalizedEconomics
	}
	*target += value
	return nil
}
