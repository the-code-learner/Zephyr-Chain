package economics

import (
	"bytes"
	"errors"
	"math"
	"math/big"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrIdleCapital = errors.New("invalid Zephyr idle-capital shadow input")

// CapitalLot is shadow-only lineage metadata. LineageID follows economic
// capital across object splits, while IdleSinceHeight records the last height
// at which that portion of capital was classified as productively deployed.
// Neither field changes ownership, spendability or live monetary state.
type CapitalLot struct {
	LineageID       types.Hash
	Amount          uint64
	IdleSinceHeight uint64
}

func (l CapitalLot) Validate() error {
	if types.IsZero32([32]byte(l.LineageID)) || l.Amount == 0 || l.IdleSinceHeight == 0 {
		return ErrIdleCapital
	}
	return nil
}

type DormancyBucket struct {
	MaxAgeBlocks uint64
	Amount       uint64
}

type DormancyHistogram struct {
	Height  uint64
	Total   uint64
	Buckets []DormancyBucket
}

// BuildDormancyHistogram aggregates capital by lineage age rather than by
// wallet/object count. Splitting one object into many therefore cannot change
// the aggregate histogram when the underlying lots are preserved.
func BuildDormancyHistogram(lots []CapitalLot, height uint64, upperBounds []uint64) (DormancyHistogram, error) {
	if height == 0 || len(lots) == 0 {
		return DormancyHistogram{}, ErrIdleCapital
	}
	for i, bound := range upperBounds {
		if bound == 0 || (i > 0 && bound <= upperBounds[i-1]) {
			return DormancyHistogram{}, ErrIdleCapital
		}
	}
	out := DormancyHistogram{Height: height, Buckets: make([]DormancyBucket, len(upperBounds)+1)}
	for i, bound := range upperBounds {
		out.Buckets[i].MaxAgeBlocks = bound
	}
	out.Buckets[len(out.Buckets)-1].MaxAgeBlocks = math.MaxUint64
	for _, lot := range lots {
		if err := lot.Validate(); err != nil || lot.IdleSinceHeight > height {
			return DormancyHistogram{}, ErrIdleCapital
		}
		age := height - lot.IdleSinceHeight
		index := sort.Search(len(upperBounds), func(i int) bool { return age <= upperBounds[i] })
		if math.MaxUint64-out.Buckets[index].Amount < lot.Amount || math.MaxUint64-out.Total < lot.Amount {
			return DormancyHistogram{}, ErrIdleCapital
		}
		out.Buckets[index].Amount += lot.Amount
		out.Total += lot.Amount
	}
	return out, nil
}

// SplitCapitalLineage deterministically allocates lineage lots to output
// amounts without changing aggregate amount or idle age. It is intentionally
// independent of wallet/account identity so address fragmentation gives no
// age reset.
func SplitCapitalLineage(lots []CapitalLot, outputAmounts []uint64) ([][]CapitalLot, error) {
	if len(lots) == 0 || len(outputAmounts) == 0 {
		return nil, ErrIdleCapital
	}
	canonical, err := compactCapitalLots(lots)
	if err != nil {
		return nil, err
	}
	totalInput := new(big.Int)
	for _, lot := range canonical {
		totalInput.Add(totalInput, new(big.Int).SetUint64(lot.Amount))
	}
	totalOutput := new(big.Int)
	for _, amount := range outputAmounts {
		if amount == 0 {
			return nil, ErrIdleCapital
		}
		totalOutput.Add(totalOutput, new(big.Int).SetUint64(amount))
	}
	if totalInput.Cmp(totalOutput) != 0 || !totalInput.IsUint64() {
		return nil, ErrIdleCapital
	}

	out := make([][]CapitalLot, len(outputAmounts))
	lotIndex := 0
	remaining := canonical[0].Amount
	for outputIndex, amount := range outputAmounts {
		need := amount
		for need > 0 {
			if lotIndex >= len(canonical) {
				return nil, ErrIdleCapital
			}
			take := remaining
			if take > need {
				take = need
			}
			lot := canonical[lotIndex]
			lot.Amount = take
			out[outputIndex] = append(out[outputIndex], lot)
			need -= take
			remaining -= take
			if remaining == 0 {
				lotIndex++
				if lotIndex < len(canonical) {
					remaining = canonical[lotIndex].Amount
				}
			}
		}
	}
	if lotIndex != len(canonical) {
		return nil, ErrIdleCapital
	}
	return out, nil
}

// MarkProductiveCoverage resets the idle clock for exactly the requested
// fraction of aggregate capital. Lots are compacted and processed oldest-first
// before the split, making rounding independent of wallet fragmentation.
func MarkProductiveCoverage(lots []CapitalLot, height uint64, coverageBps uint32) ([]CapitalLot, error) {
	if height == 0 || coverageBps > 10_000 {
		return nil, ErrIdleCapital
	}
	canonical, err := compactCapitalLots(lots)
	if err != nil {
		return nil, err
	}
	for _, lot := range canonical {
		if lot.IdleSinceHeight > height {
			return nil, ErrIdleCapital
		}
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].IdleSinceHeight != canonical[j].IdleSinceHeight {
			return canonical[i].IdleSinceHeight < canonical[j].IdleSinceHeight
		}
		return bytes.Compare(canonical[i].LineageID[:], canonical[j].LineageID[:]) < 0
	})
	total := new(big.Int)
	for _, lot := range canonical {
		total.Add(total, new(big.Int).SetUint64(lot.Amount))
	}
	target := new(big.Int).Mul(new(big.Int).Set(total), new(big.Int).SetUint64(uint64(coverageBps)))
	target.Quo(target, big.NewInt(10_000))
	if !target.IsUint64() {
		return nil, ErrIdleCapital
	}
	remaining := target.Uint64()
	out := make([]CapitalLot, 0, len(canonical)+1)
	for _, lot := range canonical {
		if remaining == 0 {
			out = append(out, lot)
			continue
		}
		productive := lot.Amount
		if productive > remaining {
			productive = remaining
		}
		if productive < lot.Amount {
			idle := lot
			idle.Amount -= productive
			out = append(out, idle)
		}
		productiveLot := lot
		productiveLot.Amount = productive
		productiveLot.IdleSinceHeight = height
		out = append(out, productiveLot)
		remaining -= productive
	}
	if remaining != 0 {
		return nil, ErrIdleCapital
	}
	return compactCapitalLots(out)
}

