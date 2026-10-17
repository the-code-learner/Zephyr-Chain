package economics

import (
	"bytes"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// FinalizedProviderComputeEvidence records one provider whose result was
// accepted and paid by a deterministic finalized compute settlement.
//
// Work Units/Vector come from the protocol WorkRegistry. PaidZPH is the
// settlement payment, not a contribution score or reward multiplier. For
// replicated jobs, multiple paid providers can legitimately carry the same
// WorkSpec because replicas perform the same workload; callers must not sum
// these records into an economic score without a separately specified policy.
type FinalizedProviderComputeEvidence struct {
	JobObjectID          types.ObjectID
	JobID                types.JobID
	JobOwner             types.AccountID
	Provider             types.AccountID
	WorkloadHash         types.Hash
	BenchmarkHash        types.Hash
	Class                compute.WorkClass
	Units                uint64
	Vector               compute.WorkVector
	PaidZPH              uint64
	Verification         compute.VerificationMode
	ResultRoot           types.Hash
	AssignedProviderCount uint32
	PaidProviderCount     uint32
	JobStateHash          types.Hash
	SettlementReceiptHash types.Hash
	WorkRegistryHash      types.Hash
}

func (e FinalizedProviderComputeEvidence) Validate() error {
	if types.IsZero32([32]byte(e.JobObjectID)) ||
		types.IsZero32([32]byte(e.JobID)) ||
		types.IsZero32([32]byte(e.JobOwner)) ||
		types.IsZero32([32]byte(e.Provider)) ||
		types.IsZero32([32]byte(e.WorkloadHash)) ||
		types.IsZero32([32]byte(e.BenchmarkHash)) ||
		e.Class <= compute.WorkUnknown || e.Class >= compute.WorkClassCount ||
		e.Units == 0 || e.Vector.IsZero() || e.PaidZPH == 0 ||
		e.Verification <= compute.VerificationUnknown || e.Verification > compute.VerificationHybrid ||
		types.IsZero32([32]byte(e.ResultRoot)) ||
		e.AssignedProviderCount == 0 || e.PaidProviderCount == 0 ||
		e.PaidProviderCount > e.AssignedProviderCount ||
		types.IsZero32([32]byte(e.JobStateHash)) ||
		types.IsZero32([32]byte(e.SettlementReceiptHash)) ||
		types.IsZero32([32]byte(e.WorkRegistryHash)) {
		return ErrFinalizedEconomics
	}
	return nil
}

func (e FinalizedProviderComputeEvidence) CanonicalBytes() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Fixed(e.JobObjectID[:])
	w.Fixed(e.JobID[:])
	w.Fixed(e.JobOwner[:])
	w.Fixed(e.Provider[:])
	w.Fixed(e.WorkloadHash[:])
	w.Fixed(e.BenchmarkHash[:])
	w.U8(uint8(e.Class))
	w.U64(e.Units)
	w.U64(e.Vector.CPUUnits)
	w.U64(e.Vector.GPUFP32Units)
	w.U64(e.Vector.GPUFP64Units)
	w.U64(e.Vector.TensorUnits)
	w.U64(e.Vector.MemoryByteSeconds)
	w.U64(e.Vector.VRAMByteSeconds)
	w.U64(e.Vector.StorageBytes)
	w.U64(e.Vector.NetworkBytes)
	w.U64(e.PaidZPH)
	w.U8(uint8(e.Verification))
	w.Fixed(e.ResultRoot[:])
	w.U32(e.AssignedProviderCount)
	w.U32(e.PaidProviderCount)
	w.Fixed(e.JobStateHash[:])
	w.Fixed(e.SettlementReceiptHash[:])
	w.Fixed(e.WorkRegistryHash[:])
	return w.BytesCopy(), nil
}

func (e FinalizedProviderComputeEvidence) Hash() (types.Hash, error) {
	raw, err := e.CanonicalBytes()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/finalized-provider-compute-evidence/v1", raw)), nil
}

