package ledger

import (
	"errors"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestStoreBindsDataDirectoryToConfiguredChain(t *testing.T) {
	dataDir := t.TempDir()
	chainA := "zephyr-devnet-a"
	chainB := "zephyr-devnet-b"

	store, err := NewStoreWithChainID(dataDir, chainA)
	if err != nil {
		t.Fatalf("create chain A store: %v", err)
	}
	if _, err := store.Credit("zph_sender", 1); err != nil {
		t.Fatalf("persist chain A state: %v", err)
	}

	if _, err := NewStoreWithChainID(dataDir, chainB); !errors.Is(err, ErrStateChainMismatch) {
		t.Fatalf("expected reused data directory to reject chain B, got %v", err)
	}
	if _, err := NewStoreWithChainID(dataDir, chainA); err != nil {
		t.Fatalf("expected same-chain restart to succeed, got %v", err)
	}
}

func TestQuorumSnapshotPreservesFundingIdempotencyAndCommitsFundingIDs(t *testing.T) {
	first := newConsensusSigner(t)
	second := newConsensusSigner(t)
	validators := []dpos.Validator{
		{Rank: 1, Address: first.address, VotingPower: 60, SelfStake: 60},
		{Rank: 2, Address: second.address, VotingPower: 40, SelfStake: 40},
	}

	producer := newTestStore(t)
	if _, err := producer.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set producer validators: %v", err)
	}
	envelope := signedEnvelope(t, 25, 1, "funding-id-snapshot")
	if _, err := producer.CreditWithID("fund-1", envelope.From, 100); err != nil {
		t.Fatalf("fund producer: %v", err)
	}
	if _, err := producer.Accept(envelope); err != nil {
		t.Fatalf("accept producer transaction: %v", err)
	}
	if _, err := producer.ProduceBlock(10); err != nil {
		t.Fatalf("produce committed block: %v", err)
	}
	snapshot := producer.Snapshot()
	if snapshot.ChainID != protocol.DefaultChainID {
		t.Fatalf("expected snapshot chain %q, got %q", protocol.DefaultChainID, snapshot.ChainID)
	}

	makeProof := func(signer consensusSigner) SnapshotProof {
		proof, err := BuildSnapshotProofTemplate(snapshot, protocol.DefaultChainID, signer.address)
		if err != nil {
			t.Fatalf("build snapshot proof: %v", err)
		}
		proof.PublicKey = signer.publicKey
		proof.Payload = proof.CanonicalPayload()
		proof.Signature, err = tx.SignPayload(signer.privateKey, proof.Payload)
		if err != nil {
			t.Fatalf("sign snapshot proof: %v", err)
		}
		return proof
	}
	proofs := []SnapshotProof{makeProof(first), makeProof(second)}

	replica := newTestStore(t)
	if _, err := replica.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set replica validators: %v", err)
	}
	trusted := replica.ValidatorSet()
	if err := replica.RestoreQuorumSnapshot(snapshot, protocol.DefaultChainID, proofs, trusted, time.Now().UTC()); err != nil {
		t.Fatalf("restore quorum snapshot: %v", err)
	}
	before := replica.View(envelope.From).Balance
	if _, err := replica.CreditWithID("fund-1", envelope.From, 100); err != nil {
		t.Fatalf("replay funding id: %v", err)
	}
	if after := replica.View(envelope.From).Balance; after != before {
		t.Fatalf("expected funding idempotency after restore, balance changed from %d to %d", before, after)
	}

	tampered := snapshot
	tampered.AppliedFundingIDs = []string{"different-funding-id"}
	if err := ValidateSnapshotQuorum(tampered, protocol.DefaultChainID, proofs, trusted); err == nil {
		t.Fatal("expected tampered funding IDs to invalidate committed snapshot state")
	}
}