type ProductiveCoverageAccumulator struct {
	observedCapital     uint64
	weightedCoverageBps big.Int
}

type ProductiveCoverageSnapshot struct {
	ObservedCapital       uint64
	ProductiveCoverageBps uint32
}

// Observe accepts a deterministic, consensus-derived coverage classification
// supplied by a concrete productive-use hook. It only aggregates the signal;
// it does not decide that arbitrary transfers are productive.
func (a *ProductiveCoverageAccumulator) Observe(amount uint64, coverageBps uint32) error {
	if a == nil || amount == 0 || coverageBps > 10_000 || math.MaxUint64-a.observedCapital < amount {
		return ErrIdleCapital
	}
	a.observedCapital += amount
	term := new(big.Int).Mul(new(big.Int).SetUint64(amount), new(big.Int).SetUint64(uint64(coverageBps)))
	a.weightedCoverageBps.Add(&a.weightedCoverageBps, term)
	return nil
}

func (a *ProductiveCoverageAccumulator) Snapshot() (ProductiveCoverageSnapshot, error) {
	if a == nil || a.observedCapital == 0 {
		return ProductiveCoverageSnapshot{}, ErrIdleCapital
	}
	value := new(big.Int).Quo(new(big.Int).Set(&a.weightedCoverageBps), new(big.Int).SetUint64(a.observedCapital))
	if !value.IsUint64() || value.Uint64() > 10_000 {
		return ProductiveCoverageSnapshot{}, ErrIdleCapital
	}
	return ProductiveCoverageSnapshot{ObservedCapital: a.observedCapital, ProductiveCoverageBps: uint32(value.Uint64())}, nil
}