// DeriveFinalizedProviderComputeEvidence derives per-provider accepted/paid
// compute evidence from the same finalized transaction/result pair used by
// productive-capital accounting.
//
// Non-settlement transactions return (nil, false, nil). Settlement evidence
// fails closed unless:
//   - the settlement operation and consumed job are exact;
//   - the canonical settlement receipt matches deterministic execution;
//   - the workload exists in the protocol WorkRegistry;
//   - every paid provider has exactly one assignment and one accepted result.
//
// No DeviceCredential, RPC summary, advertised capacity or external claim is
// used for provider attribution.
func DeriveFinalizedProviderComputeEvidence(
	transaction tx.Transaction,
	result execution.Result,
	registry *compute.WorkRegistry,
) ([]FinalizedProviderComputeEvidence, bool, error) {
	productive, applied, err := DeriveFinalizedComputeProductiveEvidence(transaction, result)
	if err != nil || !applied {
		return nil, applied, err
	}
	if registry == nil {
		return nil, false, ErrFinalizedEconomics
	}

	if len(transaction.Operations) != 1 {
		return nil, false, ErrFinalizedEconomics
	}
	ref, err := compute.ParseJobRef(transaction.Operations[0].Payload)
	if err != nil || ref.JobObject != productive.JobObjectID {
		return nil, false, ErrFinalizedEconomics
	}

	var record compute.OnChainJob
	found := false
	for _, witness := range transaction.Witnesses {
		if witness.Object.ID != ref.JobObject {
			continue
		}
		if found || witness.Object.Kind != object.KindComputeJob || witness.Object.Owner != transaction.Sender {
			return nil, false, ErrFinalizedEconomics
		}
		record, err = compute.ParseOnChainJob(witness.Object.Data)
		if err != nil || record.Job.Owner != transaction.Sender {
			return nil, false, ErrFinalizedEconomics
		}
		found = true
	}
	if !found || record.ID != productive.JobID {
		return nil, false, ErrFinalizedEconomics
	}

	receipt, ok, err := uniqueSettlementReceiptForJob(result.Created, record.ID)
	if err != nil || !ok {
		return nil, false, ErrFinalizedEconomics
	}
	verified, err := compute.ObserveFinalizedSettlement(record, receipt, registry)
	if err != nil ||
		verified.JobID != productive.JobID ||
		verified.ResultRoot != productive.ResultRoot ||
		verified.Verification != productive.Verification {
		return nil, false, ErrFinalizedEconomics
	}

	spec, ok := registry.Resolve(record.Job.WorkloadHash)
	if !ok || spec.Validate() != nil ||
		spec.Class != verified.Class ||
		spec.Units != verified.Units ||
		spec.Vector != verified.Vector {
		return nil, false, ErrFinalizedEconomics
	}
	registryHash, err := registry.Hash()
	if err != nil || types.IsZero32([32]byte(registryHash)) {
		return nil, false, ErrFinalizedEconomics
	}
	jobRaw, err := record.MarshalBinary()
	if err != nil {
		return nil, false, ErrFinalizedEconomics
	}
	jobStateHash := types.Hash(codec.DomainHash("zephyr/compute/pre-settlement-job/v1", jobRaw))
	receiptRaw := receipt.MarshalBinary()
	settlementHash := types.Hash(codec.DomainHash("zephyr/compute/settlement-receipt/v1", receiptRaw))
	if types.IsZero32([32]byte(jobStateHash)) || types.IsZero32([32]byte(settlementHash)) {
		return nil, false, ErrFinalizedEconomics
	}

	assignments := make(map[types.AccountID]compute.Assignment, len(record.Assignments))
	for _, assignment := range record.Assignments {
		if types.IsZero32([32]byte(assignment.Provider)) || assignment.Price == 0 {
			return nil, false, ErrFinalizedEconomics
		}
		if _, duplicate := assignments[assignment.Provider]; duplicate {
			return nil, false, ErrFinalizedEconomics
		}
		assignments[assignment.Provider] = assignment
	}
	results := make(map[types.AccountID]compute.Result, len(record.Results))
	for _, providerResult := range record.Results {
		if err := providerResult.Validate(); err != nil || providerResult.JobID != record.ID {
			return nil, false, ErrFinalizedEconomics
		}
		if _, duplicate := results[providerResult.Provider]; duplicate {
			return nil, false, ErrFinalizedEconomics
		}
		results[providerResult.Provider] = providerResult
	}

	providers := make([]types.AccountID, 0, len(receipt.Payments))
	for provider, paid := range receipt.Payments {
		assignment, assigned := assignments[provider]
		providerResult, reported := results[provider]
		if !assigned || !reported || paid == 0 || paid != assignment.Price ||
			providerResult.ResultRoot != receipt.ResultRoot {
			return nil, false, ErrFinalizedEconomics
		}
		providers = append(providers, provider)
	}
	if len(providers) == 0 || len(providers) > len(record.Assignments) ||
		uint64(len(providers)) > uint64(^uint32(0)) ||
		uint64(len(record.Assignments)) > uint64(^uint32(0)) {
		return nil, false, ErrFinalizedEconomics
	}
	sort.Slice(providers, func(i, j int) bool {
		return bytes.Compare(providers[i][:], providers[j][:]) < 0
	})

	evidence := make([]FinalizedProviderComputeEvidence, 0, len(providers))
	for _, provider := range providers {
		item := FinalizedProviderComputeEvidence{
			JobObjectID:           productive.JobObjectID,
			JobID:                 record.ID,
			JobOwner:              record.Job.Owner,
			Provider:              provider,
			WorkloadHash:          spec.WorkloadHash,
			BenchmarkHash:         spec.BenchmarkHash,
			Class:                 spec.Class,
			Units:                 spec.Units,
			Vector:                spec.Vector,
			PaidZPH:               receipt.Payments[provider],
			Verification:          record.Job.Verification,
			ResultRoot:            receipt.ResultRoot,
			AssignedProviderCount: uint32(len(record.Assignments)),
			PaidProviderCount:     uint32(len(providers)),
			JobStateHash:          jobStateHash,
			SettlementReceiptHash: settlementHash,
			WorkRegistryHash:      registryHash,
		}
		if err := item.Validate(); err != nil {
			return nil, false, err
		}
		evidence = append(evidence, item)
	}
	return evidence, true, nil
}
