package economics

import (
	"bytes"
	"math"
	"math/big"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	idleCapitalTrackerVersion uint16 = 1
	maxIdleTrackerObjects            = 250_000
	maxIdleTrackerTransfers          = 250_000
	maxIdleTrackerLots               = 500_000
	maxIdleTrackerBigIntBytes        = 128
)

// CapitalTarget identifies a deterministic destination for capital lineage.
// Exactly one of ObjectID or TransferID must be non-zero. TransferID is an
// opaque consensus-derived cross-shard receipt/transfer key supplied by the
// caller; the tracker does not invent receipt identity.
type CapitalTarget struct {
	ObjectID   types.ObjectID
	TransferID types.Hash
	Amount     uint64
}

func (t CapitalTarget) Validate() error {
	objectSet := !types.IsZero32([32]byte(t.ObjectID))
	transferSet := !types.IsZero32([32]byte(t.TransferID))
	if t.Amount == 0 || objectSet == transferSet {
		return ErrIdleCapital
	}
	return nil
}

type IdleCapitalSnapshot struct {
	Height                    uint64
	TrackedCapital            uint64
	BootstrapCapital          uint64
	LiveObjects               uint64
	PendingTransfers          uint64
	Dormancy                  DormancyHistogram
	ProductiveObservedCapital uint64
	ProductiveCoverageBps     uint32
}

// IdleCapitalTracker is prospective shadow telemetry. It follows lineage after
// first observation without changing coin/object consensus encoding. A lineage
// first seen through BootstrapObject remains explicitly marked as bootstrap so
// callers can distinguish prospective coverage from complete historical
// lineage knowledge.
type IdleCapitalTracker struct {
	objects           map[types.ObjectID][]CapitalLot
	pendingTransfers  map[types.Hash][]CapitalLot
	bootstrapLineages map[types.Hash]struct{}
	productive        ProductiveCoverageAccumulator
}

func NewIdleCapitalTracker() *IdleCapitalTracker {
	return &IdleCapitalTracker{
		objects:           make(map[types.ObjectID][]CapitalLot),
		pendingTransfers:  make(map[types.Hash][]CapitalLot),
		bootstrapLineages: make(map[types.Hash]struct{}),
	}
}

func (t *IdleCapitalTracker) Clone() *IdleCapitalTracker {
	if t == nil {
		return nil
	}
	out := NewIdleCapitalTracker()
	for id, lots := range t.objects {
		out.objects[id] = append([]CapitalLot(nil), lots...)
	}
	for id, lots := range t.pendingTransfers {
		out.pendingTransfers[id] = append([]CapitalLot(nil), lots...)
	}
	for lineage := range t.bootstrapLineages {
		out.bootstrapLineages[lineage] = struct{}{}
	}
	out.productive.observedCapital = t.productive.observedCapital
	out.productive.weightedCoverageBps.Set(&t.productive.weightedCoverageBps)
	return out
}

// BootstrapObject begins prospective tracking for a native coin already known
// to consensus. CreatedHeight is trusted only because it comes from the
// consensus-stamped coin object. The resulting lineage is marked bootstrap:
// its history before this object existed is intentionally not claimed.
func (t *IdleCapitalTracker) BootstrapObject(id types.ObjectID, amount, createdHeight uint64) error {
	if t == nil || types.IsZero32([32]byte(id)) || amount == 0 || createdHeight == 0 {
		return ErrIdleCapital
	}
	if len(t.objects) >= maxIdleTrackerObjects {
		return ErrIdleCapital
	}
	if _, exists := t.objects[id]; exists {
		return ErrIdleCapital
	}
	lineage := types.HashBytes("idle-capital/bootstrap-lineage/v1", id[:])
	lot := CapitalLot{LineageID: lineage, Amount: amount, IdleSinceHeight: createdHeight}
	if err := lot.Validate(); err != nil {
		return err
	}
	t.objects[id] = []CapitalLot{lot}
	t.bootstrapLineages[lineage] = struct{}{}
	return nil
}

