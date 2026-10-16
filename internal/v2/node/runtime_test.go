package node

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCandidateDoesNotMutateBeforeQCAndCommitsAfterQC(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("node-runtime")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	stateStore := worldstate.NewMemory()

	validatorKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	validatorPub := elliptic.Marshal(elliptic.P256(), validatorKey.PublicKey.X, validatorKey.PublicKey.Y)
	validatorID := types.ValidatorIDFromPublicKey(validatorPub)
	validators := v2consensus.ValidatorSet{Network: network, Validators: []v2consensus.Validator{{ID: validatorID, PublicKey: validatorPub, Power: 10}}}
	validatorRoot, err := validators.Root()
	if err != nil {
		t.Fatal(err)
	}

	aliceKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	alicePub := elliptic.Marshal(elliptic.P256(), aliceKey.PublicKey.X, aliceKey.PublicKey.Y)
	alice := types.AccountIDFromPublicKey(alicePub)
	bob := types.AccountIDFromPublicKey([]byte("bob"))
	inputID := types.ObjectIDFromTransaction(types.HashBytes("genesis", []byte("coin")), 0)
	inputOut, _ := object.NewCoinOutput(alice, native, 100)
	input := object.Object{ID: inputID, Version: 1, Owner: alice, Kind: inputOut.Kind, Data: inputOut.Data}
	root, err := stateStore.Apply(nil, []object.Object{input})
	if err != nil {
		t.Fatal(err)
	}
	witness, proof, ok := stateStore.Proof(inputID)
	if !ok {
		t.Fatal("missing genesis input")
	}
	witnessHash := witness.Hash()
	toBob, _ := object.NewCoinOutput(bob, native, 25)
	change, _ := object.NewCoinOutput(alice, native, 74)
	transaction := tx.Transaction{
		Version: tx.Version, Network: network, ShardID: 0, StateRoot: root,
		Inputs:  []tx.InputRef{{ObjectID: inputID, Version: 1, ObjectHash: witnessHash}},
		Outputs: []object.OutputSpec{toBob, change}, Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Fee: 1, Witnesses: []tx.Witness{{Object: witness, Proof: proof}},
	}
	transaction.Salt[0] = 1
	if err := transaction.Sign(aliceKey); err != nil {
		t.Fatal(err)
	}

	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: stateStore}, 2)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := runtime.BuildCandidate(1, map[uint32]ShardBatch{0: {Transactions: []tx.Transaction{transaction}}})
	if err != nil {
		t.Fatal(err)
	}
	if stateStore.Root() != root {
		t.Fatal("candidate simulation mutated committed state before QC")
	}
	if candidate.Commitments[0].StateRoot == root {
		t.Fatal("candidate did not calculate a new state root")
	}

	proposal, err := v2consensus.SignProposal(validatorKey, candidate.Header, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := validators.VerifyProposal(proposal); err != nil {
		t.Fatal(err)
	}
	headerHash := v2consensus.HeaderConsensusHash(candidate.Header)
	vote, err := v2consensus.SignVote(validatorKey, network, 1, 0, headerHash)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := validators.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := runtime.Commit(candidate, certificate, validators)
	if err != nil {
		t.Fatal(err)
	}
	if types.IsZero32([32]byte(finalized.CertificateHash)) || runtime.Height != 1 || stateStore.Root() != candidate.Commitments[0].StateRoot {
		t.Fatal("QC commit did not finalize candidate state")
	}
}
