package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestV2QuorumCertificateRequiresTwoThirdsPlus(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("consensus-v2")))
	set := ValidatorSet{Network: network}
	keys := make(map[types.ValidatorID]*ecdsa.PrivateKey)
	for i := 0; i < 4; i++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		id := types.ValidatorIDFromPublicKey(pub)
		set.Validators = append(set.Validators, Validator{ID: id, PublicKey: pub, Power: 10})
		keys[id] = key
	}
	if QuorumPower(40) != 27 {
		t.Fatalf("unexpected quorum: %d", QuorumPower(40))
	}
	proposer, err := set.Proposer(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	header := sharding.GlobalHeader{
		Version: 2, Network: network, Height: 1,
		ShardCommitmentRoot: types.HashBytes("shards", []byte("root")),
		ValidatorRoot:       types.HashBytes("validators", []byte("root")),
		DataRoot:            types.HashBytes("data", []byte("root")),
	}
	proposal, err := SignProposal(keys[proposer.ID], header, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.VerifyProposal(proposal); err != nil {
		t.Fatal(err)
	}
	headerHash := HeaderConsensusHash(header)
	votes := make([]Vote, 0, 3)
	for i := 0; i < 3; i++ {
		validator := set.Validators[i]
		vote, err := SignVote(keys[validator.ID], network, 1, 0, headerHash)
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	if _, err := set.BuildCertificate(proposal, votes[:2]); err != ErrInsufficientPower {
		t.Fatalf("expected insufficient power, got %v", err)
	}
	certificate, err := set.BuildCertificate(proposal, votes)
	if err != nil {
		t.Fatal(err)
	}
	if types.IsZero32([32]byte(certificate.Hash())) {
		t.Fatal("certificate hash is zero")
	}
	if err := set.VerifyCertificate(certificate); err != nil {
		t.Fatal(err)
	}
}

func TestV2CertificateRejectsDuplicateVote(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("duplicate")))
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	id := types.ValidatorIDFromPublicKey(pub)
	set := ValidatorSet{Network: network, Validators: []Validator{{ID: id, PublicKey: pub, Power: 1}}}
	hash := types.HashBytes("header", []byte("h"))
	vote, err := SignVote(key, network, 1, 0, hash)
	if err != nil {
		t.Fatal(err)
	}
	certificate := Certificate{Network: network, Height: 1, HeaderHash: hash, Votes: []Vote{vote, vote}}
	if err := set.VerifyCertificate(certificate); err != ErrCertificate {
		t.Fatalf("expected duplicate vote rejection, got %v", err)
	}
}
