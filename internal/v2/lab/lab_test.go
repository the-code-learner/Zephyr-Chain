package lab

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/node"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

type cluster struct {
	network    types.NetworkID
	native     types.TokenID
	validators v2consensus.ValidatorSet
	keys       map[types.ValidatorID]*ecdsa.PrivateKey
	runtimes   []*node.Runtime
	candidates []node.Candidate
}

func TestV2LabSevenValidatorsCertifiedHappyPath(t *testing.T) {
	c := newCluster(t, 7)
	proposal := c.proposal(t)
	votes := c.votes(t, proposal, []int{0, 1, 2, 3, 4})
	certificate, err := c.validators.BuildCertificate(proposal, votes)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.runtimes {
		if _, err := c.runtimes[i].Commit(c.candidates[i], certificate, c.validators); err != nil {
			t.Fatalf("validator %d failed certified commit: %v", i, err)
		}
		if c.runtimes[i].Height != 1 {
			t.Fatalf("validator %d did not finalize height 1", i)
		}
	}
}

func TestV2LabFourThreePartitionStallsThenHeals(t *testing.T) {
	c := newCluster(t, 7)
	proposal := c.proposal(t)
	left := c.votes(t, proposal, []int{0, 1, 2, 3})
	right := c.votes(t, proposal, []int{4, 5, 6})
	if _, err := c.validators.BuildCertificate(proposal, left); !errors.Is(err, v2consensus.ErrInsufficientPower) {
		t.Fatalf("4/3 left side unexpectedly reached quorum: %v", err)
	}
	if _, err := c.validators.BuildCertificate(proposal, right); !errors.Is(err, v2consensus.ErrInsufficientPower) {
		t.Fatalf("4/3 right side unexpectedly reached quorum: %v", err)
	}
	for i, runtime := range c.runtimes {
		if runtime.Height != 0 {
			t.Fatalf("validator %d committed while partitioned", i)
		}
	}

	healed := append(append([]v2consensus.Vote{}, left...), right[0])
	certificate, err := c.validators.BuildCertificate(proposal, healed)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.runtimes {
		if _, err := c.runtimes[i].Commit(c.candidates[i], certificate, c.validators); err != nil {
			t.Fatalf("validator %d failed after partition heal: %v", i, err)
		}
	}
}

func TestV2LabFiveTwoPartitionFinalizesQuorumSideAndMinorityCatchesUp(t *testing.T) {
	c := newCluster(t, 7)
	proposal := c.proposal(t)
	quorumVotes := c.votes(t, proposal, []int{0, 1, 2, 3, 4})
	minorityVotes := c.votes(t, proposal, []int{5, 6})
	if _, err := c.validators.BuildCertificate(proposal, minorityVotes); !errors.Is(err, v2consensus.ErrInsufficientPower) {
		t.Fatalf("2-validator minority unexpectedly reached quorum: %v", err)
	}
	certificate, err := c.validators.BuildCertificate(proposal, quorumVotes)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := c.runtimes[i].Commit(c.candidates[i], certificate, c.validators); err != nil {
			t.Fatalf("quorum validator %d failed commit: %v", i, err)
		}
	}
	if c.runtimes[5].Height != 0 || c.runtimes[6].Height != 0 {
		t.Fatal("minority committed before receiving certificate")
	}
	for i := 5; i < 7; i++ {
		if _, err := c.runtimes[i].Commit(c.candidates[i], certificate, c.validators); err != nil {
			t.Fatalf("minority validator %d failed certificate catch-up: %v", i, err)
		}
	}
}

