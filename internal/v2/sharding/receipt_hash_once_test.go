package sharding

import (
	"sort"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestReceiptHashOnceSortPreservesCanonicalRootAndProofs(t *testing.T) {
	owner := types.AccountIDFromPublicKey([]byte("receipt-hash-once-owner"))
	token := types.TokenID(types.HashBytes("token", []byte("receipt-hash-once")))
	output, err := object.NewCoinOutput(owner, token, 25)
	if err != nil {
		t.Fatal(err)
	}
	receipts := make([]CrossShardReceipt, 12)
	for i := range receipts {
		receipts[i] = CrossShardReceipt{
			SourceShard: 0, DestinationShard: 1, SourceHeight: 7,
			TransactionID: types.HashBytes("tx", []byte{byte(i)}),
			OutputIndex:   uint32(i), Output: output,
			SourceStateRoot: types.HashBytes("root", []byte("receipt-hash-once")),
		}
	}
	batch := ReceiptBatch{Receipts: receipts}
	got, err := batch.Root()
	if err != nil {
		t.Fatal(err)
	}
	// Independent oracle using the old canonical hexadecimal ordering.
	legacy := append([]CrossShardReceipt(nil), receipts...)
	sort.Slice(legacy, func(i, j int) bool {
		a, _ := legacy[i].Hash()
		b, _ := legacy[j].Hash()
		return a.String() < b.String()
	})
	hashes := make([]types.Hash, len(legacy))
	for i, receipt := range legacy {
		hashes[i], err = receipt.Hash()
		if err != nil {
			t.Fatal(err)
		}
	}
	if want := merkle.Root(hashes); got != want {
		t.Fatalf("receipt root changed: got %s want %s", got.String(), want.String())
	}
	for _, receipt := range receipts {
		proof, err := batch.Proof(receipt)
		if err != nil {
			t.Fatal(err)
		}
		h, err := receipt.Hash()
		if err != nil || !merkle.Verify(got, h, proof) {
			t.Fatalf("invalid receipt proof: %v", err)
		}
	}
	reordered := append([]CrossShardReceipt(nil), receipts...)
	for i, j := 0, len(reordered)-1; i < j; i, j = i+1, j-1 {
		reordered[i], reordered[j] = reordered[j], reordered[i]
	}
	other, err := (ReceiptBatch{Receipts: reordered}).Root()
	if err != nil || other != got {
		t.Fatalf("receipt order affected canonical root: %v", err)
	}
	if _, err := (ReceiptBatch{Receipts: append(receipts, receipts[0])}).Root(); err != ErrReceiptReplay {
		t.Fatalf("duplicate receipt not rejected: %v", err)
	}
}
