package contracts

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrContractWire = errors.New("invalid contract operation wire payload")

type StoredContract struct {
	ID         types.ContractID
	Deployment Deployment
}

func ParseDeployment(data []byte) (Deployment, error) {
	r := codec.NewReader(data)
	runtimeName, err := r.String(64)
	if err != nil {
		return Deployment{}, ErrContractWire
	}
	code, err := r.Bytes(MaxModuleBytes)
	if err != nil {
		return Deployment{}, ErrContractWire
	}
	abi, err := r.U16()
	if err != nil {
		return Deployment{}, ErrContractWire
	}
	authorityRaw, err := r.Fixed(32)
	if err != nil {
		return Deployment{}, ErrContractWire
	}
	var authority types.AccountID
	copy(authority[:], authorityRaw)
	initial, err := r.Bytes(MaxInitialStateBytes)
	if err != nil {
		return Deployment{}, ErrContractWire
	}
	pages, err := r.U32()
	if err != nil || r.Done() != nil {
		return Deployment{}, ErrContractWire
	}
	deployment := Deployment{Runtime: runtimeName, Code: code, ABI: abi, UpgradeAuthority: authority, InitialState: initial, MaxMemoryPages: pages}
	if err := deployment.Validate(); err != nil {
		return Deployment{}, err
	}
	return deployment, nil
}

func (s StoredContract) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(s.ID)) {
		return nil, ErrContractWire
	}
	deployment, err := s.Deployment.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Fixed(s.ID[:])
	w.Bytes(deployment)
	return w.BytesCopy(), nil
}

func ParseStoredContract(data []byte) (StoredContract, error) {
	r := codec.NewReader(data)
	idRaw, err := r.Fixed(32)
	if err != nil {
		return StoredContract{}, ErrContractWire
	}
	var id types.ContractID
	copy(id[:], idRaw)
	deploymentRaw, err := r.Bytes(MaxModuleBytes + MaxInitialStateBytes + 256)
	if err != nil || r.Done() != nil {
		return StoredContract{}, ErrContractWire
	}
	deployment, err := ParseDeployment(deploymentRaw)
	if err != nil || types.IsZero32([32]byte(id)) {
		return StoredContract{}, ErrContractWire
	}
	return StoredContract{ID: id, Deployment: deployment}, nil
}

type Call struct {
	ContractObject types.ObjectID
	Entrypoint     string
	Arguments      []byte
	FuelLimit      uint64
	Accesses       []Access
}

func (c Call) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(c.ContractObject)) || c.Entrypoint == "" || len(c.Entrypoint) > 128 || len(c.Arguments) > MaxArgumentsBytes || c.FuelLimit == 0 || len(c.Accesses) > MaxAccesses {
		return nil, ErrContractWire
	}
	var w codec.Writer
	w.Fixed(c.ContractObject[:])
	w.String(c.Entrypoint)
	w.Bytes(c.Arguments)
	w.U64(c.FuelLimit)
	w.U32(uint32(len(c.Accesses)))
	seen := make(map[types.ObjectID]struct{}, len(c.Accesses))
	for _, access := range c.Accesses {
		if types.IsZero32([32]byte(access.ObjectID)) {
			return nil, ErrContractWire
		}
		if _, ok := seen[access.ObjectID]; ok {
			return nil, ErrContractWire
		}
		seen[access.ObjectID] = struct{}{}
		w.Fixed(access.ObjectID[:])
		w.Bool(access.Write)
	}
	return w.BytesCopy(), nil
}

func ParseCall(data []byte) (Call, error) {
	r := codec.NewReader(data)
	contractRaw, err := r.Fixed(32)
	if err != nil {
		return Call{}, ErrContractWire
	}
	var contractObject types.ObjectID
	copy(contractObject[:], contractRaw)
	entry, err := r.String(128)
	if err != nil {
		return Call{}, ErrContractWire
	}
	args, err := r.Bytes(MaxArgumentsBytes)
	if err != nil {
		return Call{}, ErrContractWire
	}
	fuel, err := r.U64()
	if err != nil {
		return Call{}, ErrContractWire
	}
	count, err := r.U32()
	if err != nil || count > MaxAccesses {
		return Call{}, ErrContractWire
	}
	accesses := make([]Access, int(count))
	for i := range accesses {
		raw, err := r.Fixed(32)
		if err != nil {
			return Call{}, ErrContractWire
		}
		copy(accesses[i].ObjectID[:], raw)
		accesses[i].Write, err = r.Bool()
		if err != nil {
			return Call{}, ErrContractWire
		}
	}
	if r.Done() != nil {
		return Call{}, ErrContractWire
	}
	call := Call{ContractObject: contractObject, Entrypoint: entry, Arguments: args, FuelLimit: fuel, Accesses: accesses}
	if _, err := call.MarshalBinary(); err != nil {
		return Call{}, err
	}
	return call, nil
}

type Receipt struct {
	ContractID types.ContractID
	FuelUsed   uint64
	ReturnHash types.Hash
	EventRoot  types.Hash
}

func (r Receipt) MarshalBinary() []byte {
	var w codec.Writer
	w.Fixed(r.ContractID[:])
	w.U64(r.FuelUsed)
	w.Fixed(r.ReturnHash[:])
	w.Fixed(r.EventRoot[:])
	return w.BytesCopy()
}