// ApplyTransition atomically consumes tracked local objects and allocates their
// lineage to local object outputs and/or opaque cross-shard transfer targets.
// target declaration order is irrelevant: allocation is canonical by target
// kind and identifier. burned is removed from circulation and receives no new
// lineage. Ordinary transfers never reset IdleSinceHeight.
func (t *IdleCapitalTracker) ApplyTransition(inputs []types.ObjectID, targets []CapitalTarget, burned uint64) error {
	if t == nil || len(inputs) == 0 || (len(targets) == 0 && burned == 0) {
		return ErrIdleCapital
	}
	preview := t.Clone()
	if preview == nil {
		return ErrIdleCapital
	}
	if err := preview.applyTransition(inputs, targets, burned); err != nil {
		return err
	}
	*t = *preview
	return nil
}

func (t *IdleCapitalTracker) applyTransition(inputs []types.ObjectID, targets []CapitalTarget, burned uint64) error {
	seenInputs := make(map[types.ObjectID]struct{}, len(inputs))
	inputLots := make([]CapitalLot, 0, len(inputs))
	for _, id := range inputs {
		if types.IsZero32([32]byte(id)) {
			return ErrIdleCapital
		}
		if _, duplicate := seenInputs[id]; duplicate {
			return ErrIdleCapital
		}
		seenInputs[id] = struct{}{}
		lots, ok := t.objects[id]
		if !ok || len(lots) == 0 {
			return ErrIdleCapital
		}
		inputLots = append(inputLots, lots...)
	}
	canonicalLots, err := compactCapitalLots(inputLots)
	if err != nil {
		return err
	}

	orderedTargets := append([]CapitalTarget(nil), targets...)
	seenObjects := make(map[types.ObjectID]struct{}, len(targets))
	seenTransfers := make(map[types.Hash]struct{}, len(targets))
	for _, target := range orderedTargets {
		if err := target.Validate(); err != nil {
			return err
		}
		if !types.IsZero32([32]byte(target.ObjectID)) {
			if _, duplicate := seenObjects[target.ObjectID]; duplicate {
				return ErrIdleCapital
			}
			seenObjects[target.ObjectID] = struct{}{}
			if _, exists := t.objects[target.ObjectID]; exists {
				return ErrIdleCapital
			}
		} else {
			if _, duplicate := seenTransfers[target.TransferID]; duplicate {
				return ErrIdleCapital
			}
			seenTransfers[target.TransferID] = struct{}{}
			if _, exists := t.pendingTransfers[target.TransferID]; exists {
				return ErrIdleCapital
			}
		}
	}
	if len(t.objects)-len(inputs)+len(seenObjects) > maxIdleTrackerObjects || len(t.pendingTransfers)+len(seenTransfers) > maxIdleTrackerTransfers {
		return ErrIdleCapital
	}
	sort.Slice(orderedTargets, func(i, j int) bool {
		iObject := !types.IsZero32([32]byte(orderedTargets[i].ObjectID))
		jObject := !types.IsZero32([32]byte(orderedTargets[j].ObjectID))
		if iObject != jObject {
			return iObject
		}
		if iObject {
			return bytes.Compare(orderedTargets[i].ObjectID[:], orderedTargets[j].ObjectID[:]) < 0
		}
		return bytes.Compare(orderedTargets[i].TransferID[:], orderedTargets[j].TransferID[:]) < 0
	})

	amounts := make([]uint64, 0, len(orderedTargets)+1)
	for _, target := range orderedTargets {
		amounts = append(amounts, target.Amount)
	}
	if burned > 0 {
		amounts = append(amounts, burned)
	}
	if len(amounts) == 0 {
		return ErrIdleCapital
	}
	allocations, err := SplitCapitalLineage(canonicalLots, amounts)
	if err != nil {
		return err
	}
	for id := range seenInputs {
		delete(t.objects, id)
	}
	for i, target := range orderedTargets {
		lots := allocations[i]
		if !types.IsZero32([32]byte(target.ObjectID)) {
			t.objects[target.ObjectID] = lots
		} else {
			t.pendingTransfers[target.TransferID] = lots
		}
	}
	t.pruneBootstrapLineages()
	return nil
}

// MaterializeTransfer turns one tracked pending cross-shard target into its
// imported local object without changing age or lineage.
func (t *IdleCapitalTracker) MaterializeTransfer(transferID types.Hash, objectID types.ObjectID) error {
	if t == nil || types.IsZero32([32]byte(transferID)) || types.IsZero32([32]byte(objectID)) {
		return ErrIdleCapital
	}
	if len(t.objects) >= maxIdleTrackerObjects {
		return ErrIdleCapital
	}
	if _, exists := t.objects[objectID]; exists {
		return ErrIdleCapital
	}
	lots, exists := t.pendingTransfers[transferID]
	if !exists || len(lots) == 0 {
		return ErrIdleCapital
	}
	delete(t.pendingTransfers, transferID)
	t.objects[objectID] = append([]CapitalLot(nil), lots...)
	return nil
}

