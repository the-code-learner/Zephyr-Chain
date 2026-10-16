package da

import (
	"bytes"
	"errors"

	"github.com/klauspost/reedsolomon"
)

const (
	MaxDataShards   = 128
	MaxParityShards = 128
	MaxBlobBytes    = 64 << 20
)

var (
	ErrInvalidErasureConfig = errors.New("invalid data-availability erasure configuration")
	ErrReconstruction       = errors.New("data-availability reconstruction failed")
)

type ReedSolomonEncoder struct{}

func (ReedSolomonEncoder) Encode(data []byte, dataShards, parityShards uint16) ([][]byte, error) {
	if err := validateErasureConfig(len(data), dataShards, parityShards); err != nil {
		return nil, err
	}
	encoder, err := reedsolomon.New(int(dataShards), int(parityShards))
	if err != nil {
		return nil, ErrInvalidErasureConfig
	}
	shards, err := encoder.Split(data)
	if err != nil {
		return nil, ErrInvalidErasureConfig
	}
	if err := encoder.Encode(shards); err != nil {
		return nil, ErrInvalidErasureConfig
	}
	return cloneChunks(shards), nil
}

func EncodeBlob(data []byte, dataShards, parityShards uint16) (Commitment, [][]byte, []Sample, error) {
	chunks, err := (ReedSolomonEncoder{}).Encode(data, dataShards, parityShards)
	if err != nil {
		return Commitment{}, nil, nil, err
	}
	commitment, samples, err := CommitChunks(chunks, dataShards, parityShards, uint64(len(data)))
	if err != nil {
		return Commitment{}, nil, nil, err
	}
	return commitment, chunks, samples, nil
}

func ReconstructBlob(commitment Commitment, chunks [][]byte, samples []Sample) ([]byte, error) {
	if err := validateCommitment(commitment); err != nil || len(chunks) != int(commitment.ChunkCount) || len(samples) != int(commitment.ChunkCount) {
		return nil, ErrReconstruction
	}
	working := cloneChunks(chunks)
	valid := 0
	for i := range working {
		if working[i] == nil {
			continue
		}
		if samples[i].Index != uint32(i) || !VerifySample(commitment, samples[i], working[i]) {
			working[i] = nil
			continue
		}
		valid++
	}
	if valid < int(commitment.DataShards) {
		return nil, ErrReconstruction
	}
	encoder, err := reedsolomon.New(int(commitment.DataShards), int(commitment.ParityShards))
	if err != nil {
		return nil, ErrReconstruction
	}
	if err := encoder.Reconstruct(working); err != nil {
		return nil, ErrReconstruction
	}
	for i := range working {
		if !VerifySample(commitment, samples[i], working[i]) {
			return nil, ErrReconstruction
		}
	}
	var out bytes.Buffer
	if err := encoder.Join(&out, working, int(commitment.OriginalSize)); err != nil {
		return nil, ErrReconstruction
	}
	return out.Bytes(), nil
}

func validateErasureConfig(size int, dataShards, parityShards uint16) error {
	if size <= 0 || size > MaxBlobBytes || dataShards == 0 || parityShards == 0 ||
		dataShards > MaxDataShards || parityShards > MaxParityShards || int(dataShards)+int(parityShards) > 256 {
		return ErrInvalidErasureConfig
	}
	return nil
}

func validateCommitment(commitment Commitment) error {
	if commitment.OriginalSize == 0 || commitment.OriginalSize > MaxBlobBytes ||
		commitment.DataShards == 0 || commitment.ParityShards == 0 ||
		commitment.DataShards > MaxDataShards || commitment.ParityShards > MaxParityShards ||
		uint32(commitment.DataShards)+uint32(commitment.ParityShards) != commitment.ChunkCount {
		return ErrInvalidErasureConfig
	}
	return nil
}

func cloneChunks(in [][]byte) [][]byte {
	out := make([][]byte, len(in))
	for i, chunk := range in {
		if chunk != nil {
			out[i] = append([]byte(nil), chunk...)
		}
	}
	return out
}
