package sharding

import (
	"reflect"
	"sort"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestCanonicalCommitmentFastPathMatchesSortedOracle(t *testing.T) {
	canonical := make([]Commitment, 12)
	for shard := range canonical {
		canonical[shard] = Commitment{
			ShardID:     uint32(shard),
			StateRoot:   types.HashBytes("state", []byte{byte(shard)}),
			ReceiptRoot: types.HashBytes("receipt", []byte{byte(shard)}),
			DataRoot:    types.HashBytes("data", []byte{byte(shard)}),
		}
	}
	oracle := append([]Commitment(nil), canonical...)
	sort.Slice(oracle, func(i, j int) bool { return oracle[i].ShardID < oracle[j].ShardID })
	leaves := make([]types.Hash, len(oracle))
	for i, item := range oracle {
		leaves[i] = item.Hash()
	}
	want := merkle.Root(leaves)
	for _, input := range [][]Commitment{
		append([]Commitment(nil), canonical...),
		func() []Commitment {
			reversed := append([]Commitment(nil), canonical...)
			for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
				reversed[i], reversed[j] = reversed[j], reversed[i]
			}
			return reversed
		}(),
	} {
		original := append([]Commitment(nil), input...)
		got, err := CommitmentRoot(input)
		if err != nil || got != want {
			t.Fatalf("commitment root changed: got=%v want=%v err=%v", got, want, err)
		}
		if !reflect.DeepEqual(input, original) {
			t.Fatal("commitment root reordered caller input")
		}
		for _, item := range canonical {
			found, proof, err := CommitmentProof(input, item.ShardID)
			if err != nil || found != item || !merkle.Verify(got, item.Hash(), proof) {
				t.Fatalf("shard=%d: proof changed: %v", item.ShardID, err)
			}
		}
	}
	for _, bad := range [][]Commitment{
		{canonical[0], canonical[0]},
		{canonical[1], canonical[0], canonical[1]},
		{{ShardID: 0}},
	} {
		if _, err := CommitmentRoot(bad); err != ErrShardCount {
			t.Fatalf("malformed commitments accepted: %v", err)
		}
	}
}
