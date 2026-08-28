package economics

import (
	"math/big"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	collectorCheckpointVersion       uint16 = 2
	collectorCheckpointVersionLegacy uint16 = 1
	maxCheckpointVerifiedWork               = 1_000_000
	maxVelocityAccumulatorBytes             = 128
	maxIdleCapitalCheckpointBytes           = 48 << 20
)

// CheckpointBytes serializes the exact mid-epoch collector state. The workload
// registry itself is not serialized; only its commitment is stored so restore
// must be supplied the intended registry explicitly.
func (c *EpochCollector) CheckpointBytes() ([]byte, error) {
	if c == nil || c.config.Epoch == 0 || c.config.ShardCount == 0 || len(c.verifiedWork) > maxCheckpointVerifiedWork {
		return nil, ErrFinalizedEconomics
	}
	registryHash, err := collectorRegistryHash(c.config.WorkRegistry)
	if err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(collectorCheckpointVersion)
	w.U64(c.config.Epoch)
	w.U32(c.config.ShardCount)
	w.Fixed(c.config.NativeToken[:])
	w.Fixed(registryHash[:])
	w.U64(c.config.VelocityPolicy.MinAgeBlocks)
	w.U64(c.config.VelocityPolicy.FullWeightAgeBlocks)
	w.U32(c.config.VelocityPolicy.MaxVelocityBps)
	w.U32(c.config.FeePolicy.BurnBps)
	w.U32(c.config.FeePolicy.ValidatorBps)
	w.U32(c.config.FeePolicy.ReserveBps)
	w.U64(c.lastHeight)

	for shard := uint32(0); shard < c.config.ShardCount; shard++ {
		acc := c.shards[shard]
		if acc == nil || acc.velocity == nil || c.config.ResourceCapacityPerBlock[shard] == 0 {
			return nil, ErrFinalizedEconomics
		}
		w.U64(c.config.ResourceCapacityPerBlock[shard])
		w.U64(c.supply[shard])
		w.U64(acc.fees.Total)
		w.U64(acc.fees.Burn)
		w.U64(acc.fees.Validators)
		w.U64(acc.fees.Reserve)
		w.U64(acc.operations)
		w.U64(acc.resourceUsed)
		w.U64(acc.resourceCapacity)
		w.U64(acc.openingBacklog)
		w.U64(acc.newDemand)
		w.U64(acc.fulfilled)
		w.U64(acc.expired)
		w.U64(acc.closingBacklog)
		w.U64(acc.computeSupply)
		w.Bool(acc.computeSupplyReliable)
		weighted := acc.velocity.weightedValueBps.Bytes()
		if len(weighted) > maxVelocityAccumulatorBytes {
			return nil, ErrFinalizedEconomics
		}
		w.Bytes(weighted)
		w.U64(acc.velocity.observedSpends)
		w.U64(acc.velocity.eligibleSpends)
		w.U64(acc.velocity.unknownAgeSpends)
		w.U64(acc.velocity.freshSpends)
	}

	w.U32(uint32(len(c.verifiedWork)))
	for _, observation := range c.verifiedWork {
		if err := writeVerifiedWork(&w, observation); err != nil {
			return nil, err
		}
	}
	w.Bool(c.idleCapital != nil)
	if c.idleCapital != nil {
		idleRaw, err := c.idleCapital.CheckpointBytes()
		if err != nil || len(idleRaw) > maxIdleCapitalCheckpointBytes {
			return nil, ErrFinalizedEconomics
		}
		w.Bytes(idleRaw)
	}
	return w.BytesCopy(), nil
}

