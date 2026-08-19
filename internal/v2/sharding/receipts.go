package sharding

import (
	"errors"
	"sort"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var (
	ErrReceiptProof  = errors.New("invalid cross-shard receipt proof")
	ErrReceiptReplay = errors.New("cross-shard receipt already consumed")
)

type ReceiptBatch struct {
	Receipts []CrossShardReceipt
}

func (b ReceiptBatch) Root() (types.Hash, error) {
	hashes, _, err := b.sortedHashes()
	if err != nil {
		return types.Hash{}, err
	}
	return merkle.Root(hashes), nil
}

func (b ReceiptBatch) Proof(receipt CrossShardReceipt) (merkle.Proof, error) {
	target, err := receipt.Hash()
	if err != nil {
		return merkle.Proof{}, err
	}
	hashes, _, err := b.sortedHashes()
	if err != nil {
		return merkle.Proof{}, err
	}
	for i, hash := range hashes {
		if hash == target {
			return merkle.BuildProof(hashes, i)
		}
	}
	return merkle.Proof{}, ErrReceiptProof
}

func (b ReceiptBatch) sortedHashes() ([]types.Hash, []CrossShardReceipt, error) {
	items := append([]CrossShardReceipt(nil), b.Receipts...)
	for _, receipt := range items {
		if err := receipt.Validate(); err != nil {
			return nil, nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, _ := items[i].Hash()
		b, _ := items[j].Hash()
		return a.String() < b.String()
	})
	hashes := make([]types.Hash, len(items))
	for i, receipt := range items {
		hashes[i], _ = receipt.Hash()
		if i > 0 && hashes[i] == hashes[i-1] {
			return nil, nil, ErrReceiptReplay
		}
	}
	return hashes, items, nil
}

// VerifyFinalizedReceipt proves both that the source shard commitment belongs
// to a finalized global header and that the receipt belongs to that shard's
// receipt root. A destination shard therefore never trusts the source shard by
// assertion alone.
func VerifyFinalizedReceipt(header GlobalHeader, commitment Commitment, commitmentProof merkle.Proof, receipt CrossShardReceipt, receiptProof merkle.Proof) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	if commitment.ShardID != receipt.SourceShard || header.Height != receipt.SourceHeight {
		return ErrReceiptProof
	}
	if !merkle.Verify(header.ShardCommitmentRoot, commitment.Hash(), commitmentProof) {
		return ErrReceiptProof
	}
	receiptHash, err := receipt.Hash()
	if err != nil {
		return err
	}
	if !merkle.Verify(commitment.ReceiptRoot, receiptHash, receiptProof) {
		return ErrReceiptProof
	}
	return nil
}

type ReceiptTracker struct {
	mu       sync.Mutex
	consumed map[types.Hash]uint64
}

func NewReceiptTracker() *ReceiptTracker {
	return &ReceiptTracker{consumed: make(map[types.Hash]uint64)}
}

func (t *ReceiptTracker) Consume(destinationShard uint32, header GlobalHeader, commitment Commitment, commitmentProof merkle.Proof, receipt CrossShardReceipt, receiptProof merkle.Proof) error {
	if receipt.DestinationShard != destinationShard {
		return ErrReceiptProof
	}
	if err := VerifyFinalizedReceipt(header, commitment, commitmentProof, receipt, receiptProof); err != nil {
		return err
	}
	hash, _ := receipt.Hash()
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.consumed[hash]; exists {
		return ErrReceiptReplay
	}
	t.consumed[hash] = header.Height
	return nil
}

func (t *ReceiptTracker) Consumed(receipt CrossShardReceipt) bool {
	hash, err := receipt.Hash()
	if err != nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.consumed[hash]
	return ok
}
