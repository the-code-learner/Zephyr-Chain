package node

import (
	"errors"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	NetworkMessageProposal uint8 = 1
	NetworkMessageCommit   uint8 = 2
	MaxNetworkShards             = 1024
	MaxNetworkTransactions       = 65536
	MaxNetworkImports            = 65536
	MaxNetworkMessageBytes       = 64 << 20
)

var ErrNetworkWire = errors.New("invalid v2 node network message")

type BlockData struct {
	Batches map[uint32]ShardBatch
}

type ConsensusMessage struct {
	Kind        uint8
	Proposal    v2consensus.Proposal
	Block       BlockData
	Certificate *v2consensus.Certificate
}

func (b BlockData) MarshalBinary() ([]byte, error) {
	if len(b.Batches) > MaxNetworkShards {
		return nil, ErrNetworkWire
	}
	shards := make([]int, 0, len(b.Batches))
	for shard := range b.Batches {
		shards = append(shards, int(shard))
	}
	sort.Ints(shards)
	var w codec.Writer
	w.U32(uint32(len(shards)))
	for _, shardValue := range shards {
		shard := uint32(shardValue)
		batch := b.Batches[shard]
		if len(batch.Transactions) > MaxNetworkTransactions || len(batch.Imports) > MaxNetworkImports {
			return nil, ErrNetworkWire
		}
		w.U32(shard)
		w.Fixed(batch.DataRoot[:])
		w.U32(uint32(len(batch.Transactions)))
		for _, transaction := range batch.Transactions {
			raw, err := transaction.MarshalBinary()
			if err != nil {
				return nil, err
			}
			w.Bytes(raw)
		}
		w.U32(uint32(len(batch.Imports)))
		for _, receiptImport := range batch.Imports {
			raw, err := receiptImport.MarshalBinary()
			if err != nil {
				return nil, err
			}
			w.Bytes(raw)
		}
	}
	payload := w.BytesCopy()
	if len(payload) > MaxNetworkMessageBytes {
		return nil, ErrNetworkWire
	}
	return payload, nil
}

func ParseBlockData(data []byte) (BlockData, error) {
	if len(data) > MaxNetworkMessageBytes {
		return BlockData{}, ErrNetworkWire
	}
	r := codec.NewReader(data)
	count, err := r.U32()
	if err != nil || count > MaxNetworkShards {
		return BlockData{}, ErrNetworkWire
	}
	out := BlockData{Batches: make(map[uint32]ShardBatch, int(count))}
	for i := uint32(0); i < count; i++ {
		shard, err := r.U32()
		if err != nil {
			return BlockData{}, ErrNetworkWire
		}
		if _, duplicate := out.Batches[shard]; duplicate {
			return BlockData{}, ErrNetworkWire
		}
		rootRaw, err := r.Fixed(32)
		if err != nil {
			return BlockData{}, ErrNetworkWire
		}
		var dataRoot types.Hash
		copy(dataRoot[:], rootRaw)
		txCount, err := r.U32()
		if err != nil || txCount > MaxNetworkTransactions {
			return BlockData{}, ErrNetworkWire
		}
		batch := ShardBatch{Transactions: make([]tx.Transaction, int(txCount)), DataRoot: dataRoot}
		for j := range batch.Transactions {
			raw, err := r.Bytes(tx.MaxWireBytes)
			if err != nil {
				return BlockData{}, ErrNetworkWire
			}
			batch.Transactions[j], err = tx.ParseTransaction(raw)
			if err != nil {
				return BlockData{}, err
			}
		}
		importCount, err := r.U32()
		if err != nil || importCount > MaxNetworkImports {
			return BlockData{}, ErrNetworkWire
		}
		batch.Imports = make([]ReceiptImport, int(importCount))
		for j := range batch.Imports {
			raw, err := r.Bytes(MaxNetworkMessageBytes)
			if err != nil {
				return BlockData{}, ErrNetworkWire
			}
			batch.Imports[j], err = ParseReceiptImport(raw)
			if err != nil {
				return BlockData{}, err
			}
		}
		out.Batches[shard] = batch
	}
	if r.Done() != nil {
		return BlockData{}, ErrNetworkWire
	}
	return out, nil
}

func (m ConsensusMessage) MarshalBinary() ([]byte, error) {
	if m.Kind != NetworkMessageProposal && m.Kind != NetworkMessageCommit {
		return nil, ErrNetworkWire
	}
	proposal, err := m.Proposal.MarshalBinary()
	if err != nil {
		return nil, err
	}
	block, err := m.Block.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U8(m.Kind)
	w.Bytes(proposal)
	w.Bytes(block)
	if m.Kind == NetworkMessageCommit {
		if m.Certificate == nil {
			return nil, ErrNetworkWire
		}
		certificate, err := m.Certificate.MarshalBinary()
		if err != nil {
			return nil, err
		}
		w.Bytes(certificate)
	}
	payload := w.BytesCopy()
	if len(payload) > MaxNetworkMessageBytes {
		return nil, ErrNetworkWire
	}
	return payload, nil
}