func RestoreEpochCollector(data []byte, registry *compute.WorkRegistry) (*EpochCollector, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || (version != collectorCheckpointVersionLegacy && version != collectorCheckpointVersion) {
		return nil, ErrFinalizedEconomics
	}
	epoch, err := r.U64()
	if err != nil || epoch == 0 {
		return nil, ErrFinalizedEconomics
	}
	shardCount, err := r.U32()
	if err != nil || shardCount == 0 || shardCount > 1_000_000 {
		return nil, ErrFinalizedEconomics
	}
	nativeRaw, err := r.Fixed(32)
	if err != nil {
		return nil, ErrFinalizedEconomics
	}
	registryRaw, err := r.Fixed(32)
	if err != nil {
		return nil, ErrFinalizedEconomics
	}
	var native types.TokenID
	var expectedRegistryHash types.Hash
	copy(native[:], nativeRaw)
	copy(expectedRegistryHash[:], registryRaw)
	if types.IsZero32([32]byte(native)) {
		return nil, ErrFinalizedEconomics
	}
	actualRegistryHash, err := collectorRegistryHash(registry)
	if err != nil || actualRegistryHash != expectedRegistryHash {
		return nil, ErrFinalizedEconomics
	}
	velocityPolicy := VelocityPolicy{}
	velocityPolicy.MinAgeBlocks, err = r.U64()
	if err != nil {
		return nil, ErrFinalizedEconomics
	}
	velocityPolicy.FullWeightAgeBlocks, err = r.U64()
	if err != nil {
		return nil, ErrFinalizedEconomics
	}
	velocityPolicy.MaxVelocityBps, err = r.U32()
	if err != nil {
		return nil, ErrFinalizedEconomics
	}
	if _, err := NewVelocityAccumulator(velocityPolicy); err != nil {
		return nil, err
	}
	feePolicy := FeePolicy{}
	feePolicy.BurnBps, err = r.U32()
	if err != nil {
		return nil, ErrFinalizedEconomics
	}
	feePolicy.ValidatorBps, err = r.U32()
	if err != nil {
		return nil, ErrFinalizedEconomics
	}
	feePolicy.ReserveBps, err = r.U32()
	if err != nil || feePolicy != CompatibilityFeePolicy() {
		return nil, ErrFinalizedEconomics
	}
	lastHeight, err := r.U64()
	if err != nil {
		return nil, ErrFinalizedEconomics
	}

	capacity := make(map[uint32]uint64, shardCount)
	supply := make(map[uint32]uint64, shardCount)
	shards := make(map[uint32]*shardEpochAccumulator, shardCount)
	for shard := uint32(0); shard < shardCount; shard++ {
		perBlock, err := r.U64()
		if err != nil || perBlock == 0 {
			return nil, ErrFinalizedEconomics
		}
		capacity[shard] = perBlock
		supply[shard], err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc := &shardEpochAccumulator{}
		acc.fees.Total, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.fees.Burn, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.fees.Validators, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.fees.Reserve, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.operations, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.resourceUsed, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.resourceCapacity, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.openingBacklog, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.newDemand, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.fulfilled, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.expired, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.closingBacklog, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.computeSupply, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.computeSupplyReliable, err = r.Bool()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		weighted, err := r.Bytes(maxVelocityAccumulatorBytes)
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		velocity, err := NewVelocityAccumulator(velocityPolicy)
		if err != nil {
			return nil, err
		}
		velocity.weightedValueBps.SetBytes(weighted)
		velocity.observedSpends, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		velocity.eligibleSpends, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		velocity.unknownAgeSpends, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		velocity.freshSpends, err = r.U64()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		acc.velocity = velocity
		if err := validateRestoredAccumulator(acc); err != nil {
			return nil, err
		}
		shards[shard] = acc
	}

	verifiedCount, err := r.U32()
	if err != nil || verifiedCount > maxCheckpointVerifiedWork {
		return nil, ErrFinalizedEconomics
	}
	verified := make([]compute.VerifiedWork, int(verifiedCount))
	for i := range verified {
		verified[i], err = readVerifiedWork(r)
		if err != nil {
			return nil, err
		}
	}
	var idleCapital *IdleCapitalTracker
	if version == collectorCheckpointVersion {
		hasIdleCapital, err := r.Bool()
		if err != nil {
			return nil, ErrFinalizedEconomics
		}
		if hasIdleCapital {
			idleRaw, err := r.Bytes(maxIdleCapitalCheckpointBytes)
			if err != nil {
				return nil, ErrFinalizedEconomics
			}
			idleCapital, err = RestoreIdleCapitalTracker(idleRaw)
			if err != nil {
				return nil, err
			}
		}
	}
	if r.Done() != nil {
		return nil, ErrFinalizedEconomics
	}
	ownedRegistry, err := cloneCollectorRegistry(registry)
	if err != nil {
		return nil, err
	}
	return &EpochCollector{
		config: EpochCollectorConfig{
			Epoch: epoch, ShardCount: shardCount, NativeToken: native,
			InitialCirculatingSupply: copyShardMap(supply),
			OpeningComputeBacklog:    make(map[uint32]uint64, shardCount),
			ResourceCapacityPerBlock: capacity,
			VelocityPolicy:           velocityPolicy,
			FeePolicy:                feePolicy,
			WorkRegistry:             ownedRegistry,
		},
		shards: shards, supply: supply, verifiedWork: verified, idleCapital: idleCapital, lastHeight: lastHeight,
	}, nil
}

func collectorRegistryHash(registry *compute.WorkRegistry) (types.Hash, error) {
	if registry == nil {
		return types.Hash{}, nil
	}
	return registry.Hash()
}

