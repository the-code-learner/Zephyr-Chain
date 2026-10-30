package genesis

import (
	"errors"
	"math"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestGenesisRejectsInitialSupplyOverflow(t *testing.T) {
	validator := Validator{
		ID:                 types.ValidatorID{1},
		ConsensusPublicKey: []byte("validator-fixture"),
		VotingPower:        1,
	}
	base := Config{
		Version:           ProtocolVersion,
		ChainName:         "supply-overflow-check",
		GenesisUnix:       1,
		InitialShardCount: 1,
		MaxShardCount:     1,
		NativeSymbol:      "ZPH",
		Validators:        []Validator{validator},
		Allocations: []Allocation{
			{Owner: types.AccountID{1}, Amount: math.MaxUint64},
		},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("exact maximum supply should be valid: %v", err)
	}
	base.Allocations = append(base.Allocations, Allocation{Owner: types.AccountID{2}, Amount: 1})
	if err := base.Validate(); !errors.Is(err, ErrAllocationSupply) {
		t.Fatalf("overflow error = %v, want ErrAllocationSupply", err)
	}
	if _, err := base.CanonicalBytes(); !errors.Is(err, ErrAllocationSupply) {
		t.Fatalf("canonical genesis accepted overflow: %v", err)
	}
	if _, err := base.NetworkID(); !errors.Is(err, ErrAllocationSupply) {
		t.Fatalf("network ID hashed overflowed supply: %v", err)
	}
}
