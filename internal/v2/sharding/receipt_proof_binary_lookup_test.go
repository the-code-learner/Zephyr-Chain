package sharding

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestReceiptProofBinarySearchRetainsCanonicalIndexAndMissingRejection(t *testing.T) {
	owner := types.AccountIDFromPublicKey([]byte("proof-binary-lookup-owner"))
	token := types.TokenID(types.HashBytes("token", []byte("proof-binary-lookup")))
	output, err := object.NewCoinOutput(owner, token, 25)
	if err != nil {
		t.Fatal(err)
	}
	receipts := make([]CrossShardReceipt, 17)
	for i := range receipts {
		receipts[i] = CrossShardReceipt{
			SourceShard: 0, DestinationShard: 1, SourceHeight: 17,
			TransactionID: types.HashBytes("receipt-proof-lookup", []byte{byte(i)}),
			OutputIndex: uint32(i), Output: output,
			SourceStateRoot: types.HashBytes("receipt-proof-lookup", []byte("root")),
		}
	}
	for i, j := 0, len(receipts)-1; i < j; i, j = i+1, j-1 {
		receipts[i], receipts[j] = receipts[j], receipts[i]
	}
	batch := ReceiptBatch{Receipts: receipts}
	root, err := batch.Root()
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := batch.sortedHashes()
	if err != nil {
		t.Fatal(err)
	}
	for _, receipt := range receipts {
		hash, err := receipt.Hash()
		if err != nil {
			t.Fatal(err)
		}
		wantIndex := -1
		for i, h := range hashes {
			if h == hash {
				wantIndex = i
				break
			}
		}
		if wantIndex == -1 {
			t.Fatal("oracle hash missing")
		}
		proof, err := batch.Proof(receipt)
		if err != nil || proof.Index != uint32(wantIndex) || !merkle.Verify(root, hash, proof) {
			t.Fatalf("binary lookup changed canonical receipt proof index=%d: %v", wantIndex, err)
		}
	}
	missing := receipts[0]
	missing.TransactionID = types.HashBytes("receipt-proof-lookup", []byte("absent"))
	if _, err := batch.Proof(missing); !errors.Is(err, ErrReceiptProof) {
		t.Fatalf("unknown valid receipt accepted: %v", err)
	}
	if _, err := (ReceiptBatch{}).Proof(receipts[0]); !errors.Is(err, ErrReceiptProof) {
		t.Fatalf("empty batch accepted target receipt: %v", err)
	}
	if _, err := (ReceiptBatch{Receipts: append(receipts, receipts[0])}).Proof(receipts[0]); !errors.Is(err, ErrReceiptReplay) {
		t.Fatalf("duplicate leaf accepted by proof lookup: %v", err)
	}
}