func cloneCollectorRegistry(registry *compute.WorkRegistry) (*compute.WorkRegistry, error) {
	if registry == nil {
		return nil, nil
	}
	return registry.Clone()
}

func validateRestoredAccumulator(acc *shardEpochAccumulator) error {
	if acc == nil || acc.velocity == nil || acc.resourceUsed > acc.resourceCapacity ||
		acc.fees.Total != acc.fees.Burn+acc.fees.Validators+acc.fees.Reserve ||
		!validComputeFlow(acc.openingBacklog, acc.newDemand, acc.fulfilled, acc.expired, acc.closingBacklog) ||
		(acc.computeSupplyReliable && acc.fulfilled > acc.computeSupply) ||
		acc.velocity.eligibleSpends > acc.velocity.observedSpends ||
		acc.velocity.unknownAgeSpends > acc.velocity.observedSpends ||
		acc.velocity.freshSpends > acc.velocity.observedSpends {
		return ErrFinalizedEconomics
	}
	return nil
}

func writeVerifiedWork(w *codec.Writer, observation compute.VerifiedWork) error {
	if w == nil || types.IsZero32([32]byte(observation.JobID)) || observation.Class <= compute.WorkUnknown ||
		observation.Class >= compute.WorkClassCount || observation.Units == 0 || observation.Vector.IsZero() || observation.PaidZPH == 0 ||
		observation.Verification <= compute.VerificationUnknown || observation.Verification > compute.VerificationHybrid ||
		types.IsZero32([32]byte(observation.ResultRoot)) {
		return ErrFinalizedEconomics
	}
	w.Fixed(observation.JobID[:])
	w.U8(uint8(observation.Class))
	w.U64(observation.Units)
	writeWorkVector(w, observation.Vector)
	w.U64(observation.PaidZPH)
	w.U8(uint8(observation.Verification))
	w.Fixed(observation.ResultRoot[:])
	return nil
}

func readVerifiedWork(r *codec.Reader) (compute.VerifiedWork, error) {
	jobRaw, err := r.Fixed(32)
	if err != nil {
		return compute.VerifiedWork{}, ErrFinalizedEconomics
	}
	class, err := r.U8()
	if err != nil {
		return compute.VerifiedWork{}, ErrFinalizedEconomics
	}
	units, err := r.U64()
	if err != nil {
		return compute.VerifiedWork{}, ErrFinalizedEconomics
	}
	vector, err := readWorkVector(r)
	if err != nil {
		return compute.VerifiedWork{}, err
	}
	paid, err := r.U64()
	if err != nil {
		return compute.VerifiedWork{}, ErrFinalizedEconomics
	}
	verification, err := r.U8()
	if err != nil {
		return compute.VerifiedWork{}, ErrFinalizedEconomics
	}
	rootRaw, err := r.Fixed(32)
	if err != nil {
		return compute.VerifiedWork{}, ErrFinalizedEconomics
	}
	var jobID types.JobID
	var resultRoot types.Hash
	copy(jobID[:], jobRaw)
	copy(resultRoot[:], rootRaw)
	out := compute.VerifiedWork{
		JobID: jobID, Class: compute.WorkClass(class), Units: units, Vector: vector,
		PaidZPH: paid, Verification: compute.VerificationMode(verification), ResultRoot: resultRoot,
	}
	var sink codec.Writer
	if err := writeVerifiedWork(&sink, out); err != nil {
		return compute.VerifiedWork{}, err
	}
	return out, nil
}

func writeWorkVector(w *codec.Writer, vector compute.WorkVector) {
	w.U64(vector.CPUUnits)
	w.U64(vector.GPUFP32Units)
	w.U64(vector.GPUFP64Units)
	w.U64(vector.TensorUnits)
	w.U64(vector.MemoryByteSeconds)
	w.U64(vector.VRAMByteSeconds)
	w.U64(vector.StorageBytes)
	w.U64(vector.NetworkBytes)
}

func readWorkVector(r *codec.Reader) (compute.WorkVector, error) {
	values := make([]uint64, 8)
	var err error
	for i := range values {
		values[i], err = r.U64()
		if err != nil {
			return compute.WorkVector{}, ErrFinalizedEconomics
		}
	}
	return compute.WorkVector{
		CPUUnits: values[0], GPUFP32Units: values[1], GPUFP64Units: values[2], TensorUnits: values[3],
		MemoryByteSeconds: values[4], VRAMByteSeconds: values[5], StorageBytes: values[6], NetworkBytes: values[7],
	}, nil
}

func copyShardMap(source map[uint32]uint64) map[uint32]uint64 {
	out := make(map[uint32]uint64, len(source))
	for shard, value := range source {
		out[shard] = value
	}
	return out
}

var _ = big.NewInt
