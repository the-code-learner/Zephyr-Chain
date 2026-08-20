package compute

import (
	"errors"
	"math"
	"sort"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var (
	ErrMarketNotFound     = errors.New("compute market record not found")
	ErrMarketState        = errors.New("invalid compute market state transition")
	ErrMarketMatch        = errors.New("compute offer does not satisfy job")
	ErrMarketEscrow       = errors.New("compute job escrow is insufficient")
	ErrMarketVerification = errors.New("compute result verification requirements not met")
	ErrMarketDuplicate    = errors.New("duplicate compute market record")
)

type JobStatus uint8

const (
	JobPending JobStatus = iota + 1
	JobAssigned
	JobAwaitingVerification
	JobSettled
	JobExpired
)

type OfferRecord struct {
	ID    types.Hash
	Offer Offer
}

type Assignment struct {
	OfferID  types.Hash
	Provider types.AccountID
	Price    uint64
}

type JobRecord struct {
	ID          types.JobID
	Job         Job
	Escrow      uint64
	Status      JobStatus
	Assignments []Assignment
	Results     map[types.AccountID]Result
}

type VerificationEvidence struct {
	DeterministicReplay bool
	ProofVerified       bool
	AttestationVerified bool
	ChallengePassed     bool
	ClientApproved      bool
}

type Settlement struct {
	JobID      types.JobID
	ResultRoot types.Hash
	Payments   map[types.AccountID]uint64
	Refund     uint64
}

type Market struct {
	mu     sync.Mutex
	offers map[types.Hash]OfferRecord
	jobs   map[types.JobID]*JobRecord
}

func NewMarket() *Market {
	return &Market{offers: make(map[types.Hash]OfferRecord), jobs: make(map[types.JobID]*JobRecord)}
}

func OfferID(offer Offer) (types.Hash, error) {
	raw, err := offer.MarshalBinary()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/compute-offer/v2", raw)), nil
}

func ComputeJobID(job Job) (types.JobID, error) {
	raw, err := job.MarshalBinary()
	if err != nil {
		return types.JobID{}, err
	}
	return types.JobID(codec.DomainHash("zephyr/compute-job/v2", raw)), nil
}

func (m *Market) PublishOffer(offer Offer, height uint64) (types.Hash, error) {
	if err := offer.Validate(); err != nil || height == 0 || offer.ValidUntilHeight < height {
		return types.Hash{}, ErrInvalidOffer
	}
	id, _ := OfferID(offer)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.offers[id]; exists {
		return types.Hash{}, ErrMarketDuplicate
	}
	m.offers[id] = OfferRecord{ID: id, Offer: offer}
	return id, nil
}

func (m *Market) PostJob(job Job, escrow, height uint64) (types.JobID, error) {
	if err := job.Validate(); err != nil || height == 0 || job.DeadlineHeight < height {
		return types.JobID{}, ErrInvalidJob
	}
	if escrow < job.MaxPrice {
		return types.JobID{}, ErrMarketEscrow
	}
	id, _ := ComputeJobID(job)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.jobs[id]; exists {
		return types.JobID{}, ErrMarketDuplicate
	}
	m.jobs[id] = &JobRecord{ID: id, Job: job, Escrow: escrow, Status: JobPending, Results: make(map[types.AccountID]Result)}
	return id, nil
}

func (m *Market) Assign(jobID types.JobID, offerID types.Hash, height uint64) (Assignment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return Assignment{}, ErrMarketNotFound
	}
	offerRecord, ok := m.offers[offerID]
	if !ok {
		return Assignment{}, ErrMarketNotFound
	}
	if job.Status == JobSettled || job.Status == JobExpired || height == 0 || height > job.Job.DeadlineHeight || height > offerRecord.Offer.ValidUntilHeight {
		return Assignment{}, ErrMarketState
	}
	if !offerMatchesJob(offerRecord.Offer, job.Job) {
		return Assignment{}, ErrMarketMatch
	}
	for _, existing := range job.Assignments {
		if existing.Provider == offerRecord.Offer.Provider {
			return Assignment{}, ErrMarketDuplicate
		}
	}
	target := requiredAssignments(job.Job)
	if len(job.Assignments) >= target {
		return Assignment{}, ErrMarketState
	}
	var committed uint64
	for _, existing := range job.Assignments {
		if math.MaxUint64-committed < existing.Price {
			return Assignment{}, ErrMarketEscrow
		}
		committed += existing.Price
	}
	price := offerRecord.Offer.PricePerUnit
	if price > job.Job.MaxPrice || math.MaxUint64-committed < price || committed+price > job.Job.MaxPrice || committed+price > job.Escrow {
		return Assignment{}, ErrMarketEscrow
	}
	assignment := Assignment{OfferID: offerID, Provider: offerRecord.Offer.Provider, Price: price}
	job.Assignments = append(job.Assignments, assignment)
	if len(job.Assignments) == target {
		job.Status = JobAssigned
	}
	return assignment, nil
}

