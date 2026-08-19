package compute

import (
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	computeOfferObjectIndex uint32 = 0x90000000
	computeJobObjectIndex   uint32 = 0x90000001
)

type OnChainJob struct {
	ID          types.JobID
	Job         Job
	Escrow      uint64
	Status      JobStatus
	Assignments []Assignment
	Results     []Result
}

func ParseOffer(data []byte) (Offer, error) {
	r := codec.NewReader(data)
	provider, err := readAccount(r)
	if err != nil {
		return Offer{}, ErrInvalidOffer
	}
	resources, err := readResources(r)
	if err != nil {
		return Offer{}, ErrInvalidOffer
	}
	price, err := r.U64()
	if err != nil {
		return Offer{}, ErrInvalidOffer
	}
	collateral, err := r.U64()
	if err != nil {
		return Offer{}, ErrInvalidOffer
	}
	count, err := r.U32()
	if err != nil || count == 0 || count > 7 {
		return Offer{}, ErrInvalidOffer
	}
	modes := make([]VerificationMode, int(count))
	for i := range modes {
		mode, err := r.U8()
		if err != nil {
			return Offer{}, ErrInvalidOffer
		}
		modes[i] = VerificationMode(mode)
	}
	validUntil, err := r.U64()
	if err != nil || r.Done() != nil {
		return Offer{}, ErrInvalidOffer
	}
	offer := Offer{Provider: provider, Resources: resources, PricePerUnit: price, Collateral: collateral, Verification: modes, ValidUntilHeight: validUntil}
	if err := offer.Validate(); err != nil {
		return Offer{}, err
	}
	return offer, nil
}

func ParseJob(data []byte) (Job, error) {
	r := codec.NewReader(data)
	owner, err := readAccount(r)
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	workload, err := readHash(r)
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	inputRoot, err := readHash(r)
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	resources, err := readResources(r)
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	maxPrice, err := r.U64()
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	collateral, err := r.U64()
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	mode, err := r.U8()
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	deadline, err := r.U64()
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	replicas, err := r.U16()
	if err != nil {
		return Job{}, ErrInvalidJob
	}
	private, err := r.Bool()
	if err != nil || r.Done() != nil {
		return Job{}, ErrInvalidJob
	}
	job := Job{Owner: owner, WorkloadHash: workload, InputRoot: inputRoot, Resources: resources, MaxPrice: maxPrice, CollateralRequired: collateral, Verification: VerificationMode(mode), DeadlineHeight: deadline, Replicas: replicas, Private: private}
	if err := job.Validate(); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (r Result) MarshalBinary() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Fixed(r.JobID[:])
	w.Fixed(r.Provider[:])
	w.Fixed(r.ResultRoot[:])
	w.Fixed(r.ProofHash[:])
	w.Fixed(r.AttestationHash[:])
	w.U64(r.CompletedHeight)
	return w.BytesCopy(), nil
}

func ParseResult(data []byte) (Result, error) {
	r := codec.NewReader(data)
	jobIDBytes, err := r.Fixed(32)
	if err != nil {
		return Result{}, ErrInvalidResult
	}
	provider, err := readAccount(r)
	if err != nil {
		return Result{}, ErrInvalidResult
	}
	resultRoot, err := readHash(r)
	if err != nil {
		return Result{}, ErrInvalidResult
	}
	proofHash, err := readHash(r)
	if err != nil {
		return Result{}, ErrInvalidResult
	}
	attestationHash, err := readHash(r)
	if err != nil {
		return Result{}, ErrInvalidResult
	}
	completedHeight, err := r.U64()
	if err != nil || r.Done() != nil {
		return Result{}, ErrInvalidResult
	}
	var jobID types.JobID
	copy(jobID[:], jobIDBytes)
	result := Result{JobID: jobID, Provider: provider, ResultRoot: resultRoot, ProofHash: proofHash, AttestationHash: attestationHash, CompletedHeight: completedHeight}
	if err := result.Validate(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (a Assignment) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(a.OfferID)) || types.IsZero32([32]byte(a.Provider)) || a.Price == 0 {
		return nil, ErrMarketState
	}
	var w codec.Writer
	w.Fixed(a.OfferID[:])
	w.Fixed(a.Provider[:])
	w.U64(a.Price)
	return w.BytesCopy(), nil
}

func ParseAssignment(data []byte) (Assignment, error) {
	r := codec.NewReader(data)
	offerID, err := readHash(r)
	if err != nil {
		return Assignment{}, ErrMarketState
	}
	provider, err := readAccount(r)
	if err != nil {
		return Assignment{}, ErrMarketState
	}
	price, err := r.U64()
	if err != nil || r.Done() != nil || price == 0 {
		return Assignment{}, ErrMarketState
	}
	assignment := Assignment{OfferID: offerID, Provider: provider, Price: price}
	if _, err := assignment.MarshalBinary(); err != nil {
		return Assignment{}, err
	}
	return assignment, nil
}