func TestV2LabConflictingProposalIsNotVoted(t *testing.T) {
	c := newCluster(t, 7)
	proposer, err := c.validators.Proposer(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	conflicting := c.candidates[0].Header
	conflicting.DataRoot = types.HashBytes("conflicting-data-root", []byte("evil"))
	proposal, err := v2consensus.SignProposal(c.keys[proposer.ID], conflicting, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.candidates {
		if err := node.VerifyProposalAgainstCandidate(c.candidates[i], proposal, c.validators); !errors.Is(err, node.ErrCandidateState) {
			t.Fatalf("validator %d accepted conflicting proposal: %v", i, err)
		}
	}
}

func newCluster(t *testing.T, validatorCount int) *cluster {
	t.Helper()
	network := types.NetworkID(types.HashBytes("network", []byte("v2-lab")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	validators := v2consensus.ValidatorSet{Network: network}
	keys := make(map[types.ValidatorID]*ecdsa.PrivateKey, validatorCount)
	for i := 0; i < validatorCount; i++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		id := types.ValidatorIDFromPublicKey(pub)
		validators.Validators = append(validators.Validators, v2consensus.Validator{ID: id, PublicKey: pub, Power: 10_000})
		keys[id] = key
	}
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
	bob := types.AccountIDFromPublicKey([]byte("v2-lab-bob"))
	inputID := types.ObjectIDFromTransaction(types.HashBytes("v2-lab", []byte("input")), 0)
	inputOut, _ := object.NewCoinOutput(alice, native, 100)
	input := object.Object{ID: inputID, Version: 1, Owner: alice, Kind: inputOut.Kind, Data: inputOut.Data}

	stores := make([]*worldstate.Memory, validatorCount)
	for i := range stores {
		stores[i] = worldstate.NewMemory()
		if _, err := stores[i].Apply(nil, []object.Object{input}); err != nil {
			t.Fatal(err)
		}
	}
	root := stores[0].Root()
	witness, proof, ok := stores[0].Proof(inputID)
	if !ok {
		t.Fatal("missing lab witness")
	}
	witnessHash := witness.Hash()
	payment, _ := object.NewCoinOutput(bob, native, 25)
	change, _ := object.NewCoinOutput(alice, native, 74)
	transaction := tx.Transaction{
		Version: tx.Version, Network: network, ShardID: 0, StateRoot: root,
		Inputs:  []tx.InputRef{{ObjectID: inputID, Version: 1, ObjectHash: witnessHash}},
		Outputs: []object.OutputSpec{payment, change}, Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Fee: 1, Witnesses: []tx.Witness{{Object: witness, Proof: proof}},
	}
	transaction.Salt[0] = 1
	if err := transaction.Sign(aliceKey); err != nil {
		t.Fatal(err)
	}

	runtimes := make([]*node.Runtime, validatorCount)
	candidates := make([]node.Candidate, validatorCount)
	for i := 0; i < validatorCount; i++ {
		runtime, err := node.NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: stores[i]}, 4)
		if err != nil {
			t.Fatal(err)
		}
		runtimes[i] = runtime
		candidate, err := runtime.BuildCandidate(1, map[uint32]node.ShardBatch{0: {Transactions: []tx.Transaction{transaction}}})
		if err != nil {
			t.Fatal(err)
		}
		candidates[i] = candidate
		if i > 0 && v2consensus.HeaderConsensusHash(candidates[i].Header) != v2consensus.HeaderConsensusHash(candidates[0].Header) {
			t.Fatalf("validator %d derived a different candidate header", i)
		}
	}
	return &cluster{network: network, native: native, validators: validators, keys: keys, runtimes: runtimes, candidates: candidates}
}

func (c *cluster) proposal(t *testing.T) v2consensus.Proposal {
	t.Helper()
	proposer, err := c.validators.Proposer(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := v2consensus.SignProposal(c.keys[proposer.ID], c.candidates[0].Header, 0)
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func (c *cluster) votes(t *testing.T, proposal v2consensus.Proposal, indices []int) []v2consensus.Vote {
	t.Helper()
	votes := make([]v2consensus.Vote, 0, len(indices))
	for _, index := range indices {
		if err := node.VerifyProposalAgainstCandidate(c.candidates[index], proposal, c.validators); err != nil {
			t.Fatal(err)
		}
		validator := c.validators.Validators[index]
		vote, err := v2consensus.SignVote(c.keys[validator.ID], c.network, proposal.Header.Height, proposal.Round, v2consensus.HeaderConsensusHash(proposal.Header))
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	return votes
}
