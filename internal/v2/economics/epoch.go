package economics

import (
	"errors"
	"math/big"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const EpochMetricsVersion uint16 = 2

var ErrEpochMetrics = errors.New("invalid Zephyr economic epoch metrics")

type ShardEpochMetrics struct {
	Version                   uint16
	Epoch                     uint64
	ShardID                   uint32
	ChargedFees               uint64
	BurnedFees                uint64
	ValidatorFees             uint64
	ReserveFees               uint64
	FinalizedOperations       uint64
	ResourceUsed              uint64
	ResourceCapacity          uint64
	CirculatingNativeSupply   uint64
	AgeWeightedVelocityBps    uint32
	EscrowBackedComputeDemand uint64
	VerifiedComputeSupply     uint64
	ComputeSupplyReliable     bool
	OpeningComputeBacklog     uint64
	ComputeFulfilled          uint64
	ComputeExpired            uint64
	ComputeBacklog            uint64
}

func (m ShardEpochMetrics) Validate() error {
	if m.Version != EpochMetricsVersion || m.Epoch == 0 || m.ResourceCapacity == 0 ||
		m.ResourceUsed > m.ResourceCapacity || m.AgeWeightedVelocityBps > 10*BasisPoints ||
		(m.ComputeSupplyReliable && m.ComputeFulfilled > m.VerifiedComputeSupply) || !validComputeFlow(
			m.OpeningComputeBacklog,
			m.EscrowBackedComputeDemand,
			m.ComputeFulfilled,
			m.ComputeExpired,
			m.ComputeBacklog,
		) {
		return ErrEpochMetrics
	}
	feeTotal := new(big.Int).SetUint64(m.BurnedFees)
	feeTotal.Add(feeTotal, new(big.Int).SetUint64(m.ValidatorFees))
	feeTotal.Add(feeTotal, new(big.Int).SetUint64(m.ReserveFees))
	if !feeTotal.IsUint64() || feeTotal.Uint64() != m.ChargedFees {
		return ErrEpochMetrics
	}
	return nil
}

func (m ShardEpochMetrics) CanonicalBytes() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(m.Version)
	w.U64(m.Epoch)
	w.U32(m.ShardID)
	w.U64(m.ChargedFees)
	w.U64(m.BurnedFees)
	w.U64(m.ValidatorFees)
	w.U64(m.ReserveFees)
	w.U64(m.FinalizedOperations)
	w.U64(m.ResourceUsed)
	w.U64(m.ResourceCapacity)
	w.U64(m.CirculatingNativeSupply)
	w.U32(m.AgeWeightedVelocityBps)
	w.U64(m.EscrowBackedComputeDemand)
	w.U64(m.VerifiedComputeSupply)
	w.Bool(m.ComputeSupplyReliable)
	w.U64(m.OpeningComputeBacklog)
	w.U64(m.ComputeFulfilled)
	w.U64(m.ComputeExpired)
	w.U64(m.ComputeBacklog)
	return w.BytesCopy(), nil
}

func (m ShardEpochMetrics) Hash() (types.Hash, error) {
	raw, err := m.CanonicalBytes()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/shard-economics/v2", raw)), nil
}

type EpochAggregate struct {
	Epoch                     uint64
	ShardCount                uint32
	ChargedFees               uint64
	BurnedFees                uint64
	ValidatorFees             uint64
	ReserveFees               uint64
	FinalizedOperations       uint64
	ResourceUsed              uint64
	ResourceCapacity          uint64
	ResourceUtilizationBps    uint32
	CirculatingNativeSupply   uint64
	AgeWeightedVelocityBps    uint32
	EscrowBackedComputeDemand uint64
	VerifiedComputeSupply     uint64
	ComputeSupplyReliable     bool
	OpeningComputeBacklog     uint64
	ComputeFulfilled          uint64
	ComputeExpired            uint64
	ComputeBacklog            uint64
	ComputeUtilizationBps     uint32
}

