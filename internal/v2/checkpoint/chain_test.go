package checkpoint

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestCheckpointChainTracksCertifiedValidatorRotation(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("checkpoint-chain")))
	currentKey, current := testValidatorSet(t, network, 10)
	_, next := testValidatorSet(t, network, 20)
	currentRoot, err := current.Root()
	if err != nil {
		t.Fatal(err)
	}
	nextRoot, err := next.Root()
	if err != nil {
		t.Fatal(err)
	}
	chain, err := New(network, current)
	if err != nil {
		t.Fatal(err)
	}

	header := sharding.GlobalHeader{
		Version:             2,
		Network:             network,
		Height:              1,
		ShardCommitmentRoot: types.HashBytes("shards", []byte("one")),
		ValidatorRoot:       currentRoot,
		NextValidatorRoot:   nextRoot,
		DataRoot:            types.HashBytes("data", []byte("one")),
	}
	proposal, err := v2consensus.SignProposal(currentKey, header, 0)
	if err != nil {
		t.Fatal(err)
	}
	vote, err := v2consensus.SignVote(currentKey, network, 1, 0, v2consensus.HeaderConsensusHash(header))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := current.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	header.CertificateHash = certificate.Hash()
	if err := chain.Append(header, certificate, current, &next); err != nil {
		t.Fatal(err)
	}
	if chain.Height() != 1 || chain.CurrentRoot() != nextRoot {
		t.Fatal("checkpoint chain did not advance validator trust root")
	}
	if err := chain.VerifyEntry(1); err != nil {
		t.Fatal(err)
	}
	if historical, ok := chain.ValidatorSet(currentRoot); !ok || historical.Network != network {
		t.Fatal("historical validator set unavailable")
	}
	if future, ok := chain.ValidatorSet(nextRoot); !ok || future.Network != network {
		t.Fatal("next validator set unavailable")
	}
}

func TestCheckpointChainRejectsUncertifiedRotation(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("checkpoint-reject")))
	key, current := testValidatorSet(t, network, 10)
	_, attacker := testValidatorSet(t, network, 10)
	currentRoot, _ := current.Root()
	attackerRoot, _ := attacker.Root()
	chain, err := New(network, current)
	if err != nil {
		t.Fatal(err)
	}
	header := sharding.GlobalHeader{
		Version:             2,
		Network:             network,
		Height:              1,
		ShardCommitmentRoot: types.HashBytes("shards", []byte("reject")),
		ValidatorRoot:       currentRoot,
		NextValidatorRoot:   attackerRoot,
		DataRoot:            types.HashBytes("data", []byte("reject")),
	}
	proposal, err := v2consensus.SignProposal(key, header, 0)
	if err != nil {
		t.Fatal(err)
	}
	vote, err := v2consensus.SignVote(key, network, 1, 0, v2consensus.HeaderConsensusHash(header))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := current.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	header.CertificateHash = certificate.Hash()
	if err := chain.Append(header, certificate, current, nil); err != ErrCheckpointValidators {
		t.Fatalf("expected uncertified next-set rejection, got %v", err)
	}
}

func testValidatorSet(t *testing.T, network types.NetworkID, power uint64) (*ecdsa.PrivateKey, v2consensus.ValidatorSet) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	id := types.ValidatorIDFromPublicKey(pub)
	return key, v2consensus.ValidatorSet{Network: network, Validators: []v2consensus.Validator{{ID: id, PublicKey: pub, Power: power}}}
}