// MarkObjectProductive is an explicit productive-use hook. It is intentionally
// separate from ApplyTransition so self-transfers and generic contract calls
// cannot reset dormancy merely by moving capital.
func (t *IdleCapitalTracker) MarkObjectProductive(objectID types.ObjectID, height uint64, coverageBps uint32) error {
	if t == nil || types.IsZero32([32]byte(objectID)) || height == 0 || coverageBps > 10_000 {
		return ErrIdleCapital
	}
	lots, exists := t.objects[objectID]
	if !exists || len(lots) == 0 {
		return ErrIdleCapital
	}
	var amount uint64
	for _, lot := range lots {
		if math.MaxUint64-amount < lot.Amount {
			return ErrIdleCapital
		}
		amount += lot.Amount
	}
	marked, err := MarkProductiveCoverage(lots, height, coverageBps)
	if err != nil {
		return err
	}
	preview := t.Clone()
	if preview == nil {
		return ErrIdleCapital
	}
	preview.objects[objectID] = marked
	if err := preview.productive.Observe(amount, coverageBps); err != nil {
		return err
	}
	*t = *preview
	return nil
}

// ResetProductiveCoverage starts a new aggregation interval while preserving
// lineage age state.
func (t *IdleCapitalTracker) ResetProductiveCoverage() error {
	if t == nil {
		return ErrIdleCapital
	}
	t.productive = ProductiveCoverageAccumulator{}
	return nil
}

func (t *IdleCapitalTracker) ObjectLots(objectID types.ObjectID) ([]CapitalLot, bool) {
	if t == nil {
		return nil, false
	}
	lots, ok := t.objects[objectID]
	if !ok {
		return nil, false
	}
	return append([]CapitalLot(nil), lots...), true
}

func (t *IdleCapitalTracker) Snapshot(height uint64, bounds []uint64) (IdleCapitalSnapshot, error) {
	if t == nil || height == 0 {
		return IdleCapitalSnapshot{}, ErrIdleCapital
	}
	lots := t.allLots()
	if len(lots) == 0 {
		return IdleCapitalSnapshot{}, ErrIdleCapital
	}
	histogram, err := BuildDormancyHistogram(lots, height, bounds)
	if err != nil {
		return IdleCapitalSnapshot{}, err
	}
	bootstrap := uint64(0)
	for _, lot := range lots {
		if _, ok := t.bootstrapLineages[lot.LineageID]; !ok {
			continue
		}
		if math.MaxUint64-bootstrap < lot.Amount {
			return IdleCapitalSnapshot{}, ErrIdleCapital
		}
		bootstrap += lot.Amount
	}
	out := IdleCapitalSnapshot{
		Height:           height,
		TrackedCapital:   histogram.Total,
		BootstrapCapital: bootstrap,
		LiveObjects:      uint64(len(t.objects)),
		PendingTransfers: uint64(len(t.pendingTransfers)),
		Dormancy:         histogram,
	}
	if t.productive.observedCapital > 0 {
		coverage, err := t.productive.Snapshot()
		if err != nil {
			return IdleCapitalSnapshot{}, err
		}
		out.ProductiveObservedCapital = coverage.ObservedCapital
		out.ProductiveCoverageBps = coverage.ProductiveCoverageBps
	}
	return out, nil
}

