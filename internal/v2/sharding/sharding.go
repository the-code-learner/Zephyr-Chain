package sharding

import (
	"bytes"
	"errors"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var (
	ErrShardCount = errors.New("invalid shard count")
	ErrReceipt    = errors.New("invalid cross-shard receipt")
)

type Router struct {
	ShardCount uint32
}

func (r Router) ShardForAccount(account types.AccountID) (uint32, error) {
	if r.ShardCount == 0 || types.IsZero32([32]byte(account)) {
		return 0, ErrShardCount
	}
	return types.AccountShard(account, r.ShardCount), nil
}

func (r Router) ShardForObject(id types.ObjectID) (uint32, error) {
	if r.ShardCount == 0 || types.IsZero32([32]byte(id)) {
		return 0, ErrShardCount
	}
	shard := types.ObjectShard(id)
	if shard >= r.ShardCount {
		return 0, ErrShardCount
	}
	return shard, nil
}

type Commitment struct {
	ShardID     uint32
	StateRoot   types.Hash
	ReceiptRoot types.Hash
	DataRoot    types.Hash
}

func (c Commitment) CanonicalBytes() []byte {
	var w codec.Writer
	w.U32(c.ShardID)
	w.Fixed(c.StateRoot[:])
	w.Fixed(c.ReceiptRoot[:])
	w.Fixed(c.DataRoot[:])
	return w.BytesCopy()
}

func (c Commitment) Hash() types.Hash {
	return merkle.Leaf("shard-commitment", c.CanonicalBytes())
}

func CommitmentRoot(commitments []Commitment) (types.Hash, error) {
	sorted, err := sortedCommitments(commitments)
	if err != nil {
		return types.Hash{}, err
	}
	leaves := make([]types.Hash, len(sorted))
	for i, c := range sorted {
		leaves[i] = c.Hash()
	}
	return merkle.Root(leaves), nil
}

func CommitmentProof(commitments []Commitment, shardID uint32) (Commitment, merkle.Proof, error) {
	sorted, err := sortedCommitments(commitments)
	if err != nil {
		return Commitment{}, merkle.Proof{}, err
	}
	for i, c := range sorted {
		if c.ShardID == shardID {
			leaves := make([]types.Hash, len(sorted))
			for j, item := range sorted {
				leaves[j] = item.Hash()
			}
			proof, err := merkle.BuildProof(leaves, i)
			return c, proof, err
		}
	}
	return Commitment{}, merkle.Proof{}, ErrReceipt
}

type GlobalHeader struct {
	Version             uint16
	Network             types.NetworkID
	Height              uint64
	ParentHash          types.Hash
	ShardCommitmentRoot types.Hash
	ValidatorRoot       types.Hash
	DataRoot            types.Hash
	CertificateHash     types.Hash
}

func (h GlobalHeader) CanonicalBytes() []byte {
	var w codec.Writer
	w.U16(h.Version)
	w.Fixed(h.Network[:])
	w.U64(h.Height)
	w.Fixed(h.ParentHash[:])
	w.Fixed(h.ShardCommitmentRoot[:])
	w.Fixed(h.ValidatorRoot[:])
	w.Fixed(h.DataRoot[:])
	w.Fixed(h.CertificateHash[:])
	return w.BytesCopy()
}

func (h GlobalHeader) Hash() types.Hash {
	return types.Hash(codec.DomainHash("zephyr/global-header/v2", h.CanonicalBytes()))
}

type CrossShardReceipt struct {
	SourceShard      uint32
	DestinationShard uint32
	SourceHeight     uint64
	TransactionID    types.Hash
	OutputIndex      uint32
	Output           object.OutputSpec
	SourceStateRoot  types.Hash
}

func (r CrossShardReceipt) Validate() error {
	if r.SourceShard == r.DestinationShard || r.SourceHeight == 0 ||
		types.IsZero32([32]byte(r.TransactionID)) || types.IsZero32([32]byte(r.SourceStateRoot)) {
		return ErrReceipt
	}
	return r.Output.Validate()
}

func (r CrossShardReceipt) CanonicalBytes() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U32(r.SourceShard)
	w.U32(r.DestinationShard)
	w.U64(r.SourceHeight)
	w.Fixed(r.TransactionID[:])
	w.U32(r.OutputIndex)
	w.Bytes(r.Output.CanonicalBytes())
	w.Fixed(r.SourceStateRoot[:])
	return w.BytesCopy(), nil
}

func (r CrossShardReceipt) Hash() (types.Hash, error) {
	payload, err := r.CanonicalBytes()
	if err != nil {
		return types.Hash{}, err
	}
	return merkle.Leaf("cross-shard-receipt", payload), nil
}

func (r CrossShardReceipt) DestinationObject() (object.Object, error) {
	if err := r.Validate(); err != nil {
		return object.Object{}, err
	}
	return object.Object{
		ID:      types.ObjectIDForShard(r.TransactionID, r.OutputIndex, r.DestinationShard),
		Version: 1,
		Owner:   r.Output.Owner,
		Kind:    r.Output.Kind,
		Data:    append([]byte(nil), r.Output.Data...),
	}, nil
}

func sortedCommitments(in []Commitment) ([]Commitment, error) {
	if len(in) == 0 {
		return nil, ErrShardCount
	}
	out := append([]Commitment(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].ShardID < out[j].ShardID })
	for i := range out {
		if i > 0 && out[i-1].ShardID == out[i].ShardID {
			return nil, ErrShardCount
		}
		if types.IsZero32([32]byte(out[i].StateRoot)) {
			return nil, ErrShardCount
		}
	}
	return out, nil
}

func SameCommitment(a, b Commitment) bool {
	return a.ShardID == b.ShardID && bytes.Equal(a.CanonicalBytes(), b.CanonicalBytes())
}