func AggregateEpochMetrics(metrics []ShardEpochMetrics) (EpochAggregate, error) {
	if len(metrics) == 0 {
		return EpochAggregate{}, ErrEpochMetrics
	}
	ordered := append([]ShardEpochMetrics(nil), metrics...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ShardID < ordered[j].ShardID })
	epoch := ordered[0].Epoch
	var charged, burned, validators, reserve, operations, used, capacity, circulating big.Int
	var demand, supply, openingBacklog, fulfilled, expired, backlog, weightedVelocity big.Int
	supplyReliable := true
	for i, metric := range ordered {
		if err := metric.Validate(); err != nil || metric.Epoch != epoch || (i > 0 && ordered[i-1].ShardID == metric.ShardID) {
			return EpochAggregate{}, ErrEpochMetrics
		}
		addBig(&charged, metric.ChargedFees)
		addBig(&burned, metric.BurnedFees)
		addBig(&validators, metric.ValidatorFees)
		addBig(&reserve, metric.ReserveFees)
		addBig(&operations, metric.FinalizedOperations)
		addBig(&used, metric.ResourceUsed)
		addBig(&capacity, metric.ResourceCapacity)
		addBig(&circulating, metric.CirculatingNativeSupply)
		addBig(&demand, metric.EscrowBackedComputeDemand)
		addBig(&supply, metric.VerifiedComputeSupply)
		addBig(&openingBacklog, metric.OpeningComputeBacklog)
		addBig(&fulfilled, metric.ComputeFulfilled)
		addBig(&expired, metric.ComputeExpired)
		addBig(&backlog, metric.ComputeBacklog)
		supplyReliable = supplyReliable && metric.ComputeSupplyReliable
		term := new(big.Int).Mul(new(big.Int).SetUint64(metric.CirculatingNativeSupply), new(big.Int).SetUint64(uint64(metric.AgeWeightedVelocityBps)))
		weightedVelocity.Add(&weightedVelocity, term)
	}
	values := []*big.Int{
		&charged, &burned, &validators, &reserve, &operations, &used, &capacity, &circulating,
		&demand, &supply, &openingBacklog, &fulfilled, &expired, &backlog,
	}
	for _, value := range values {
		if !value.IsUint64() {
			return EpochAggregate{}, ErrEpochMetrics
		}
	}
	out := EpochAggregate{
		Epoch:                     epoch,
		ShardCount:                uint32(len(ordered)),
		ChargedFees:               charged.Uint64(),
		BurnedFees:                burned.Uint64(),
		ValidatorFees:             validators.Uint64(),
		ReserveFees:               reserve.Uint64(),
		FinalizedOperations:       operations.Uint64(),
		ResourceUsed:              used.Uint64(),
		ResourceCapacity:          capacity.Uint64(),
		CirculatingNativeSupply:   circulating.Uint64(),
		EscrowBackedComputeDemand: demand.Uint64(),
		VerifiedComputeSupply:     supply.Uint64(),
		ComputeSupplyReliable:     supplyReliable,
		OpeningComputeBacklog:     openingBacklog.Uint64(),
		ComputeFulfilled:          fulfilled.Uint64(),
		ComputeExpired:            expired.Uint64(),
		ComputeBacklog:            backlog.Uint64(),
	}
	if out.ResourceCapacity == 0 || !validComputeFlow(
		out.OpeningComputeBacklog,
		out.EscrowBackedComputeDemand,
		out.ComputeFulfilled,
		out.ComputeExpired,
		out.ComputeBacklog,
	) {
		return EpochAggregate{}, ErrEpochMetrics
	}
	if out.ComputeSupplyReliable && out.ComputeFulfilled > out.VerifiedComputeSupply {
		return EpochAggregate{}, ErrEpochMetrics
	}
	out.ResourceUtilizationBps = ratioBps(out.ResourceUsed, out.ResourceCapacity)
	if out.VerifiedComputeSupply != 0 {
		out.ComputeUtilizationBps = ratioBps(out.ComputeFulfilled, out.VerifiedComputeSupply)
	}
	if out.CirculatingNativeSupply != 0 {
		weightedVelocity.Quo(&weightedVelocity, new(big.Int).SetUint64(out.CirculatingNativeSupply))
		if !weightedVelocity.IsUint64() || weightedVelocity.Uint64() > uint64(10*BasisPoints) {
			return EpochAggregate{}, ErrEpochMetrics
		}
		out.AgeWeightedVelocityBps = uint32(weightedVelocity.Uint64())
	}
	return out, nil
}

func (a EpochAggregate) MonetaryMetrics(totalSupply, stakedSupply, protocolReserve, computeIndexQ9 uint64, computePriceTrendBps int32, computeIndexReliable bool) (MonetaryMetrics, error) {
	if totalSupply == 0 || a.CirculatingNativeSupply == 0 || a.CirculatingNativeSupply > totalSupply || stakedSupply > a.CirculatingNativeSupply || protocolReserve > totalSupply {
		return MonetaryMetrics{}, ErrEpochMetrics
	}
	return MonetaryMetrics{
		Supply:                 totalSupply,
		CirculatingSupply:      a.CirculatingNativeSupply,
		StakedSupply:           stakedSupply,
		ProtocolReserve:        protocolReserve,
		BurnedThisEpoch:        a.BurnedFees,
		FinalizedOperations:    a.FinalizedOperations,
		ResourceUtilizationBps: a.ResourceUtilizationBps,
		AgeWeightedVelocityBps: a.AgeWeightedVelocityBps,
		ComputeIndexQ9:         computeIndexQ9,
		ComputePriceTrendBps:   computePriceTrendBps,
		ComputeIndexReliable:   computeIndexReliable,
	}, nil
}

func (a EpochAggregate) ComputeMarketMetrics(computePriceTrendBps int32, computeIndexReliable bool) ComputeMarketMetrics {
	return ComputeMarketMetrics{
		EscrowBackedDemandUnits: a.OpeningComputeBacklog + a.EscrowBackedComputeDemand,
		VerifiedSupplyUnits:     a.VerifiedComputeSupply,
		VerifiedSupplyReliable:  a.ComputeSupplyReliable,
		BacklogUnits:            a.ComputeBacklog,
		FulfilledUnits:          a.ComputeFulfilled,
		UtilizationBps:          a.ComputeUtilizationBps,
		ComputePriceTrendBps:    computePriceTrendBps,
		ComputeIndexReliable:    computeIndexReliable,
	}
}

func validComputeFlow(opening, demand, fulfilled, expired, closing uint64) bool {
	available := new(big.Int).Add(new(big.Int).SetUint64(opening), new(big.Int).SetUint64(demand))
	resolved := new(big.Int).SetUint64(fulfilled)
	resolved.Add(resolved, new(big.Int).SetUint64(expired))
	resolved.Add(resolved, new(big.Int).SetUint64(closing))
	return available.IsUint64() && resolved.IsUint64() && available.Cmp(resolved) == 0
}

func addBig(target *big.Int, value uint64) {
	target.Add(target, new(big.Int).SetUint64(value))
}
