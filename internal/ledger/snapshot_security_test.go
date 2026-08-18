package ledger

import (
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestRestoreQuorumSnapshotRequiresValidatorQuorumAndRejectsTampering(t *testing.T) {
	first := newConsensusSigner(t)
	second := newConsensusSigner(t)
	validators := []dpos.Validator{
		{Rank: 1, Address: first.address, VotingPower: 60, SelfStake: 60},
		{Rank: 2, Address: second.address, VotingPower: 40, SelfStake: 40},
	}
	producer := newTestStore(t)
	if _, err := producer.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatal(err)
	}
	envelope := signedEnvelope(t, 25, 1, "quorum-snapshot")
	if _, err := producer.Credit(envelope.From, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := producer.Accept(envelope); err != nil {
		t.Fatal(err)
	}
	if _, err := producer.ProduceBlock(10); err != nil {
		t.Fatal(err)
	}
	snapshot := producer.Snapshot()

	makeProof := func(signer consensusSigner) SnapshotProof {
		proof, err := BuildSnapshotProofTemplate(snapshot, protocol.DefaultChainID, signer.address)
		if err != nil {
			t.Fatal(err)
		}
		proof.PublicKey = signer.publicKey
		proof.Payload = proof.CanonicalPayload()
		proof.Signature, err = tx.SignPayload(signer.privateKey, proof.Payload)
		if err != nil {
			t.Fatal(err)
		}
		return proof
	}
	firstProof := makeProof(first)
	secondProof := makeProof(second)

	replica := newTestStore(t)
	if _, err := replica.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatal(err)
	}
	trusted := replica.ValidatorSet()
	if err := replica.RestoreQuorumSnapshot(snapshot, protocol.DefaultChainID, []SnapshotProof{firstProof}, trusted, time.Now().UTC()); err != ErrSnapshotQuorumRequired {
		t.Fatalf("expected 60%% proof to miss a 2/3 quorum, got %v", err)
	}
	if err := replica.RestoreQuorumSnapshot(snapshot, protocol.DefaultChainID, []SnapshotProof{firstProof, secondProof}, trusted, time.Now().UTC()); err != nil {
		t.Fatalf("expected quorum snapshot restore, got %v", err)
	}
	if got := replica.View(envelope.From); got.Balance != 75 || got.Nonce != 1 {
		t.Fatalf("unexpected restored account %+v", got)
	}

	tampered := snapshot
	tampered.Accounts = cloneAccounts(snapshot.Accounts)
	account := tampered.Accounts[envelope.From]
	account.Balance++
	tampered.Accounts[envelope.From] = account
	if err := ValidateSnapshotQuorum(tampered, protocol.DefaultChainID, []SnapshotProof{firstProof, secondProof}, trusted); err == nil {
		t.Fatal("expected tampered account state to invalidate snapshot commitment")
	}
}
