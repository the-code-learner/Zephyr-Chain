package provider

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	MaxCapabilityBytes = 64
	MaxInlineInput      = 1 << 20
	MaxParameters       = 1 << 20
	MaxInlineOutput     = 1 << 20
)

var (
	ErrRequest       = errors.New("invalid compute provider request")
	ErrExecutor      = errors.New("compute executor is unavailable")
	ErrInputRoot     = errors.New("compute input does not match declared root")
	ErrOutput        = errors.New("invalid compute provider output")
	ErrDuplicateExec = errors.New("duplicate compute executor capability")
)

type Request struct {
	JobID        types.JobID
	WorkloadHash types.Hash
	InputRoot    types.Hash
	Capability   string
	Input        []byte
	Parameters   []byte
}

type Response struct {
	JobID      types.JobID
	ResultRoot types.Hash
	Output     []byte
	Stored     bool
}

type Executor interface {
	Capability() string
	Execute(context.Context, []byte, []byte) ([]byte, error)
}

type Store interface {
	Put(types.Hash, []byte) error
	Get(types.Hash) ([]byte, error)
}

type Service struct {
	mu        sync.RWMutex
	executors map[string]Executor
	store     Store
}

func New(store Store, executors ...Executor) (*Service, error) {
	if store == nil {
		return nil, ErrRequest
	}
	s := &Service{executors: make(map[string]Executor), store: store}
	for _, executor := range executors {
		if err := s.Register(executor); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) Register(executor Executor) error {
	if executor == nil {
		return ErrExecutor
	}
	capability := strings.TrimSpace(executor.Capability())
	if capability == "" || len(capability) > MaxCapabilityBytes {
		return ErrExecutor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.executors[capability]; exists {
		return ErrDuplicateExec
	}
	s.executors[capability] = executor
	return nil
}

func (s *Service) Handle(ctx context.Context, payload []byte) ([]byte, error) {
	request, err := ParseRequest(payload)
	if err != nil {
		return nil, err
	}
	response, err := s.Execute(ctx, request)
	if err != nil {
		return nil, err
	}
	return response.MarshalBinary()
}

func (s *Service) Execute(ctx context.Context, request Request) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	input := request.Input
	if len(input) == 0 {
		var err error
		input, err = s.store.Get(request.InputRoot)
		if err != nil {
			return Response{}, ErrInputRoot
		}
	}
	if InputRoot(input) != request.InputRoot {
		return Response{}, ErrInputRoot
	}
	s.mu.RLock()
	executor := s.executors[request.Capability]
	s.mu.RUnlock()
	if executor == nil {
		return Response{}, ErrExecutor
	}
	output, err := executor.Execute(ctx, append([]byte(nil), input...), append([]byte(nil), request.Parameters...))
	if err != nil {
		return Response{}, err
	}
	root := ResultRoot(output)
	if err := s.store.Put(root, output); err != nil {
		return Response{}, err
	}
	response := Response{JobID: request.JobID, ResultRoot: root, Stored: true}
	if len(output) <= MaxInlineOutput {
		response.Output = append([]byte(nil), output...)
	}
	return response, nil
}

func (r Request) Validate() error {
	capability := strings.TrimSpace(r.Capability)
	if types.IsZero32([32]byte(r.JobID)) || types.IsZero32([32]byte(r.WorkloadHash)) || types.IsZero32([32]byte(r.InputRoot)) || capability == "" || len(capability) > MaxCapabilityBytes || len(r.Input) > MaxInlineInput || len(r.Parameters) > MaxParameters {
		return ErrRequest
	}
	if len(r.Input) > 0 && InputRoot(r.Input) != r.InputRoot {
		return ErrInputRoot
	}
	return nil
}

func (r Request) MarshalBinary() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Fixed(r.JobID[:])
	w.Fixed(r.WorkloadHash[:])
	w.Fixed(r.InputRoot[:])
	w.String(strings.TrimSpace(r.Capability))
	w.Bytes(r.Input)
	w.Bytes(r.Parameters)
	return w.BytesCopy(), nil
}

func ParseRequest(data []byte) (Request, error) {
	r := codec.NewReader(data)
	jobRaw, err := r.Fixed(32)
	if err != nil {
		return Request{}, ErrRequest
	}
	workloadRaw, err := r.Fixed(32)
	if err != nil {
		return Request{}, ErrRequest
	}
	inputRootRaw, err := r.Fixed(32)
	if err != nil {
		return Request{}, ErrRequest
	}
	capability, err := r.String(MaxCapabilityBytes)
	if err != nil {
		return Request{}, ErrRequest
	}
	input, err := r.Bytes(MaxInlineInput)
	if err != nil {
		return Request{}, ErrRequest
	}
	parameters, err := r.Bytes(MaxParameters)
	if err != nil || r.Done() != nil {
		return Request{}, ErrRequest
	}
	var jobID types.JobID
	var workload, inputRoot types.Hash
	copy(jobID[:], jobRaw)
	copy(workload[:], workloadRaw)
	copy(inputRoot[:], inputRootRaw)
	request := Request{JobID: jobID, WorkloadHash: workload, InputRoot: inputRoot, Capability: capability, Input: input, Parameters: parameters}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

func (r Response) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(r.JobID)) || types.IsZero32([32]byte(r.ResultRoot)) || len(r.Output) > MaxInlineOutput {
		return nil, ErrOutput
	}
	if len(r.Output) > 0 && ResultRoot(r.Output) != r.ResultRoot {
		return nil, ErrOutput
	}
	var w codec.Writer
	w.Fixed(r.JobID[:])
	w.Fixed(r.ResultRoot[:])
	w.Bool(r.Stored)
	w.Bytes(r.Output)
	return w.BytesCopy(), nil
}

func ParseResponse(data []byte) (Response, error) {
	r := codec.NewReader(data)
	jobRaw, err := r.Fixed(32)
	if err != nil {
		return Response{}, ErrOutput
	}
	rootRaw, err := r.Fixed(32)
	if err != nil {
		return Response{}, ErrOutput
	}
	stored, err := r.Bool()
	if err != nil {
		return Response{}, ErrOutput
	}
	output, err := r.Bytes(MaxInlineOutput)
	if err != nil || r.Done() != nil {
		return Response{}, ErrOutput
	}
	var jobID types.JobID
	var resultRoot types.Hash
	copy(jobID[:], jobRaw)
	copy(resultRoot[:], rootRaw)
	response := Response{JobID: jobID, ResultRoot: resultRoot, Stored: stored, Output: output}
	if _, err := response.MarshalBinary(); err != nil {
		return Response{}, err
	}
	return response, nil
}

func InputRoot(input []byte) types.Hash {
	return types.Hash(codec.DomainHash("zephyr/compute/input/v2", input))
}

func ResultRoot(output []byte) types.Hash {
	return types.Hash(codec.DomainHash("zephyr/compute/result/v2", output))
}

type HashExecutor struct{}

func (HashExecutor) Capability() string { return "sha256" }
func (HashExecutor) Execute(_ context.Context, input, _ []byte) ([]byte, error) {
	hash := sha256.Sum256(input)
	return hash[:], nil
}

type IdentityExecutor struct{}

func (IdentityExecutor) Capability() string { return "identity" }
func (IdentityExecutor) Execute(_ context.Context, input, _ []byte) ([]byte, error) {
	return append([]byte(nil), input...), nil
}