type StateCarryingCostPolicy struct {
	BaseUnitsPerObject uint64
	UnitsPerKiB        uint64
}

func EstimateStateCarryingCost(objectCount, payloadBytes uint64, policy StateCarryingCostPolicy) (uint64, error) {
	if objectCount == 0 || policy.BaseUnitsPerObject == 0 || policy.UnitsPerKiB == 0 {
		return 0, ErrIdleCapital
	}
	kilobytes := payloadBytes / 1024
	if payloadBytes%1024 != 0 {
		kilobytes++
	}
	cost := new(big.Int).Mul(new(big.Int).SetUint64(objectCount), new(big.Int).SetUint64(policy.BaseUnitsPerObject))
	cost.Add(cost, new(big.Int).Mul(new(big.Int).SetUint64(kilobytes), new(big.Int).SetUint64(policy.UnitsPerKiB)))
	if !cost.IsUint64() {
		return 0, ErrIdleCapital
	}
	return cost.Uint64(), nil
}

type FragmentationScenario struct {
	Fragments         uint32
	Histogram         DormancyHistogram
	CarryingCostUnits uint64
}

// SimulateWalletFragmentation is a shadow-only adversarial helper. It splits a
// single capital lot into N object-sized fragments and proves the economic age
// distribution remains invariant while state carrying cost increases.
func SimulateWalletFragmentation(seed CapitalLot, fragments uint32, height, payloadBytesPerObject uint64, bounds []uint64, policy StateCarryingCostPolicy) (FragmentationScenario, error) {
	if err := seed.Validate(); err != nil || fragments == 0 || uint64(fragments) > seed.Amount {
		return FragmentationScenario{}, ErrIdleCapital
	}
	amounts := make([]uint64, int(fragments))
	base := seed.Amount / uint64(fragments)
	remainder := seed.Amount % uint64(fragments)
	for i := range amounts {
		amounts[i] = base
		if uint64(i) < remainder {
			amounts[i]++
		}
	}
	split, err := SplitCapitalLineage([]CapitalLot{seed}, amounts)
	if err != nil {
		return FragmentationScenario{}, err
	}
	flattened := make([]CapitalLot, 0, fragments)
	for _, outputLots := range split {
		flattened = append(flattened, outputLots...)
	}
	histogram, err := BuildDormancyHistogram(flattened, height, bounds)
	if err != nil {
		return FragmentationScenario{}, err
	}
	payloadTotal := new(big.Int).Mul(new(big.Int).SetUint64(uint64(fragments)), new(big.Int).SetUint64(payloadBytesPerObject))
	if !payloadTotal.IsUint64() {
		return FragmentationScenario{}, ErrIdleCapital
	}
	cost, err := EstimateStateCarryingCost(uint64(fragments), payloadTotal.Uint64(), policy)
	if err != nil {
		return FragmentationScenario{}, err
	}
	return FragmentationScenario{Fragments: fragments, Histogram: histogram, CarryingCostUnits: cost}, nil
}

func compactCapitalLots(lots []CapitalLot) ([]CapitalLot, error) {
	if len(lots) == 0 {
		return nil, ErrIdleCapital
	}
	out := append([]CapitalLot(nil), lots...)
	for _, lot := range out {
		if err := lot.Validate(); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		cmp := bytes.Compare(out[i].LineageID[:], out[j].LineageID[:])
		if cmp != 0 {
			return cmp < 0
		}
		return out[i].IdleSinceHeight < out[j].IdleSinceHeight
	})
	compacted := make([]CapitalLot, 0, len(out))
	for _, lot := range out {
		if len(compacted) > 0 {
			last := &compacted[len(compacted)-1]
			if last.LineageID == lot.LineageID && last.IdleSinceHeight == lot.IdleSinceHeight {
				if math.MaxUint64-last.Amount < lot.Amount {
					return nil, ErrIdleCapital
				}
				last.Amount += lot.Amount
				continue
			}
		}
		compacted = append(compacted, lot)
	}
	return compacted, nil
}
