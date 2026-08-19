package node

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestTwoShardTransferFinalizesReceiptThenImportsOnce(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("two-shard")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	aliceKey, alice := accountOnShard(t, 0, 2)
	_, bob := accountOnShard(t, 1, 2)
	shard0 := worldstate.NewMemory()
	shard1 := worldstate.NewMemory()

	inputID := types.ObjectIDForShard(types.HashBytes("genesis", []byte("alice-coin")), 0, 0)
	inputOut, err := object.NewCoinOutput(alice, native, 100)
	if err != nil {
		t.Fatal(err)
	}
	input := object.Object{ID: inputID, Version: 1, Owner: alice, Kind: inputOut.Kind, Data: inputOut.Data}
	root0, err := shard0.Apply(nil, []object.Object{input})
	if err != nil {
		t.Fatal(err)
	}
	witness, proof, ok := shard0.Proof(inputID)
	if !ok {
		t.Fatal("missing source witness")
	}
	witnessHash := witness.Hash()
	payment, _ := object.NewCoinOutput(bob, native, 25)
	change, _ := object.NewCoinOutput(alice, native, 74)
	transaction := tx.Transaction{
		Version: tx.Version, Network: network, ShardID: 0, StateRoot: root0,
		Inputs:  []tx.InputRef{{ObjectID: inputID, Version: 1, ObjectHash: witnessHash}},
		Outputs: []object.OutputSpec{payment, change}, Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Fee: 1, Witnesses: []tx.Witness{{Object: witness, Proof: proof}},
	}
	transaction.Salt[0] = 1
	if err := transaction.Sign(aliceKey); err != nil {
		t.Fatal(err)
	}

	validatorKey, validators := singleValidatorSet(t, network)
	validatorRoot := types.HashBytes("validators", []byte("single"))
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: shard0, 1: shard1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	candidate1, err := runtime.BuildCandidate(1, map[uint32]ShardBatch{0: {Transactions: []tx.Transaction{transaction}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate1.Receipts[0]) != 1 || len(candidate1.Results[0]) != 1 || len(candidate1.Results[0][0].Outbound) != 1 {
		t.Fatalf("expected one cross-shard receipt: %+v", candidate1.Results[0])
	}
	if shard1.Root() != candidate1.Commitments[1].StateRoot {
		t.Fatal("destination state changed before receipt import")
	}
	proposal1, certificate1 := certifyCandidate(t, validators, validatorKey, candidate1)
	_ = proposal1
	finalized1, err := runtime.Commit(candidate1, certificate1, validators)
	if err != nil {
		t.Fatal(err)
	}

	receipt := candidate1.Receipts[0][0]
	commitment, commitmentProof, err := sharding.CommitmentProof(candidate1.Commitments, 0)
	if err != nil {
		t.Fatal(err)
	}
	receiptProof, err := (sharding.ReceiptBatch{Receipts: candidate1.Receipts[0]}).Proof(receipt)
	if err != nil {
		t.Fatal(err)
	}
	importReceipt := ReceiptImport{
		Header: finalized1, Certificate: certificate1, Validators: validators,
		Commitment: commitment, CommitmentProof: commitmentProof,
		Receipt: receipt, ReceiptProof: receiptProof,
	}

	root1Before := shard1.Root()
	candidate2, err := runtime.BuildCandidate(2, map[uint32]ShardBatch{1: {Imports: []ReceiptImport{importReceipt}}})
	if err != nil {
		t.Fatal(err)
	}
	if shard1.Root() != root1Before {
		t.Fatal("receipt import mutated state before destination QC")
	}
	_, certificate2 := certifyCandidate(t, validators, validatorKey, candidate2)
	if _, err := runtime.Commit(candidate2, certificate2, validators); err != nil {
		t.Fatal(err)
	}
	destinationObject, err := receipt.DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shard1.GetObject(destinationObject.ID); !ok {
		t.Fatal("destination coin was not materialized")
	}
	marker, err := sharding.ReceiptMarker(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shard1.GetObject(marker.ID); !ok {
		t.Fatal("durable receipt marker was not committed")
	}
	if _, err := runtime.BuildCandidate(3, map[uint32]ShardBatch{1: {Imports: []ReceiptImport{importReceipt}}}); err != sharding.ErrReceiptReplay {
		t.Fatalf("expected durable receipt replay rejection, got %v", err)
	}
}

func accountOnShard(t *testing.T, shard, shardCount uint32) (*ecdsa.PrivateKey, types.AccountID) {
	t.Helper()
	for i := 0; i < 10_000; i++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		account := types.AccountIDFromPublicKey(pub)
		if types.AccountShard(account, shardCount) == shard {
			return key, account
		}
	}
	t.Fatal("could not generate account on requested shard")
	return nil, types.AccountID{}
}

func singleValidatorSet(t *testing.T, network types.NetworkID) (*ecdsa.PrivateKey, v2consensus.ValidatorSet) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	id := types.ValidatorIDFromPublicKey(pub)
	return key, v2consensus.ValidatorSet{Network: network, Validators: []v2consensus.Validator{{ID: id, PublicKey: pub, Power: 10}}}
}

func certifyCandidate(t *testing.T, validators v2consensus.ValidatorSet, key *ecdsa.PrivateKey, candidate Candidate) (v2consensus.Proposal, v2consensus.Certificate) {
	t.Helper()
	proposal, err := v2consensus.SignProposal(key, candidate.Header, 0)
	if err != nil {
		t.Fatal(err)
	}
	headerHash := v2consensus.HeaderConsensusHash(candidate.Header)
	vote, err := v2consensus.SignVote(key, candidate.Header.Network, candidate.Header.Height, 0, headerHash)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := validators.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	return proposal, certificate
}