func (t *IdleCapitalTracker) CheckpointBytes() ([]byte, error) {
	if t == nil || len(t.objects) > maxIdleTrackerObjects || len(t.pendingTransfers) > maxIdleTrackerTransfers {
		return nil, ErrIdleCapital
	}
	var w codec.Writer
	w.U16(idleCapitalTrackerVersion)

	objectIDs := make([]types.ObjectID, 0, len(t.objects))
	for id := range t.objects {
		objectIDs = append(objectIDs, id)
	}
	sort.Slice(objectIDs, func(i, j int) bool { return bytes.Compare(objectIDs[i][:], objectIDs[j][:]) < 0 })
	w.U32(uint32(len(objectIDs)))
	for _, id := range objectIDs {
		w.Fixed(id[:])
		if err := writeCapitalLots(&w, t.objects[id]); err != nil {
			return nil, err
		}
	}

	transferIDs := make([]types.Hash, 0, len(t.pendingTransfers))
	for id := range t.pendingTransfers {
		transferIDs = append(transferIDs, id)
	}
	sort.Slice(transferIDs, func(i, j int) bool { return bytes.Compare(transferIDs[i][:], transferIDs[j][:]) < 0 })
	w.U32(uint32(len(transferIDs)))
	for _, id := range transferIDs {
		w.Fixed(id[:])
		if err := writeCapitalLots(&w, t.pendingTransfers[id]); err != nil {
			return nil, err
		}
	}

	lineages := make([]types.Hash, 0, len(t.bootstrapLineages))
	for lineage := range t.bootstrapLineages {
		lineages = append(lineages, lineage)
	}
	sort.Slice(lineages, func(i, j int) bool { return bytes.Compare(lineages[i][:], lineages[j][:]) < 0 })
	w.U32(uint32(len(lineages)))
	for _, lineage := range lineages {
		w.Fixed(lineage[:])
	}
	w.U64(t.productive.observedCapital)
	weighted := t.productive.weightedCoverageBps.Bytes()
	if len(weighted) > maxIdleTrackerBigIntBytes {
		return nil, ErrIdleCapital
	}
	w.Bytes(weighted)
	return w.BytesCopy(), nil
}

func RestoreIdleCapitalTracker(data []byte) (*IdleCapitalTracker, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != idleCapitalTrackerVersion {
		return nil, ErrIdleCapital
	}
	out := NewIdleCapitalTracker()
	lotCount := 0

	objectCount, err := r.U32()
	if err != nil || objectCount > maxIdleTrackerObjects {
		return nil, ErrIdleCapital
	}
	for i := uint32(0); i < objectCount; i++ {
		raw, err := r.Fixed(32)
		if err != nil {
			return nil, ErrIdleCapital
		}
		var id types.ObjectID
		copy(id[:], raw)
		if types.IsZero32([32]byte(id)) {
			return nil, ErrIdleCapital
		}
		if _, duplicate := out.objects[id]; duplicate {
			return nil, ErrIdleCapital
		}
		lots, err := readCapitalLots(r, &lotCount)
		if err != nil {
			return nil, err
		}
		out.objects[id] = lots
	}

	transferCount, err := r.U32()
	if err != nil || transferCount > maxIdleTrackerTransfers {
		return nil, ErrIdleCapital
	}
	for i := uint32(0); i < transferCount; i++ {
		raw, err := r.Fixed(32)
		if err != nil {
			return nil, ErrIdleCapital
		}
		var id types.Hash
		copy(id[:], raw)
		if types.IsZero32([32]byte(id)) {
			return nil, ErrIdleCapital
		}
		if _, duplicate := out.pendingTransfers[id]; duplicate {
			return nil, ErrIdleCapital
		}
		lots, err := readCapitalLots(r, &lotCount)
		if err != nil {
			return nil, err
		}
		out.pendingTransfers[id] = lots
	}

	bootstrapCount, err := r.U32()
	if err != nil || bootstrapCount > maxIdleTrackerLots {
		return nil, ErrIdleCapital
	}
	for i := uint32(0); i < bootstrapCount; i++ {
		raw, err := r.Fixed(32)
		if err != nil {
			return nil, ErrIdleCapital
		}
		var lineage types.Hash
		copy(lineage[:], raw)
		if types.IsZero32([32]byte(lineage)) {
			return nil, ErrIdleCapital
		}
		if _, duplicate := out.bootstrapLineages[lineage]; duplicate {
			return nil, ErrIdleCapital
		}
		out.bootstrapLineages[lineage] = struct{}{}
	}
	out.productive.observedCapital, err = r.U64()
	if err != nil {
		return nil, ErrIdleCapital
	}
	weighted, err := r.Bytes(maxIdleTrackerBigIntBytes)
	if err != nil || r.Done() != nil {
		return nil, ErrIdleCapital
	}
	out.productive.weightedCoverageBps.SetBytes(weighted)
	if err := out.validate(); err != nil {
		return nil, err
	}
	return out, nil
}

