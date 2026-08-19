package compute

import (
	"bytes"
	"errors"
	"math"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const WorkSpecVersion uint16 = 1

type WorkClass uint8

const (
	WorkUnknown WorkClass = iota
	WorkCPUGeneral
	WorkGPUFP32
	WorkGPUFP64
	WorkTensorAI
	WorkMemory
	WorkStorage
	WorkNetwork
	WorkRendering
	WorkAIInference
	WorkAITraining
	WorkScientific
	WorkClassCount
)

var (
	ErrInvalidWorkSpec       = errors.New("invalid normalized compute work specification")
	ErrInvalidWorkRegistry   = errors.New("invalid normalized compute work registry update")
	ErrInvalidWorkSettlement = errors.New("invalid verified compute work settlement")
)

// WorkVector is intentionally a vector rather than a single synthetic FLOP
// number. Units are protocol-defined normalized work units for CPU/GPU classes
// plus directly measurable byte-based resource dimensions.
type WorkVector struct {
	CPUUnits          uint64
	GPUFP32Units      uint64
	GPUFP64Units      uint64
	TensorUnits       uint64
	MemoryByteSeconds uint64
	VRAMByteSeconds   uint64
	StorageBytes      uint64
	NetworkBytes      uint64
}

func (v WorkVector) IsZero() bool {
	return v.CPUUnits == 0 && v.GPUFP32Units == 0 && v.GPUFP64Units == 0 && v.TensorUnits == 0 &&
		v.MemoryByteSeconds == 0 && v.VRAMByteSeconds == 0 && v.StorageBytes == 0 && v.NetworkBytes == 0
}

// WorkSpec binds a normalized unit definition to a concrete workload and a
// benchmark/specification hash. Only registry-approved specs should be used by
// monetary telemetry; arbitrary provider declarations are never sufficient.
type WorkSpec struct {
	Version       uint16
	Class         WorkClass
	Units         uint64
	WorkloadHash  types.Hash
	BenchmarkHash types.Hash
	Vector        WorkVector
}

func (s WorkSpec) Validate() error {
	if s.Version != WorkSpecVersion || s.Class <= WorkUnknown || s.Class >= WorkClassCount || s.Units == 0 ||
		types.IsZero32([32]byte(s.WorkloadHash)) || types.IsZero32([32]byte(s.BenchmarkHash)) || s.Vector.IsZero() {
		return ErrInvalidWorkSpec
	}
	return nil
}

func (s WorkSpec) MarshalBinary() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(s.Version)
	w.U8(uint8(s.Class))
	w.U64(s.Units)
	w.Fixed(s.WorkloadHash[:])
	w.Fixed(s.BenchmarkHash[:])
	w.U64(s.Vector.CPUUnits)
	w.U64(s.Vector.GPUFP32Units)
	w.U64(s.Vector.GPUFP64Units)
	w.U64(s.Vector.TensorUnits)
	w.U64(s.Vector.MemoryByteSeconds)
	w.U64(s.Vector.VRAMByteSeconds)
	w.U64(s.Vector.StorageBytes)
	w.U64(s.Vector.NetworkBytes)
	return w.BytesCopy(), nil
}

func ParseWorkSpec(data []byte) (WorkSpec, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil {
		return WorkSpec{}, ErrInvalidWorkSpec
	}
	class, err := r.U8()
	if err != nil {
		return WorkSpec{}, ErrInvalidWorkSpec
	}
	units, err := r.U64()
	if err != nil {
		return WorkSpec{}, ErrInvalidWorkSpec
	}
	workload, err := readHash(r)
	if err != nil {
		return WorkSpec{}, ErrInvalidWorkSpec
	}
	benchmark, err := readHash(r)
	if err != nil {
		return WorkSpec{}, ErrInvalidWorkSpec
	}
	values := make([]uint64, 8)
	for i := range values {
		values[i], err = r.U64()
		if err != nil {
			return WorkSpec{}, ErrInvalidWorkSpec
		}
	}
	if r.Done() != nil {
		return WorkSpec{}, ErrInvalidWorkSpec
	}
	out := WorkSpec{
		Version:       version,
		Class:         WorkClass(class),
		Units:         units,
		WorkloadHash:  workload,
		BenchmarkHash: benchmark,
		Vector: WorkVector{
			CPUUnits: values[0], GPUFP32Units: values[1], GPUFP64Units: values[2], TensorUnits: values[3],
			MemoryByteSeconds: values[4], VRAMByteSeconds: values[5], StorageBytes: values[6], NetworkBytes: values[7],
		},
	}
	if err := out.Validate(); err != nil {
		return WorkSpec{}, err
	}
	return out, nil
}

