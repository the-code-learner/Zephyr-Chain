package genesis

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestTrustAnchorDerivesNetworkAndValidatorRootFromGenesis(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	validatorID := types.ValidatorIDFromPublicKey(pub)
	owner := types.AccountIDFromPublicKey([]byte("genesis-owner"))
	config := Config{
		Version: ProtocolVersion, ChainName: "zephyr-trust-test", GenesisUnix: 1,
		InitialShardCount: 1, MaxShardCount: 16, NativeSymbol: "ZPH",
		Validators:  []Validator{{ID: validatorID, ConsensusPublicKey: pub, VotingPower: 10}},
		Allocations: []Allocation{{Owner: owner, Amount: 100}},
	}
	anchor, err := config.TrustAnchor()
	if err != nil {
		t.Fatal(err)
	}
	network, err := config.NetworkID()
	if err != nil {
		t.Fatal(err)
	}
	set, err := config.ValidatorSet()
	if err != nil {
		t.Fatal(err)
	}
	root, err := set.Root()
	if err != nil {
		t.Fatal(err)
	}
	if anchor.Network != network || anchor.ValidatorRoot != root || types.IsZero32([32]byte(anchor.ValidatorRoot)) {
		t.Fatalf("unexpected trust anchor: %+v", anchor)
	}
}

func TestTrustAnchorRejectsNonP256GenesisValidator(t *testing.T) {
	pub := []byte("not-a-p256-public-key")
	config := Config{
		Version: ProtocolVersion, ChainName: "zephyr-invalid-trust", GenesisUnix: 1,
		InitialShardCount: 1, MaxShardCount: 1, NativeSymbol: "ZPH",
		Validators: []Validator{{ID: types.ValidatorIDFromPublicKey(pub), ConsensusPublicKey: pub, VotingPower: 1}},
	}
	if _, err := config.TrustAnchor(); err != ErrValidator {
		t.Fatalf("expected invalid genesis trust anchor, got %v", err)
	}
}
