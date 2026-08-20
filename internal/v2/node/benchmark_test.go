package node

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

const benchmarkBatchSize = 32

type benchmarkSigner struct {
	key     *ecdsa.PrivateKey
	account types.AccountID
}

func BenchmarkV2FinalizedBatch32(b *testing.B) {
	for _, workers := range []int{1, 4, 8, 16} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			benchmarkFinalizedBatch(b, workers)
		})
	}
}

func benchmarkFinalizedBatch(b *testing.B, workers int) {
	b.Helper()
	b.ReportAllocs()
	b.StopTimer()

	network := types.NetworkID(types.HashBytes("network", []byte("benchmark-v2")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	validators, validatorKeys := benchmarkValidators(b, network, 7)
	validatorRoot, err := validators.Root()
	if err != nil {
		b.Fatal(err)
	}
	signers := make([]benchmarkSigner, benchmarkBatchSize)
	for i := range signers {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			b.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		signers[i] = benchmarkSigner{key: key, account: types.AccountIDFromPublicKey(pub)}
	}
	recipient := types.AccountIDFromPublicKey([]byte("benchmark-recipient"))

	for iteration := 0; iteration < b.N; iteration++ {
		store := worldstate.NewMemory()
		inputs := make([]object.Object, benchmarkBatchSize)
		for i, signer := range signers {
			seed := types.HashBytes("benchmark-input", []byte(fmt.Sprintf("%d/%d", iteration, i)))
			id := types.ObjectIDFromTransaction(seed, 0)
			out, err := object.NewCoinOutput(signer.account, native, 100)
			if err != nil {
				b.Fatal(err)
			}
			inputs[i] = object.Object{ID: id, Version: 1, Owner: signer.account, Kind: out.Kind, Data: out.Data}
		}
		root, err := store.Apply(nil, inputs)
		if err != nil {
			b.Fatal(err)
		}
		transactions := make([]tx.Transaction, benchmarkBatchSize)
		for i, signer := range signers {
			witness, proof, ok := store.Proof(inputs[i].ID)
			if !ok {
				b.Fatal("missing benchmark witness")
			}
			witnessHash := witness.Hash()
			payment, _ := object.NewCoinOutput(recipient, native, 25)
			change, _ := object.NewCoinOutput(signer.account, native, 74)
			transaction := tx.Transaction{
				Version: tx.Version, Network: network, ShardID: 0, StateRoot: root,
				Inputs:  []tx.InputRef{{ObjectID: inputs[i].ID, Version: 1, ObjectHash: witnessHash}},
				Outputs: []object.OutputSpec{payment, change}, Operations: []tx.Operation{{Kind: tx.OpTransfer}},
				Fee: 1, Witnesses: []tx.Witness{{Object: witness, Proof: proof}},
			}
			transaction.Salt[0] = byte(i + 1)
			transaction.Salt[1] = byte(iteration)
			if err := transaction.Sign(signer.key); err != nil {
				b.Fatal(err)
			}
			transactions[i] = transaction
		}
		runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, workers)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		candidate, err := runtime.BuildCandidate(1, map[uint32]ShardBatch{0: {Transactions: transactions}})
		if err != nil {
			b.Fatal(err)
		}
		proposer, err := validators.Proposer(1, 0)
		if err != nil {
			b.Fatal(err)
		}
		proposal, err := v2consensus.SignProposal(validatorKeys[proposer.ID], candidate.Header, 0)
		if err != nil {
			b.Fatal(err)
		}
		headerHash := v2consensus.HeaderConsensusHash(candidate.Header)
		votes := make([]v2consensus.Vote, 0, 5)
		for _, validator := range validators.Validators[:5] {
			vote, err := v2consensus.SignVote(validatorKeys[validator.ID], network, 1, 0, headerHash)
			if err != nil {
				b.Fatal(err)
			}
			votes = append(votes, vote)
		}
		certificate, err := validators.BuildCertificate(proposal, votes)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := runtime.Commit(candidate, certificate, validators); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
	}

	if elapsed := b.Elapsed().Seconds(); elapsed > 0 {
		b.ReportMetric(float64(b.N*benchmarkBatchSize)/elapsed, "finalized-tx/s")
	}
}

func benchmarkValidators(b *testing.B, network types.NetworkID, count int) (v2consensus.ValidatorSet, map[types.ValidatorID]*ecdsa.PrivateKey) {
	b.Helper()
	set := v2consensus.ValidatorSet{Network: network, Validators: make([]v2consensus.Validator, 0, count)}
	keys := make(map[types.ValidatorID]*ecdsa.PrivateKey, count)
	for i := 0; i < count; i++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			b.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		id := types.ValidatorIDFromPublicKey(pub)
		set.Validators = append(set.Validators, v2consensus.Validator{ID: id, PublicKey: pub, Power: 10_000})
		keys[id] = key
	}
	return set, keys
}
