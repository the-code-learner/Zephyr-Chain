package sharding

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestShardCommitmentProof(t *testing.T) {
	commitments := []Commitment{
		{ShardID: 2, StateRoot: types.HashBytes("s", []byte("2")), ReceiptRoot: types.HashBytes("r", []byte("2")), DataRoot: types.HashBytes("d", []byte("2"))},
		{ShardID: 0, StateRoot: types.HashBytes("s", []byte("0")), ReceiptRoot: types.HashBytes("r", []byte("0")), DataRoot: types.HashBytes("d", []byte("0"))},
		{ShardID: 1, StateRoot: types.HashBytes("s", []byte("1")), ReceiptRoot: types.HashBytes("r", []byte("1")), DataRoot: types.HashBytes("d", []byte("1"))},
	}
	root, err := CommitmentRoot(commitments)
	if err != nil {
		t.Fatal(err)
	}
	c, proof, err := CommitmentProof(commitments, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !merkle.Verify(root, c.Hash(), proof) {
		t.Fatal("commitment proof failed")
	}
}