func (j OnChainJob) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(j.ID)) || j.Escrow < j.Job.MaxPrice || j.Status < JobPending || j.Status > JobExpired {
		return nil, ErrMarketState
	}
	jobBytes, err := j.Job.MarshalBinary()
	if err != nil {
		return nil, err
	}
	assignments := append([]Assignment(nil), j.Assignments...)
	sort.Slice(assignments, func(i, k int) bool { return assignments[i].Provider.String() < assignments[k].Provider.String() })
	results := append([]Result(nil), j.Results...)
	sort.Slice(results, func(i, k int) bool { return results[i].Provider.String() < results[k].Provider.String() })

	var w codec.Writer
	w.Fixed(j.ID[:])
	w.Bytes(jobBytes)
	w.U64(j.Escrow)
	w.U8(uint8(j.Status))
	w.U32(uint32(len(assignments)))
	for _, assignment := range assignments {
		raw, err := assignment.MarshalBinary()
		if err != nil {
			return nil, err
		}
		w.Bytes(raw)
	}
	w.U32(uint32(len(results)))
	for _, result := range results {
		raw, err := result.MarshalBinary()
		if err != nil {
			return nil, err
		}
		w.Bytes(raw)
	}
	return w.BytesCopy(), nil
}

func ParseOnChainJob(data []byte) (OnChainJob, error) {
	r := codec.NewReader(data)
	jobIDBytes, err := r.Fixed(32)
	if err != nil {
		return OnChainJob{}, ErrMarketState
	}
	jobBytes, err := r.Bytes(1 << 20)
	if err != nil {
		return OnChainJob{}, ErrMarketState
	}
	job, err := ParseJob(jobBytes)
	if err != nil {
		return OnChainJob{}, err
	}
	escrow, err := r.U64()
	if err != nil {
		return OnChainJob{}, ErrMarketState
	}
	status, err := r.U8()
	if err != nil {
		return OnChainJob{}, ErrMarketState
	}
	assignmentCount, err := r.U32()
	if err != nil || assignmentCount > 1024 {
		return OnChainJob{}, ErrMarketState
	}
	assignments := make([]Assignment, int(assignmentCount))
	for i := range assignments {
		raw, err := r.Bytes(1024)
		if err != nil {
			return OnChainJob{}, ErrMarketState
		}
		assignments[i], err = ParseAssignment(raw)
		if err != nil {
			return OnChainJob{}, err
		}
	}
	resultCount, err := r.U32()
	if err != nil || resultCount > 1024 {
		return OnChainJob{}, ErrMarketState
	}
	results := make([]Result, int(resultCount))
	for i := range results {
		raw, err := r.Bytes(1024)
		if err != nil {
			return OnChainJob{}, ErrMarketState
		}
		results[i], err = ParseResult(raw)
		if err != nil {
			return OnChainJob{}, err
		}
	}
	if r.Done() != nil {
		return OnChainJob{}, ErrMarketState
	}
	var jobID types.JobID
	copy(jobID[:], jobIDBytes)
	record := OnChainJob{ID: jobID, Job: job, Escrow: escrow, Status: JobStatus(status), Assignments: assignments, Results: results}
	if _, err := record.MarshalBinary(); err != nil {
		return OnChainJob{}, err
	}
	return record, nil
}

func NewOfferObject(txID types.Hash, shard uint32, offer Offer) (object.Object, error) {
	raw, err := offer.MarshalBinary()
	if err != nil {
		return object.Object{}, err
	}
	return object.Object{ID: types.ObjectIDForShard(txID, computeOfferObjectIndex, shard), Version: 1, Owner: offer.Provider, Kind: object.KindComputeOffer, Data: raw}, nil
}

func NewJobObject(txID types.Hash, shard uint32, job Job, escrow uint64) (object.Object, OnChainJob, error) {
	jobID := types.JobIDFromTransaction(txID, 0)
	record := OnChainJob{ID: jobID, Job: job, Escrow: escrow, Status: JobPending}
	raw, err := record.MarshalBinary()
	if err != nil {
		return object.Object{}, OnChainJob{}, err
	}
	return object.Object{ID: types.ObjectIDForShard(txID, computeJobObjectIndex, shard), Version: 1, Owner: job.Owner, Kind: object.KindComputeJob, Data: raw}, record, nil
}

func readResources(r *codec.Reader) (Resources, error) {
	cpu, err := r.U16()
	if err != nil {
		return Resources{}, err
	}
	memory, err := r.U32()
	if err != nil {
		return Resources{}, err
	}
	gpu, err := r.U16()
	if err != nil {
		return Resources{}, err
	}
	gpuMemory, err := r.U32()
	if err != nil {
		return Resources{}, err
	}
	storage, err := r.U64()
	if err != nil {
		return Resources{}, err
	}
	bandwidth, err := r.U32()
	if err != nil {
		return Resources{}, err
	}
	count, err := r.U32()
	if err != nil || count > 32 {
		return Resources{}, ErrInvalidOffer
	}
	capabilities := make([]string, int(count))
	for i := range capabilities {
		capabilities[i], err = r.String(64)
		if err != nil {
			return Resources{}, err
		}
	}
	return Resources{CPUCores: cpu, MemoryMiB: memory, GPUCount: gpu, GPUMemoryMiB: gpuMemory, StorageMiB: storage, BandwidthMbps: bandwidth, Capabilities: capabilities}, nil
}

func readAccount(r *codec.Reader) (types.AccountID, error) {
	raw, err := r.Fixed(32)
	if err != nil {
		return types.AccountID{}, err
	}
	var out types.AccountID
	copy(out[:], raw)
	return out, nil
}

func readHash(r *codec.Reader) (types.Hash, error) {
	raw, err := r.Fixed(32)
	if err != nil {
		return types.Hash{}, err
	}
	var out types.Hash
	copy(out[:], raw)
	return out, nil
}
