package da

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrInvalidSample = errors.New("invalid data-availability sample")

type Commitment struct {
	Root         types.Hash
	ChunkCount   uint32
	DataShards   uint16
	ParityShards uint16
	OriginalSize uint64
}

type Sample struct {
	Index     uint32
	ChunkHash types.Hash
	Proof     merkle.Proof
}

type Encoder interface {
	Encode(data []byte, dataShards, parityShards uint16) ([][]byte, error)
}

func CommitChunks(chunks [][]byte, dataShards, parityShards uint16, originalSize uint64) (Commitment, []Sample, error) {
	if len(chunks) == 0 || int(dataShards)+int(parityShards) != len(chunks) || dataShards == 0 {
		return Commitment{}, nil, ErrInvalidSample
	}
	leaves := make([]types.Hash, len(chunks))
	hashes := make([]types.Hash, len(chunks))
	for i, chunk := range chunks {
		hashes[i] = types.Hash(codec.DomainHash("zephyr/da/chunk/v2", chunk))
		var w codec.Writer
		w.U32(uint32(i))
		w.Fixed(hashes[i][:])
		leaves[i] = merkle.Leaf("da-chunk", w.BytesCopy())
	}
	root := merkle.Root(leaves)
	samples := make([]Sample, len(chunks))
	for i := range chunks {
		proof, err := merkle.BuildProof(leaves, i)
		if err != nil {
			return Commitment{}, nil, err
		}
		samples[i] = Sample{Index: uint32(i), ChunkHash: hashes[i], Proof: proof}
	}
	return Commitment{
		Root: root, ChunkCount: uint32(len(chunks)), DataShards: dataShards,
		ParityShards: parityShards, OriginalSize: originalSize,
	}, samples, nil
}

func VerifySample(commitment Commitment, sample Sample, chunk []byte) bool {
	if commitment.ChunkCount == 0 || sample.Index >= commitment.ChunkCount ||
		sample.Proof.Index != sample.Index || sample.Proof.LeafCount != commitment.ChunkCount {
		return false
	}
	chunkHash := types.Hash(codec.DomainHash("zephyr/da/chunk/v2", chunk))
	if chunkHash != sample.ChunkHash {
		return false
	}
	var w codec.Writer
	w.U32(sample.Index)
	w.Fixed(chunkHash[:])
	leaf := merkle.Leaf("da-chunk", w.BytesCopy())
	return merkle.Verify(commitment.Root, leaf, sample.Proof)
}