func writeCapitalLots(w *codec.Writer, lots []CapitalLot) error {
	if w == nil {
		return ErrIdleCapital
	}
	canonical, err := compactCapitalLots(lots)
	if err != nil || len(canonical) > maxIdleTrackerLots {
		return ErrIdleCapital
	}
	w.U32(uint32(len(canonical)))
	for _, lot := range canonical {
		w.Fixed(lot.LineageID[:])
		w.U64(lot.Amount)
		w.U64(lot.IdleSinceHeight)
	}
	return nil
}

func readCapitalLots(r *codec.Reader, total *int) ([]CapitalLot, error) {
	count, err := r.U32()
	if err != nil || count == 0 || count > maxIdleTrackerLots || total == nil || *total > maxIdleTrackerLots-int(count) {
		return nil, ErrIdleCapital
	}
	*total += int(count)
	lots := make([]CapitalLot, int(count))
	for i := range lots {
		raw, err := r.Fixed(32)
		if err != nil {
			return nil, ErrIdleCapital
		}
		copy(lots[i].LineageID[:], raw)
		lots[i].Amount, err = r.U64()
		if err != nil {
			return nil, ErrIdleCapital
		}
		lots[i].IdleSinceHeight, err = r.U64()
		if err != nil || lots[i].Validate() != nil {
			return nil, ErrIdleCapital
		}
	}
	canonical, err := compactCapitalLots(lots)
	if err != nil || len(canonical) != len(lots) {
		return nil, ErrIdleCapital
	}
	for i := range lots {
		if lots[i] != canonical[i] {
			return nil, ErrIdleCapital
		}
	}
	return lots, nil
}

func (t *IdleCapitalTracker) validate() error {
	if t == nil || len(t.objects) > maxIdleTrackerObjects || len(t.pendingTransfers) > maxIdleTrackerTransfers {
		return ErrIdleCapital
	}
	activeLineages := make(map[types.Hash]struct{})
	lotCount := 0
	visit := func(lots []CapitalLot) error {
		canonical, err := compactCapitalLots(lots)
		if err != nil || len(canonical) != len(lots) || lotCount > maxIdleTrackerLots-len(lots) {
			return ErrIdleCapital
		}
		lotCount += len(lots)
		for i := range lots {
			if lots[i] != canonical[i] {
				return ErrIdleCapital
			}
			activeLineages[lots[i].LineageID] = struct{}{}
		}
		return nil
	}
	for id, lots := range t.objects {
		if types.IsZero32([32]byte(id)) || visit(lots) != nil {
			return ErrIdleCapital
		}
	}
	for id, lots := range t.pendingTransfers {
		if types.IsZero32([32]byte(id)) || visit(lots) != nil {
			return ErrIdleCapital
		}
	}
	for lineage := range t.bootstrapLineages {
		if types.IsZero32([32]byte(lineage)) {
			return ErrIdleCapital
		}
		if _, active := activeLineages[lineage]; !active {
			return ErrIdleCapital
		}
	}
	weighted := new(big.Int).Set(&t.productive.weightedCoverageBps)
	if t.productive.observedCapital == 0 {
		if weighted.Sign() != 0 {
			return ErrIdleCapital
		}
		return nil
	}
	if weighted.Sign() < 0 {
		return ErrIdleCapital
	}
	maximum := new(big.Int).Mul(new(big.Int).SetUint64(t.productive.observedCapital), big.NewInt(10_000))
	if weighted.Cmp(maximum) > 0 {
		return ErrIdleCapital
	}
	return nil
}

func (t *IdleCapitalTracker) allLots() []CapitalLot {
	if t == nil {
		return nil
	}
	out := make([]CapitalLot, 0)
	for _, lots := range t.objects {
		out = append(out, lots...)
	}
	for _, lots := range t.pendingTransfers {
		out = append(out, lots...)
	}
	return out
}

func (t *IdleCapitalTracker) pruneBootstrapLineages() {
	if t == nil || len(t.bootstrapLineages) == 0 {
		return
	}
	active := make(map[types.Hash]struct{})
	for _, lot := range t.allLots() {
		active[lot.LineageID] = struct{}{}
	}
	for lineage := range t.bootstrapLineages {
		if _, ok := active[lineage]; !ok {
			delete(t.bootstrapLineages, lineage)
		}
	}
}