type WorkRegistry struct {
	byWorkload map[types.Hash]WorkSpec
}

func NewWorkRegistry(specs []WorkSpec) (*WorkRegistry, error) {
	registry := &WorkRegistry{byWorkload: make(map[types.Hash]WorkSpec, len(specs))}
	for _, spec := range specs {
		if err := registry.Register(spec); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *WorkRegistry) Register(spec WorkSpec) error {
	if r == nil || spec.Validate() != nil {
		return ErrInvalidWorkRegistry
	}
	if r.byWorkload == nil {
		r.byWorkload = make(map[types.Hash]WorkSpec)
	}
	if existing, ok := r.byWorkload[spec.WorkloadHash]; ok {
		a, _ := existing.MarshalBinary()
		b, _ := spec.MarshalBinary()
		if !bytes.Equal(a, b) {
			return ErrInvalidWorkRegistry
		}
		return nil
	}
	r.byWorkload[spec.WorkloadHash] = spec
	return nil
}

func (r *WorkRegistry) Resolve(workload types.Hash) (WorkSpec, bool) {
	if r == nil {
		return WorkSpec{}, false
	}
	spec, ok := r.byWorkload[workload]
	return spec, ok
}

// CanonicalSpecs returns an independent workload-hash-sorted registry view.
// Registry insertion order must never affect economic replay or checkpoint
// identity.
func (r *WorkRegistry) CanonicalSpecs() ([]WorkSpec, error) {
	if r == nil {
		return nil, ErrInvalidWorkRegistry
	}
	out := make([]WorkSpec, 0, len(r.byWorkload))
	for _, spec := range r.byWorkload {
		if err := spec.Validate(); err != nil {
			return nil, ErrInvalidWorkRegistry
		}
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i].WorkloadHash[:], out[j].WorkloadHash[:]) < 0
	})
	return out, nil
}

// Hash commits the exact normalized workload definitions used by ZCPI/ZCSI.
// Checkpoints bind this hash instead of serializing an implicitly trusted
// registry snapshot; restore must be supplied the same registry explicitly.
func (r *WorkRegistry) Hash() (types.Hash, error) {
	specs, err := r.CanonicalSpecs()
	if err != nil {
		return types.Hash{}, err
	}
	var w codec.Writer
	w.U32(uint32(len(specs)))
	for _, spec := range specs {
		raw, err := spec.MarshalBinary()
		if err != nil {
			return types.Hash{}, ErrInvalidWorkRegistry
		}
		w.Bytes(raw)
	}
	return types.Hash(codec.DomainHash("zephyr/work-registry/v2", w.BytesCopy())), nil
}

// VerifiedWork is an index-eligible observation. It can only be derived from a
// finalized/verified on-chain settlement plus a workload spec already approved
// by the protocol registry. Offer prices and provider self-reported capacity do
// not enter this record.
type VerifiedWork struct {
	JobID        types.JobID
	Class        WorkClass
	Units        uint64
	Vector       WorkVector
	PaidZPH      uint64
	Verification VerificationMode
	ResultRoot   types.Hash
}

func ObserveVerifiedWork(record OnChainJob, settlement OnChainSettlement, registry *WorkRegistry) (VerifiedWork, error) {
	if record.Status != JobSettled || registry == nil || settlement.JobID != record.ID ||
		types.IsZero32([32]byte(settlement.ResultRoot)) {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}
	spec, ok := registry.Resolve(record.Job.WorkloadHash)
	if !ok || spec.Validate() != nil {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}
	var paid uint64
	for _, amount := range settlement.Payments {
		if math.MaxUint64-paid < amount {
			return VerifiedWork{}, ErrInvalidWorkSettlement
		}
		paid += amount
	}
	if paid == 0 {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}
	return VerifiedWork{
		JobID: record.ID, Class: spec.Class, Units: spec.Units, Vector: spec.Vector, PaidZPH: paid,
		Verification: record.Job.Verification, ResultRoot: settlement.ResultRoot,
	}, nil
}