func (m *Market) SubmitResult(result Result) error {
	if err := result.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[result.JobID]
	if !ok {
		return ErrMarketNotFound
	}
	if job.Status != JobAssigned && job.Status != JobAwaitingVerification {
		return ErrMarketState
	}
	if result.CompletedHeight > job.Job.DeadlineHeight {
		return ErrMarketState
	}
	assigned := false
	for _, assignment := range job.Assignments {
		if assignment.Provider == result.Provider {
			assigned = true
			break
		}
	}
	if !assigned {
		return ErrMarketMatch
	}
	if _, duplicate := job.Results[result.Provider]; duplicate {
		return ErrMarketDuplicate
	}
	job.Results[result.Provider] = result
	if len(job.Results) == requiredAssignments(job.Job) {
		job.Status = JobAwaitingVerification
	}
	return nil
}

func (m *Market) Finalize(jobID types.JobID, evidence VerificationEvidence) (Settlement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return Settlement{}, ErrMarketNotFound
	}
	if job.Status != JobAwaitingVerification || len(job.Results) != requiredAssignments(job.Job) {
		return Settlement{}, ErrMarketState
	}
	root, replicatedMatch := commonResultRoot(job)
	if types.IsZero32([32]byte(root)) || !verificationSatisfied(job.Job.Verification, replicatedMatch, job.Results, evidence) {
		return Settlement{}, ErrMarketVerification
	}
	payments := make(map[types.AccountID]uint64, len(job.Assignments))
	var paid uint64
	for _, assignment := range job.Assignments {
		if math.MaxUint64-paid < assignment.Price {
			return Settlement{}, ErrMarketEscrow
		}
		paid += assignment.Price
		payments[assignment.Provider] += assignment.Price
	}
	if paid > job.Escrow {
		return Settlement{}, ErrMarketEscrow
	}
	job.Status = JobSettled
	return Settlement{JobID: jobID, ResultRoot: root, Payments: payments, Refund: job.Escrow - paid}, nil
}

func (m *Market) Expire(height uint64) []types.JobID {
	m.mu.Lock()
	defer m.mu.Unlock()
	var expired []types.JobID
	for id, job := range m.jobs {
		if job.Status != JobSettled && job.Status != JobExpired && height > job.Job.DeadlineHeight {
			job.Status = JobExpired
			expired = append(expired, id)
		}
	}
	sort.Slice(expired, func(i, j int) bool { return expired[i].String() < expired[j].String() })
	return expired
}

func requiredAssignments(job Job) int {
	if job.Verification == VerificationReplicated {
		return int(job.Replicas)
	}
	return 1
}

func offerMatchesJob(offer Offer, job Job) bool {
	if offer.Resources.CPUCores < job.Resources.CPUCores || offer.Resources.MemoryMiB < job.Resources.MemoryMiB ||
		offer.Resources.GPUCount < job.Resources.GPUCount || offer.Resources.GPUMemoryMiB < job.Resources.GPUMemoryMiB ||
		offer.Resources.StorageMiB < job.Resources.StorageMiB || offer.Resources.BandwidthMbps < job.Resources.BandwidthMbps ||
		offer.Collateral < job.CollateralRequired || !supportsMode(offer.Verification, job.Verification) {
		return false
	}
	available := make(map[string]struct{}, len(offer.Resources.Capabilities))
	for _, capability := range offer.Resources.Capabilities {
		available[capability] = struct{}{}
	}
	for _, capability := range job.Resources.Capabilities {
		if _, ok := available[capability]; !ok {
			return false
		}
	}
	return true
}

func supportsMode(modes []VerificationMode, target VerificationMode) bool {
	for _, mode := range modes {
		if mode == target || mode == VerificationHybrid {
			return true
		}
	}
	return false
}

func commonResultRoot(job *JobRecord) (types.Hash, bool) {
	var root types.Hash
	first := true
	match := true
	for _, result := range job.Results {
		if first {
			root = result.ResultRoot
			first = false
			continue
		}
		if result.ResultRoot != root {
			match = false
		}
	}
	return root, match && !first
}

func verificationSatisfied(mode VerificationMode, replicatedMatch bool, results map[types.AccountID]Result, evidence VerificationEvidence) bool {
	hasProof := true
	hasAttestation := true
	for _, result := range results {
		hasProof = hasProof && !types.IsZero32([32]byte(result.ProofHash))
		hasAttestation = hasAttestation && !types.IsZero32([32]byte(result.AttestationHash))
	}
	switch mode {
	case VerificationDeterministic:
		return evidence.DeterministicReplay
	case VerificationReplicated:
		return replicatedMatch
	case VerificationChallenge:
		return evidence.ChallengePassed
	case VerificationZeroKnowledge:
		return hasProof && evidence.ProofVerified
	case VerificationTEE:
		return hasAttestation && evidence.AttestationVerified
	case VerificationClientApproved:
		return evidence.ClientApproved
	case VerificationHybrid:
		score := 0
		if evidence.DeterministicReplay {
			score++
		}
		if replicatedMatch && len(results) > 1 {
			score++
		}
		if hasProof && evidence.ProofVerified {
			score++
		}
		if hasAttestation && evidence.AttestationVerified {
			score++
		}
		if evidence.ChallengePassed {
			score++
		}
		if evidence.ClientApproved {
			score++
		}
		return score >= 2
	default:
		return false
	}
}
