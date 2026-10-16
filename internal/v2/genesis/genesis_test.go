package genesis

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestNetworkIDIsCanonicalAcrossInputOrdering(t *testing.T) {
	v1 := types.ValidatorIDFromPublicKey([]byte("validator-1"))
	v2 := types.ValidatorIDFromPublicKey([]byte("validator-2"))
	a1 := types.AccountIDFromPublicKey([]byte("account-1"))
	a2 := types.AccountIDFromPublicKey([]byte("account-2"))

	base := Config{
		Version: ProtocolVersion, ChainName: "zephyr-test", GenesisUnix: 1,
		InitialShardCount: 1, MaxShardCount: 16, NativeSymbol: "ZPH",
		Validators: []Validator{
			{ID: v1, ConsensusPublicKey: []byte("validator-1"), VotingPower: 10},
			{ID: v2, ConsensusPublicKey: []byte("validator-2"), VotingPower: 20},
		},
		Allocations: []Allocation{{Owner: a1, Amount: 10}, {Owner: a2, Amount: 20}},
	}
	reordered := base
	reordered.Validators = []Validator{base.Validators[1], base.Validators[0]}
	reordered.Allocations = []Allocation{base.Allocations[1], base.Allocations[0]}

	id1, err := base.NetworkID()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := reordered.NetworkID()
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("network id changed with ordering: %s != %s", id1, id2)
	}
}
