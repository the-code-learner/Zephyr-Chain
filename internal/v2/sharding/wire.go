package sharding

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func ParseCommitment(data []byte) (Commitment, error) {
	r := codec.NewReader(data)
	shardID, err := r.U32()
	if err != nil {
		return Commitment{}, ErrShardCount
	}
	stateRoot, err := readHash(r)
	if err != nil {
		return Commitment{}, ErrShardCount
	}
	receiptRoot, err := readHash(r)
	if err != nil {
		return Commitment{}, ErrShardCount
	}
	dataRoot, err := readHash(r)
	if err != nil || r.Done() != nil || types.IsZero32([32]byte(stateRoot)) {
		return Commitment{}, ErrShardCount
	}
	return Commitment{ShardID: shardID, StateRoot: stateRoot, ReceiptRoot: receiptRoot, DataRoot: dataRoot}, nil
}

func ParseGlobalHeader(data []byte) (GlobalHeader, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != 2 {
		return GlobalHeader{}, ErrShardCount
	}
	networkBytes, err := r.Fixed(32)
	if err != nil {
		return GlobalHeader{}, ErrShardCount
	}
	var network types.NetworkID
	copy(network[:], networkBytes)
	height, err := r.U64()
	if err != nil || height == 0 {
		return GlobalHeader{}, ErrShardCount
	}
	parentHash, err := readHash(r)
	if err != nil {
		return GlobalHeader{}, ErrShardCount
	}
	shardRoot, err := readHash(r)
	if err != nil {
		return GlobalHeader{}, ErrShardCount
	}
	validatorRoot, err := readHash(r)
	if err != nil {
		return GlobalHeader{}, ErrShardCount
	}
	dataRoot, err := readHash(r)
	if err != nil {
		return GlobalHeader{}, ErrShardCount
	}
	certificateHash, err := readHash(r)
	if err != nil || r.Done() != nil || types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(shardRoot)) || types.IsZero32([32]byte(validatorRoot)) {
		return GlobalHeader{}, ErrShardCount
	}
	return GlobalHeader{
		Version: version, Network: network, Height: height, ParentHash: parentHash,
		ShardCommitmentRoot: shardRoot, ValidatorRoot: validatorRoot, DataRoot: dataRoot,
		CertificateHash: certificateHash,
	}, nil
}

func ParseCrossShardReceipt(data []byte) (CrossShardReceipt, error) {
	r := codec.NewReader(data)
	source, err := r.U32()
	if err != nil {
		return CrossShardReceipt{}, ErrReceipt
	}
	destination, err := r.U32()
	if err != nil {
		return CrossShardReceipt{}, ErrReceipt
	}
	height, err := r.U64()
	if err != nil {
		return CrossShardReceipt{}, ErrReceipt
	}
	txHash, err := readHash(r)
	if err != nil {
		return CrossShardReceipt{}, ErrReceipt
	}
	index, err := r.U32()
	if err != nil {
		return CrossShardReceipt{}, ErrReceipt
	}
	outputBytes, err := r.Bytes(object.MaxObjectDataBytes + 64)
	if err != nil {
		return CrossShardReceipt{}, ErrReceipt
	}
	output, err := object.ParseOutputSpec(outputBytes)
	if err != nil {
		return CrossShardReceipt{}, ErrReceipt
	}
	stateRoot, err := readHash(r)
	if err != nil || r.Done() != nil {
		return CrossShardReceipt{}, ErrReceipt
	}
	receipt := CrossShardReceipt{SourceShard: source, DestinationShard: destination, SourceHeight: height, TransactionID: txHash, OutputIndex: index, Output: output, SourceStateRoot: stateRoot}
	if err := receipt.Validate(); err != nil {
		return CrossShardReceipt{}, err
	}
	return receipt, nil
}

func readHash(r *codec.Reader) (types.Hash, error) {
	raw, err := r.Fixed(32)
	if err != nil {
		return types.Hash{}, err
	}
	var hash types.Hash
	copy(hash[:], raw)
	return hash, nil
}
