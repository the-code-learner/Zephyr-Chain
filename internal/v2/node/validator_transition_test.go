package node

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestValidatorTransitionActivatesOnlyAfterCurrentQC(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("validator-transition")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	currentKey, current := transitionValidatorSet(t, network, "current")
	_, next := transitionValidatorSet(t, network, "next")
	currentRoot, err := current.Root()
	if err != nil {
		t.Fatal(err)
	}
	nextRoot, err := next.Root()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(network, native, currentRoot, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := runtime.BuildCandidate(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ScheduleValidatorTransition(&candidate, current, next); err != nil {
		t.Fatal(err)
	}
	if candidate.Header.ValidatorRoot != currentRoot || candidate.Header.NextValidatorRoot != nextRoot {
		t.Fatal("candidate did not bind current and next validator roots")
	}
	proposal, err := v2consensus.SignProposal(currentKey, candidate.Header, 0)
	if err != nil {
		t.Fatal(err)
	}
	vote, err := v2consensus.SignVote(currentKey, network, 1, 0, v2consensus.HeaderConsensusHash(candidate.Header))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := current.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := runtime.CommitWithValidatorTransition(candidate, certificate, current)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ValidatorRoot != nextRoot || finalized.EffectiveNextValidatorRoot() != nextRoot {
		t.Fatal("next validator root did not activate after QC")
	}
	if err := v2consensus.VerifyCertifiedTransition(finalized, certificate, current, next); err != nil {
		t.Fatal(err)
	}

	candidate2, err := runtime.BuildCandidate(2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if candidate2.Header.ValidatorRoot != nextRoot {
		t.Fatal("following candidate does not use transitioned validator root")
	}
	_ = merkle.Root(nil)
}

func transitionValidatorSet(t *testing.T, network types.NetworkID, label string) (*ecdsa.PrivateKey, v2consensus.ValidatorSet) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	id := types.ValidatorIDFromPublicKey(pub)
	return key, v2consensus.ValidatorSet{Network: network, Validators: []v2consensus.Validator{{ID: id, PublicKey: pub, Power: uint64(len(label) + 1)}}}
}
