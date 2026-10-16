package compute

import (
	"errors"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type VerificationMode uint8

const (
	VerificationUnknown VerificationMode = iota
	VerificationDeterministic
	VerificationReplicated
	VerificationChallenge
	VerificationZeroKnowledge
	VerificationTEE
	VerificationClientApproved
	VerificationHybrid
)

var (
	ErrInvalidOffer  = errors.New("invalid compute offer")
	ErrInvalidJob    = errors.New("invalid compute job")
	ErrInvalidResult = errors.New("invalid compute result")
)

type Resources struct {
	CPUCores      uint16
	MemoryMiB     uint32
	GPUCount      uint16
	GPUMemoryMiB  uint32
	StorageMiB    uint64
	BandwidthMbps uint32
	Capabilities  []string
}

type Offer struct {
	Provider         types.AccountID
	Resources        Resources
	PricePerUnit     uint64
	Collateral       uint64
	Verification     []VerificationMode
	ValidUntilHeight uint64
}

type Job struct {
	Owner              types.AccountID
	WorkloadHash       types.Hash
	InputRoot          types.Hash
	Resources          Resources
	MaxPrice           uint64
	CollateralRequired uint64
	Verification       VerificationMode
	DeadlineHeight     uint64
	Replicas           uint16
	Private            bool
}

type Result struct {
	JobID           types.JobID
	Provider        types.AccountID
	ResultRoot      types.Hash
	ProofHash       types.Hash
	AttestationHash types.Hash
	CompletedHeight uint64
}

func (r Resources) Validate() error {
	if r.CPUCores == 0 && r.GPUCount == 0 {
		return ErrInvalidOffer
	}
	if r.MemoryMiB == 0 || len(r.Capabilities) > 32 {
		return ErrInvalidOffer
	}
	for _, c := range r.Capabilities {
		if strings.TrimSpace(c) == "" || len(c) > 64 {
			return ErrInvalidOffer
		}
	}
	return nil
}

func (o Offer) Validate() error {
	if types.IsZero32([32]byte(o.Provider)) || o.PricePerUnit == 0 || o.ValidUntilHeight == 0 ||
		len(o.Verification) == 0 || len(o.Verification) > 7 {
		return ErrInvalidOffer
	}
	return o.Resources.Validate()
}

func (j Job) Validate() error {
	if types.IsZero32([32]byte(j.Owner)) || types.IsZero32([32]byte(j.WorkloadHash)) ||
		types.IsZero32([32]byte(j.InputRoot)) || j.MaxPrice == 0 || j.DeadlineHeight == 0 ||
		j.Verification <= VerificationUnknown || j.Verification > VerificationHybrid {
		return ErrInvalidJob
	}
	if j.Verification == VerificationReplicated && j.Replicas < 2 {
		return ErrInvalidJob
	}
	return j.Resources.Validate()
}

func (r Result) Validate() error {
	if types.IsZero32([32]byte(r.JobID)) || types.IsZero32([32]byte(r.Provider)) ||
		types.IsZero32([32]byte(r.ResultRoot)) || r.CompletedHeight == 0 {
		return ErrInvalidResult
	}
	return nil
}

func (o Offer) MarshalBinary() ([]byte, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Fixed(o.Provider[:])
	writeResources(&w, o.Resources)
	w.U64(o.PricePerUnit)
	w.U64(o.Collateral)
	w.U32(uint32(len(o.Verification)))
	for _, mode := range o.Verification {
		w.U8(uint8(mode))
	}
	w.U64(o.ValidUntilHeight)
	return w.BytesCopy(), nil
}

func (j Job) MarshalBinary() ([]byte, error) {
	if err := j.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Fixed(j.Owner[:])
	w.Fixed(j.WorkloadHash[:])
	w.Fixed(j.InputRoot[:])
	writeResources(&w, j.Resources)
	w.U64(j.MaxPrice)
	w.U64(j.CollateralRequired)
	w.U8(uint8(j.Verification))
	w.U64(j.DeadlineHeight)
	w.U16(j.Replicas)
	w.Bool(j.Private)
	return w.BytesCopy(), nil
}

func writeResources(w *codec.Writer, r Resources) {
	w.U16(r.CPUCores)
	w.U32(r.MemoryMiB)
	w.U16(r.GPUCount)
	w.U32(r.GPUMemoryMiB)
	w.U64(r.StorageMiB)
	w.U32(r.BandwidthMbps)
	w.U32(uint32(len(r.Capabilities)))
	for _, c := range r.Capabilities {
		w.String(strings.TrimSpace(c))
	}
}
