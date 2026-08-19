package genesis

import (
	"bytes"
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const ProtocolVersion uint16 = 2

var (
	ErrProtocolVersion = errors.New("genesis protocol version must be 2")
	ErrChainName       = errors.New("genesis chain name is required")
	ErrShardConfig     = errors.New("invalid genesis shard configuration")
	ErrValidator       = errors.New("invalid genesis validator")
	ErrAllocation      = errors.New("invalid genesis allocation")
	ErrVotingPower     = errors.New("genesis voting power overflow")
)

type Validator struct {
	ID                 types.ValidatorID
	ConsensusPublicKey []byte
	VotingPower        uint64
}

type Allocation struct {
	Owner  types.AccountID
	Amount uint64
}

type Config struct {
	Version           uint16
	ChainName         string
	GenesisUnix       uint64
	InitialShardCount uint32
	MaxShardCount     uint32
	NativeSymbol      string
	Validators        []Validator
	Allocations       []Allocation
}

func (g Config) Validate() error {
	if g.Version != ProtocolVersion {
		return ErrProtocolVersion
	}
	if strings.TrimSpace(g.ChainName) == "" {
		return ErrChainName
	}
	if g.InitialShardCount == 0 || g.MaxShardCount < g.InitialShardCount {
		return ErrShardConfig
	}
	symbol := strings.TrimSpace(g.NativeSymbol)
	if symbol == "" || len(symbol) > 16 {
		return ErrAllocation
	}

	seenValidators := map[types.ValidatorID]struct{}{}
	var total uint64
	for _, v := range g.Validators {
		if types.IsZero32([32]byte(v.ID)) || len(v.ConsensusPublicKey) == 0 || v.VotingPower == 0 {
			return ErrValidator
		}
		if _, ok := seenValidators[v.ID]; ok {
			return ErrValidator
		}
		seenValidators[v.ID] = struct{}{}
		if math.MaxUint64-total < v.VotingPower {
			return ErrVotingPower
		}
		total += v.VotingPower
	}
	if len(g.Validators) == 0 || total == 0 {
		return ErrValidator
	}

	seenAllocations := map[types.AccountID]struct{}{}
	for _, a := range g.Allocations {
		if types.IsZero32([32]byte(a.Owner)) || a.Amount == 0 {
			return ErrAllocation
		}
		if _, ok := seenAllocations[a.Owner]; ok {
			return ErrAllocation
		}
		seenAllocations[a.Owner] = struct{}{}
	}
	return nil
}

func (g Config) CanonicalBytes() ([]byte, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}

	validators := append([]Validator(nil), g.Validators...)
	sort.Slice(validators, func(i, j int) bool {
		return bytes.Compare(validators[i].ID[:], validators[j].ID[:]) < 0
	})
	allocations := append([]Allocation(nil), g.Allocations...)
	sort.Slice(allocations, func(i, j int) bool {
		return bytes.Compare(allocations[i].Owner[:], allocations[j].Owner[:]) < 0
	})

	var w codec.Writer
	w.U16(g.Version)
	w.String(strings.TrimSpace(g.ChainName))
	w.U64(g.GenesisUnix)
	w.U32(g.InitialShardCount)
	w.U32(g.MaxShardCount)
	w.String(strings.TrimSpace(g.NativeSymbol))
	w.U32(uint32(len(validators)))
	for _, v := range validators {
		w.Fixed(v.ID[:])
		w.Bytes(v.ConsensusPublicKey)
		w.U64(v.VotingPower)
	}
	w.U32(uint32(len(allocations)))
	for _, a := range allocations {
		w.Fixed(a.Owner[:])
		w.U64(a.Amount)
	}
	return w.BytesCopy(), nil
}

func (g Config) NetworkID() (types.NetworkID, error) {
	payload, err := g.CanonicalBytes()
	if err != nil {
		return types.NetworkID{}, err
	}
	return types.NetworkID(codec.DomainHash("zephyr/genesis/v2", payload)), nil
}

func (g Config) TotalVotingPower() (uint64, error) {
	if err := g.Validate(); err != nil {
		return 0, err
	}
	var total uint64
	for _, v := range g.Validators {
		if math.MaxUint64-total < v.VotingPower {
			return 0, ErrVotingPower
		}
		total += v.VotingPower
	}
	return total, nil
}