func ParseConsensusMessage(data []byte) (ConsensusMessage, error) {
	if len(data) == 0 || len(data) > MaxNetworkMessageBytes {
		return ConsensusMessage{}, ErrNetworkWire
	}
	r := codec.NewReader(data)
	kind, err := r.U8()
	if err != nil || (kind != NetworkMessageProposal && kind != NetworkMessageCommit) {
		return ConsensusMessage{}, ErrNetworkWire
	}
	proposalRaw, err := r.Bytes(2048)
	if err != nil {
		return ConsensusMessage{}, ErrNetworkWire
	}
	proposal, err := v2consensus.ParseProposal(proposalRaw)
	if err != nil {
		return ConsensusMessage{}, err
	}
	blockRaw, err := r.Bytes(MaxNetworkMessageBytes)
	if err != nil {
		return ConsensusMessage{}, ErrNetworkWire
	}
	block, err := ParseBlockData(blockRaw)
	if err != nil {
		return ConsensusMessage{}, err
	}
	message := ConsensusMessage{Kind: kind, Proposal: proposal, Block: block}
	if kind == NetworkMessageCommit {
		certificateRaw, err := r.Bytes(4 << 20)
		if err != nil {
			return ConsensusMessage{}, ErrNetworkWire
		}
		certificate, err := v2consensus.ParseCertificate(certificateRaw)
		if err != nil {
			return ConsensusMessage{}, err
		}
		message.Certificate = &certificate
	}
	if r.Done() != nil {
		return ConsensusMessage{}, ErrNetworkWire
	}
	return message, nil
}

func (r ReceiptImport) MarshalBinary() ([]byte, error) {
	certificate, err := r.Certificate.MarshalBinary()
	if err != nil {
		return nil, err
	}
	validators, err := r.Validators.MarshalBinary()
	if err != nil {
		return nil, err
	}
	receipt, err := r.Receipt.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Bytes(r.Header.CanonicalBytes())
	w.Bytes(certificate)
	w.Bytes(validators)
	w.Bytes(r.Commitment.CanonicalBytes())
	w.Bytes(r.CommitmentProof.MarshalBinary())
	w.Bytes(receipt)
	w.Bytes(r.ReceiptProof.MarshalBinary())
	return w.BytesCopy(), nil
}

func ParseReceiptImport(data []byte) (ReceiptImport, error) {
	r := codec.NewReader(data)
	headerRaw, err := r.Bytes(512)
	if err != nil {
		return ReceiptImport{}, ErrNetworkWire
	}
	header, err := sharding.ParseGlobalHeader(headerRaw)
	if err != nil {
		return ReceiptImport{}, err
	}
	certificateRaw, err := r.Bytes(4 << 20)
	if err != nil {
		return ReceiptImport{}, ErrNetworkWire
	}
	certificate, err := v2consensus.ParseCertificate(certificateRaw)
	if err != nil {
		return ReceiptImport{}, err
	}
	validatorsRaw, err := r.Bytes(4 << 20)
	if err != nil {
		return ReceiptImport{}, ErrNetworkWire
	}
	validators, err := v2consensus.ParseValidatorSet(validatorsRaw)
	if err != nil {
		return ReceiptImport{}, err
	}
	commitmentRaw, err := r.Bytes(512)
	if err != nil {
		return ReceiptImport{}, ErrNetworkWire
	}
	commitment, err := sharding.ParseCommitment(commitmentRaw)
	if err != nil {
		return ReceiptImport{}, err
	}
	commitmentProofRaw, err := r.Bytes(64 << 10)
	if err != nil {
		return ReceiptImport{}, ErrNetworkWire
	}
	commitmentProof, err := merkle.ParseProof(commitmentProofRaw)
	if err != nil {
		return ReceiptImport{}, err
	}
	receiptRaw, err := r.Bytes(1 << 20)
	if err != nil {
		return ReceiptImport{}, ErrNetworkWire
	}
	receipt, err := sharding.ParseCrossShardReceipt(receiptRaw)
	if err != nil {
		return ReceiptImport{}, err
	}
	receiptProofRaw, err := r.Bytes(64 << 10)
	if err != nil {
		return ReceiptImport{}, ErrNetworkWire
	}
	receiptProof, err := merkle.ParseProof(receiptProofRaw)
	if err != nil || r.Done() != nil {
		return ReceiptImport{}, ErrNetworkWire
	}
	return ReceiptImport{Header: header, Certificate: certificate, Validators: validators, Commitment: commitment, CommitmentProof: commitmentProof, Receipt: receipt, ReceiptProof: receiptProof}, nil
}
