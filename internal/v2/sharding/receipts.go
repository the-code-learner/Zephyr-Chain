package sharding

import (
	"bytes"
	"errors"
	"sort"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
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
	hashes, err := b.sortedHashes()
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
	hashes, err := b.sortedHashes()
	if err != nil {
		return merkle.Proof{}, err
	}
	// Canonical hashes are strictly sorted (duplicates were rejected).
	// Locate the requested leaf in O(log n) comparisons rather than scan
	// each receipt again after building the Merkle leaf sequence.
	index := sort.Search(len(hashes), func(i int) bool {
		return bytes.Compare(hashes[i][:], target[:]) >= 0
	})
	if index == len(hashes) || hashes[index] != target {
		return merkle.Proof{}, ErrReceiptProof
	}
	return merkle.BuildProof(hashes, index)
}

// sortedHashes is the canonical receipt Merkle leaf ordering. Root and Proof
// use hashes only: do not copy or retain the much larger receipt structures.
// Hash() already validates each receipt's complete canonical encoding.
func (b ReceiptBatch) sortedHashes() ([]types.Hash, error) {
	hashes := make([]types.Hash, len(b.Receipts))
	for i, receipt := range b.Receipts {
		hash, err := receipt.Hash()
		if err != nil {
			return nil, err
		}
		hashes[i] = hash
	}
	sort.Slice(hashes, func(i, j int) bool {
		return bytes.Compare(hashes[i][:], hashes[j][:]) < 0
	})
	for i := 1; i < len(hashes); i++ {
		if hashes[i] == hashes[i-1] {
			return nil, ErrReceiptReplay
		}
	}
	return hashes, nil
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

// ReceiptMarker is a consensus-state object proving that a finalized receipt
// has already been consumed on its destination shard. Keeping this marker in
// the same Merkle state as balances makes anti-replay survive restart,
// checkpoint and snapshot recovery.
func ReceiptMarker(receipt CrossShardReceipt) (object.Object, error) {
	hash, err := receipt.Hash()
	if err != nil {
		return object.Object{}, err
	}
	return object.Object{
		ID:      types.ObjectIDForShard(hash, 0xfffffffe, receipt.DestinationShard),
		Version: 1,
		Kind:    object.KindSystem,
		Data:    append([]byte(nil), hash[:]...),
	}, nil
}

// ReceiptTracker is useful for transport/in-process duplicate suppression, but
// it is not the consensus anti-replay source of truth. Consensus uses
// ReceiptMarker in destination shard state.
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
