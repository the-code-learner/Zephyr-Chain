package economics

import (
	"bytes"
	"errors"
	"math"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const ZECIShadowVersion uint16 = 1

var ErrZECI = errors.New("invalid ZECI shadow telemetry")

// ZECIShadowTelemetry is an evidence envelope, not an activation score.
// Version 1 intentionally exposes independently interpretable finalized
// components rather than embedding an arbitrary composite weighting.
type ZECIShadowTelemetry struct {
	Version                     uint16
	Epoch                       uint64
	Shadow                      bool
	ShardCount                  uint32
	FinalizedOperations         uint64
	ResourceUsed                uint64
	ResourceCapacity            uint64
	ResourceUtilizationBps      uint32
	AgeWeightedVelocityBps      uint32
	VerifiedWorkObservations    uint64
	VerifiedWorkUnits           uint64
	VerifiedWorkPaidZPH         uint64
	ComputeSupplyReliable       bool
	ComputeFulfilled            uint64
	ComputeExpired              uint64
	ComputeBacklog              uint64
	ProductiveCoverageAvailable bool
	ProductiveObservedCapital   uint64
	ProductiveCoverageBps       uint32
	EvidenceHash                types.Hash
}

func (z ZECIShadowTelemetry) Validate() error {
	if z.Version != ZECIShadowVersion || z.Epoch == 0 || !z.Shadow || z.ShardCount == 0 ||
		z.ResourceCapacity == 0 || z.ResourceUsed > z.ResourceCapacity ||
		z.ResourceUtilizationBps > BasisPoints || z.AgeWeightedVelocityBps > 10*BasisPoints ||
		z.ProductiveCoverageBps > BasisPoints || types.IsZero32([32]byte(z.EvidenceHash)) {
		return ErrZECI
	}
	if !z.ProductiveCoverageAvailable && (z.ProductiveObservedCapital != 0 || z.ProductiveCoverageBps != 0) {
		return ErrZECI
	}
	if z.ProductiveCoverageAvailable && z.ProductiveObservedCapital == 0 {
		return ErrZECI
	}
	return nil
}

func (z ZECIShadowTelemetry) CanonicalBytes() ([]byte, error) {
	if err := z.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(z.Version)
	w.U64(z.Epoch)
	w.Bool(z.Shadow)
	w.U32(z.ShardCount)
	w.U64(z.FinalizedOperations)
	w.U64(z.ResourceUsed)
	w.U64(z.ResourceCapacity)
	w.U32(z.ResourceUtilizationBps)
	w.U32(z.AgeWeightedVelocityBps)
	w.U64(z.VerifiedWorkObservations)
	w.U64(z.VerifiedWorkUnits)
	w.U64(z.VerifiedWorkPaidZPH)
	w.Bool(z.ComputeSupplyReliable)
	w.U64(z.ComputeFulfilled)
	w.U64(z.ComputeExpired)
	w.U64(z.ComputeBacklog)
	w.Bool(z.ProductiveCoverageAvailable)
	w.U64(z.ProductiveObservedCapital)
	w.U32(z.ProductiveCoverageBps)
	w.Fixed(z.EvidenceHash[:])
	return w.BytesCopy(), nil
}

func (z ZECIShadowTelemetry) Hash() (types.Hash, error) {
	raw, err := z.CanonicalBytes()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/zeci-shadow/v1", raw)), nil
}

func BuildZECIShadowTelemetry(metrics []ShardEpochMetrics, verified []compute.VerifiedWork, productive *IdleCapitalSnapshot) (ZECIShadowTelemetry, error) {
	aggregate, err := AggregateEpochMetrics(metrics)
	if err != nil {
		return ZECIShadowTelemetry{}, ErrZECI
	}
	orderedMetrics := append([]ShardEpochMetrics(nil), metrics...)
	sort.Slice(orderedMetrics, func(i, j int) bool { return orderedMetrics[i].ShardID < orderedMetrics[j].ShardID })

	orderedWork := append([]compute.VerifiedWork(nil), verified...)
	sort.Slice(orderedWork, func(i, j int) bool {
		if cmp := bytes.Compare(orderedWork[i].JobID[:], orderedWork[j].JobID[:]); cmp != 0 {
			return cmp < 0
		}
		return bytes.Compare(orderedWork[i].ResultRoot[:], orderedWork[j].ResultRoot[:]) < 0
	})

	var workUnits, paid uint64
	seenJobs := make(map[types.JobID]struct{}, len(orderedWork))
	for _, work := range orderedWork {
		if types.IsZero32([32]byte(work.JobID)) || work.Class <= compute.WorkUnknown || work.Class >= compute.WorkClassCount ||
			work.Units == 0 || work.PaidZPH == 0 || work.Vector.IsZero() || types.IsZero32([32]byte(work.ResultRoot)) {
			return ZECIShadowTelemetry{}, ErrZECI
		}
		if _, duplicate := seenJobs[work.JobID]; duplicate {
			return ZECIShadowTelemetry{}, ErrZECI
		}
		seenJobs[work.JobID] = struct{}{}
		if math.MaxUint64-workUnits < work.Units || math.MaxUint64-paid < work.PaidZPH {
			return ZECIShadowTelemetry{}, ErrZECI
		}
		workUnits += work.Units
		paid += work.PaidZPH
	}

	out := ZECIShadowTelemetry{
		Version:                  ZECIShadowVersion,
		Epoch:                    aggregate.Epoch,
		Shadow:                   true,
		ShardCount:               aggregate.ShardCount,
		FinalizedOperations:      aggregate.FinalizedOperations,
		ResourceUsed:             aggregate.ResourceUsed,
		ResourceCapacity:         aggregate.ResourceCapacity,
		ResourceUtilizationBps:   aggregate.ResourceUtilizationBps,
		AgeWeightedVelocityBps:   aggregate.AgeWeightedVelocityBps,
		VerifiedWorkObservations: uint64(len(orderedWork)),
		VerifiedWorkUnits:        workUnits,
		VerifiedWorkPaidZPH:      paid,
		ComputeSupplyReliable:    aggregate.ComputeSupplyReliable,
		ComputeFulfilled:         aggregate.ComputeFulfilled,
		ComputeExpired:           aggregate.ComputeExpired,
		ComputeBacklog:           aggregate.ComputeBacklog,
	}
	if productive != nil && productive.ProductiveObservedCapital > 0 {
		if productive.ProductiveCoverageBps > BasisPoints {
			return ZECIShadowTelemetry{}, ErrZECI
		}
		out.ProductiveCoverageAvailable = true
		out.ProductiveObservedCapital = productive.ProductiveObservedCapital
		out.ProductiveCoverageBps = productive.ProductiveCoverageBps
	}

	evidence, err := zeciEvidenceHash(orderedMetrics, orderedWork, productive)
	if err != nil {
		return ZECIShadowTelemetry{}, err
	}
	out.EvidenceHash = evidence
	if err := out.Validate(); err != nil {
		return ZECIShadowTelemetry{}, err
	}
	return out, nil
}

func (c *EpochCollector) ZECIShadowTelemetry(height uint64, dormancyBounds []uint64) (ZECIShadowTelemetry, error) {
	if c == nil {
		return ZECIShadowTelemetry{}, ErrZECI
	}
	metrics, verified, err := c.FinalizeEpoch()
	if err != nil {
		return ZECIShadowTelemetry{}, err
	}
	var productive *IdleCapitalSnapshot
	if c.idleCapital != nil && len(c.idleCapital.allLots()) > 0 {
		snapshot, err := c.idleCapital.Snapshot(height, dormancyBounds)
		if err != nil {
			return ZECIShadowTelemetry{}, err
		}
		productive = &snapshot
	}
	return BuildZECIShadowTelemetry(metrics, verified, productive)
}

func zeciEvidenceHash(metrics []ShardEpochMetrics, verified []compute.VerifiedWork, productive *IdleCapitalSnapshot) (types.Hash, error) {
	var w codec.Writer
	w.U16(ZECIShadowVersion)
	w.U32(uint32(len(metrics)))
	for _, metric := range metrics {
		raw, err := metric.CanonicalBytes()
		if err != nil {
			return types.Hash{}, ErrZECI
		}
		w.Bytes(raw)
	}
	w.U32(uint32(len(verified)))
	for _, work := range verified {
		w.Fixed(work.JobID[:])
		w.U8(uint8(work.Class))
		w.U64(work.Units)
		w.U64(work.Vector.CPUUnits)
		w.U64(work.Vector.GPUFP32Units)
		w.U64(work.Vector.GPUFP64Units)
		w.U64(work.Vector.TensorUnits)
		w.U64(work.Vector.MemoryByteSeconds)
		w.U64(work.Vector.VRAMByteSeconds)
		w.U64(work.Vector.StorageBytes)
		w.U64(work.Vector.NetworkBytes)
		w.U64(work.PaidZPH)
		w.U8(uint8(work.Verification))
		w.Fixed(work.ResultRoot[:])
	}
	hasProductive := productive != nil && productive.ProductiveObservedCapital > 0
	w.Bool(hasProductive)
	if hasProductive {
		w.U64(productive.Height)
		w.U64(productive.ProductiveObservedCapital)
		w.U32(productive.ProductiveCoverageBps)
	}
	hash := types.Hash(codec.DomainHash("zephyr/zeci-evidence/v1", w.BytesCopy()))
	if types.IsZero32([32]byte(hash)) {
		return types.Hash{}, ErrZECI
	}
	return hash, nil
}
