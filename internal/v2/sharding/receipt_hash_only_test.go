package sharding

import (
	"bytes"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestReceiptHashOnlySortPreservesCanonicalLeavesAndInput(t *testing.T) {
	owner := types.AccountIDFromPublicKey([]byte("receipt-hash-only-owner"))
	token := types.TokenID(types.HashBytes("token", []byte("receipt-hash-only")))
	output, err := object.NewCoinOutput(owner, token, 15)
	if err != nil {
		t.Fatal(err)
	}
	receipts := make([]CrossShardReceipt, 10)
	for i := range receipts {
		receipts[i] = CrossShardReceipt{
			SourceShard: 0, DestinationShard: 1, SourceHeight: 2,
			TransactionID: types.HashBytes("tx", []byte{byte(i)}),
			OutputIndex:   uint32(i), Output: output,
			SourceStateRoot: types.HashBytes("state", []byte("receipt-hash-only")),
		}
	}
	for i, j := 0, len(receipts)-1; i < j; i, j = i+1, j-1 {
		receipts[i], receipts[j] = receipts[j], receipts[i]
	}
	original := append([]CrossShardReceipt(nil), receipts...)
	batch := ReceiptBatch{Receipts: receipts}
	got, err := batch.sortedHashes()
	if err != nil {
		t.Fatal(err)
	}
	want := make([]types.Hash, len(receipts))
	for i, receipt := range receipts {
		want[i], err = receipt.Hash()
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(want, func(i, j int) bool {
		return bytes.Compare(want[i][:], want[j][:]) < 0
	})
	if len(got) != len(want) {
		t.Fatal("missing canonical receipt leaves")
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("leaf %d changed", i)
		}
	}
	if !reflect.DeepEqual(receipts, original) {
		t.Fatal("receipt hash sorting modified caller-owned batch")
	}
	if _, err := (ReceiptBatch{Receipts: append(receipts, receipts[3])}).sortedHashes(); !errors.Is(err, ErrReceiptReplay) {
		t.Fatalf("duplicate receipt not rejected: %v", err)
	}
	if _, err := (ReceiptBatch{Receipts: []CrossShardReceipt{{}}}).Root(); !errors.Is(err, ErrReceipt) {
		t.Fatalf("invalid receipt not rejected: %v", err)
	}
	if _, err := batch.Proof(CrossShardReceipt{}); !errors.Is(err, ErrReceipt) {
		t.Fatalf("invalid inclusion proof request not rejected: %v", err)
	}
}
