package sharding

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestFinalizedCrossShardReceiptAndReplayProtection(t *testing.T) {
	owner := types.AccountIDFromPublicKey([]byte("receipt-owner"))
	token := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	output, err := object.NewCoinOutput(owner, token, 25)
	if err != nil {
		t.Fatal(err)
	}
	receipt := CrossShardReceipt{
		SourceShard: 0, DestinationShard: 1, SourceHeight: 7,
		TransactionID: types.HashBytes("tx", []byte("cross-shard")), OutputIndex: 0,
		Output: output, SourceStateRoot: types.HashBytes("state", []byte("source")),
	}
	batch := ReceiptBatch{Receipts: []CrossShardReceipt{receipt}}
	receiptRoot, err := batch.Root()
	if err != nil {
		t.Fatal(err)
	}
	receiptProof, err := batch.Proof(receipt)
	if err != nil {
		t.Fatal(err)
	}
	commitments := []Commitment{
		{ShardID: 0, StateRoot: receipt.SourceStateRoot, ReceiptRoot: receiptRoot, DataRoot: types.HashBytes("data", []byte("0"))},
		{ShardID: 1, StateRoot: types.HashBytes("state", []byte("dest")), ReceiptRoot: types.HashBytes("receipt", []byte("empty")), DataRoot: types.HashBytes("data", []byte("1"))},
	}
	commitmentRoot, err := CommitmentRoot(commitments)
	if err != nil {
		t.Fatal(err)
	}
	commitment, commitmentProof, err := CommitmentProof(commitments, 0)
	if err != nil {
		t.Fatal(err)
	}
	header := GlobalHeader{Version: 2, Network: types.NetworkID(types.HashBytes("network", []byte("v2"))), Height: 7, ShardCommitmentRoot: commitmentRoot}
	tracker := NewReceiptTracker()
	if err := tracker.Consume(1, header, commitment, commitmentProof, receipt, receiptProof); err != nil {
		t.Fatal(err)
	}
	if !tracker.Consumed(receipt) {
		t.Fatal("receipt was not recorded as consumed")
	}
	if err := tracker.Consume(1, header, commitment, commitmentProof, receipt, receiptProof); err != ErrReceiptReplay {
		t.Fatalf("expected replay rejection, got %v", err)
	}
}
