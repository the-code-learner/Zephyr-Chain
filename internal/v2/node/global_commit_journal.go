package node

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sort"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	globalCommitJournalVersion uint16 = 1
	globalCommitJournalMagic          = "ZGC2"
	globalCommitPreparing      uint8  = 1
	globalCommitCommitted      uint8  = 2
	maxGlobalCommitJournalBytes       = 256 << 20
	maxJournalObjects                  = 1_000_000
)

var ErrGlobalCommitJournal = errors.New("invalid Zephyr v2 global commit journal")

type journalShardDelta struct {
	ShardID  uint32
	PreRoot  types.Hash
	PostRoot types.Hash
	Consumed []types.ObjectID
	Created  []object.Object
}

type globalCommitIntent struct {
	Status             uint8
	Network            types.NetworkID
	NativeToken        types.TokenID
	ValidatorRoot      types.Hash
	ShardCount         uint32
	PreHeight          uint64
	PreParentHash      types.Hash
	Header             sharding.GlobalHeader
	Certificate        v2consensus.Certificate
	Commitments        []sharding.Commitment
	Deltas             []journalShardDelta
	EconomicCheckpoint []byte
}

func (r *Runtime) buildGlobalCommitIntent(candidate Candidate, certificate v2consensus.Certificate, commitments map[uint32]sharding.Commitment, economicCheckpoint []byte) (globalCommitIntent, error) {
	if r == nil || candidate.Header.Height != r.Height+1 || candidate.Header.ParentHash != r.ParentHash || len(commitments) != int(r.ShardCount) {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	intent := globalCommitIntent{
		Status: globalCommitPreparing, Network: r.Network, NativeToken: r.NativeToken,
		ValidatorRoot: r.ValidatorRoot, ShardCount: r.ShardCount, PreHeight: r.Height,
		PreParentHash: r.ParentHash, Header: candidate.Header, Certificate: certificate,
		Commitments: append([]sharding.Commitment(nil), candidate.Commitments...),
		EconomicCheckpoint: append([]byte(nil), economicCheckpoint...),
	}
	shards := make([]int, 0, len(candidate.deltas))
	for shard := range candidate.deltas {
		shards = append(shards, int(shard))
	}
	sort.Ints(shards)
	for _, shardValue := range shards {
		shard := uint32(shardValue)
		commitment, ok := commitments[shard]
		if !ok {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		delta := candidate.deltas[shard]
		intent.Deltas = append(intent.Deltas, journalShardDelta{
			ShardID: shard, PreRoot: r.States[shard].Root(), PostRoot: commitment.StateRoot,
			Consumed: append([]types.ObjectID(nil), delta.Consumed...),
			Created: cloneJournalObjects(delta.Created),
		})
	}
	if err := intent.Validate(); err != nil {
		return globalCommitIntent{}, err
	}
	return intent, nil
}

func (i globalCommitIntent) Validate() error {
	if (i.Status != globalCommitPreparing && i.Status != globalCommitCommitted) ||
		types.IsZero32([32]byte(i.Network)) || types.IsZero32([32]byte(i.NativeToken)) ||
		types.IsZero32([32]byte(i.ValidatorRoot)) || i.ShardCount == 0 ||
		i.Header.Network != i.Network || i.Header.Height != i.PreHeight+1 || i.Header.ParentHash != i.PreParentHash ||
		i.Header.ValidatorRoot != i.ValidatorRoot || i.Certificate.Network != i.Network ||
		i.Certificate.Height != i.Header.Height || i.Certificate.HeaderHash != v2consensus.HeaderConsensusHash(i.Header) ||
		len(i.Commitments) != int(i.ShardCount) || len(i.Deltas) > int(i.ShardCount) ||
		len(i.EconomicCheckpoint) > maxEconomicCheckpointBytes+8+32 {
		return ErrGlobalCommitJournal
	}
	root, err := sharding.CommitmentRoot(i.Commitments)
	if err != nil || root != i.Header.ShardCommitmentRoot {
		return ErrGlobalCommitJournal
	}
	commitments := make(map[uint32]sharding.Commitment, i.ShardCount)
	for _, commitment := range i.Commitments {
		if commitment.ShardID >= i.ShardCount {
			return ErrGlobalCommitJournal
		}
		if _, duplicate := commitments[commitment.ShardID]; duplicate {
			return ErrGlobalCommitJournal
		}
		commitments[commitment.ShardID] = commitment
	}
	seenDelta := make(map[uint32]struct{}, len(i.Deltas))
	for _, delta := range i.Deltas {
		if delta.ShardID >= i.ShardCount || len(delta.Consumed) > maxJournalObjects || len(delta.Created) > maxJournalObjects ||
			types.IsZero32([32]byte(delta.PreRoot)) || types.IsZero32([32]byte(delta.PostRoot)) {
			return ErrGlobalCommitJournal
		}
		if _, duplicate := seenDelta[delta.ShardID]; duplicate {
			return ErrGlobalCommitJournal
		}
		seenDelta[delta.ShardID] = struct{}{}
		commitment, ok := commitments[delta.ShardID]
		if !ok || commitment.StateRoot != delta.PostRoot {
			return ErrGlobalCommitJournal
		}
		for _, id := range delta.Consumed {
			if types.IsZero32([32]byte(id)) {
				return ErrGlobalCommitJournal
			}
		}
		for _, created := range delta.Created {
			if created.Validate() != nil {
				return ErrGlobalCommitJournal
			}
		}
	}
	return nil
}

func (i globalCommitIntent) MarshalBinary() ([]byte, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(globalCommitJournalVersion)
	w.U8(i.Status)
	w.Fixed(i.Network[:])
	w.Fixed(i.NativeToken[:])
	w.Fixed(i.ValidatorRoot[:])
	w.U32(i.ShardCount)
	w.U64(i.PreHeight)
	w.Fixed(i.PreParentHash[:])
	headerRaw, err := i.Header.MarshalBinary()
	if err != nil {
		return nil, err
	}
	w.Bytes(headerRaw)
	certificateRaw, err := v2consensus.MarshalCertificate(i.Certificate)
	if err != nil {
		return nil, err
	}
	w.Bytes(certificateRaw)

	orderedCommitments := append([]sharding.Commitment(nil), i.Commitments...)
	sort.Slice(orderedCommitments, func(a, b int) bool { return orderedCommitments[a].ShardID < orderedCommitments[b].ShardID })
	w.U32(uint32(len(orderedCommitments)))
	for _, commitment := range orderedCommitments {
		w.U32(commitment.ShardID)
		w.Fixed(commitment.StateRoot[:])
		w.Fixed(commitment.ReceiptRoot[:])
		w.Fixed(commitment.DataRoot[:])
	}

	orderedDeltas := append([]journalShardDelta(nil), i.Deltas...)
	sort.Slice(orderedDeltas, func(a, b int) bool { return orderedDeltas[a].ShardID < orderedDeltas[b].ShardID })
	w.U32(uint32(len(orderedDeltas)))
	for _, delta := range orderedDeltas {
		w.U32(delta.ShardID)
		w.Fixed(delta.PreRoot[:])
		w.Fixed(delta.PostRoot[:])
		w.U32(uint32(len(delta.Consumed)))
		for _, id := range delta.Consumed {
			w.Fixed(id[:])
		}
		w.U32(uint32(len(delta.Created)))
		for _, created := range delta.Created {
			w.Bytes(created.CanonicalBytes())
		}
	}
	w.Bytes(i.EconomicCheckpoint)
	return w.BytesCopy(), nil
}

func parseGlobalCommitIntent(data []byte) (globalCommitIntent, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != globalCommitJournalVersion {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	intent := globalCommitIntent{}
	intent.Status, err = r.U8()
	if err != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	networkRaw, err := r.Fixed(32)
	if err != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	copy(intent.Network[:], networkRaw)
	nativeRaw, err := r.Fixed(32)
	if err != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	copy(intent.NativeToken[:], nativeRaw)
	validatorRaw, err := r.Fixed(32)
	if err != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	copy(intent.ValidatorRoot[:], validatorRaw)
	intent.ShardCount, err = r.U32()
	if err != nil || intent.ShardCount == 0 || intent.ShardCount > 1_000_000 {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	intent.PreHeight, err = r.U64()
	if err != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	parentRaw, err := r.Fixed(32)
	if err != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	copy(intent.PreParentHash[:], parentRaw)
	headerRaw, err := r.Bytes(1 << 20)
	if err != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	intent.Header, err = sharding.ParseGlobalHeader(headerRaw)
	if err != nil {
		return globalCommitIntent{}, err
	}
	certificateRaw, err := r.Bytes(64 << 20)
	if err != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	intent.Certificate, err = v2consensus.ParseCertificate(certificateRaw)
	if err != nil {
		return globalCommitIntent{}, err
	}

	commitmentCount, err := r.U32()
	if err != nil || commitmentCount != intent.ShardCount {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	intent.Commitments = make([]sharding.Commitment, int(commitmentCount))
	for index := range intent.Commitments {
		intent.Commitments[index].ShardID, err = r.U32()
		if err != nil {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		stateRaw, err := r.Fixed(32)
		if err != nil {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		copy(intent.Commitments[index].StateRoot[:], stateRaw)
		receiptRaw, err := r.Fixed(32)
		if err != nil {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		copy(intent.Commitments[index].ReceiptRoot[:], receiptRaw)
		dataRaw, err := r.Fixed(32)
		if err != nil {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		copy(intent.Commitments[index].DataRoot[:], dataRaw)
	}

	deltaCount, err := r.U32()
	if err != nil || deltaCount > intent.ShardCount {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	intent.Deltas = make([]journalShardDelta, int(deltaCount))
	for index := range intent.Deltas {
		delta := &intent.Deltas[index]
		delta.ShardID, err = r.U32()
		if err != nil {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		preRaw, err := r.Fixed(32)
		if err != nil {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		copy(delta.PreRoot[:], preRaw)
		postRaw, err := r.Fixed(32)
		if err != nil {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		copy(delta.PostRoot[:], postRaw)
		consumedCount, err := r.U32()
		if err != nil || consumedCount > maxJournalObjects {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		delta.Consumed = make([]types.ObjectID, int(consumedCount))
		for consumedIndex := range delta.Consumed {
			raw, err := r.Fixed(32)
			if err != nil {
				return globalCommitIntent{}, ErrGlobalCommitJournal
			}
			copy(delta.Consumed[consumedIndex][:], raw)
		}
		createdCount, err := r.U32()
		if err != nil || createdCount > maxJournalObjects {
			return globalCommitIntent{}, ErrGlobalCommitJournal
		}
		delta.Created = make([]object.Object, int(createdCount))
		for createdIndex := range delta.Created {
			raw, err := r.Bytes(object.MaxObjectDataBytes + 128)
			if err != nil {
				return globalCommitIntent{}, ErrGlobalCommitJournal
			}
			created, err := object.ParseObject(raw)
			if err != nil {
				return globalCommitIntent{}, err
			}
			delta.Created[createdIndex] = created
		}
	}
	intent.EconomicCheckpoint, err = r.Bytes(maxEconomicCheckpointBytes + 8 + 32)
	if err != nil || r.Done() != nil {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	if err := intent.Validate(); err != nil {
		return globalCommitIntent{}, err
	}
	return intent, nil
}

func encodeGlobalCommitJournal(intent globalCommitIntent) ([]byte, error) {
	payload, err := intent.MarshalBinary()
	if err != nil || len(payload) == 0 || len(payload) > maxGlobalCommitJournalBytes {
		return nil, ErrGlobalCommitJournal
	}
	digest := codec.DomainHash("zephyr/global-commit-journal/v2", payload)
	var out bytes.Buffer
	out.WriteString(globalCommitJournalMagic)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	out.Write(length[:])
	out.Write(payload)
	out.Write(digest[:])
	return out.Bytes(), nil
}

func decodeGlobalCommitJournal(data []byte) (globalCommitIntent, error) {
	if len(data) < 8+32 || len(data) > maxGlobalCommitJournalBytes+8+32 || string(data[:4]) != globalCommitJournalMagic {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	length := binary.BigEndian.Uint32(data[4:8])
	if length == 0 || int(length) > maxGlobalCommitJournalBytes || len(data) != 8+int(length)+32 {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	payload := data[8 : 8+int(length)]
	digest := codec.DomainHash("zephyr/global-commit-journal/v2", payload)
	if !bytes.Equal(digest[:], data[8+int(length):]) {
		return globalCommitIntent{}, ErrGlobalCommitJournal
	}
	return parseGlobalCommitIntent(payload)
}

func writeGlobalCommitJournal(path string, intent globalCommitIntent) error {
	raw, err := encodeGlobalCommitJournal(intent)
	if err != nil {
		return err
	}
	return writeAtomicGlobalCommitFile(path, raw)
}

func readGlobalCommitJournal(path string) (globalCommitIntent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return globalCommitIntent{}, err
	}
	return decodeGlobalCommitJournal(raw)
}

func writeAtomicGlobalCommitFile(path string, raw []byte) error {
	if path == "" || len(raw) == 0 || len(raw) > maxGlobalCommitJournalBytes+8+32 {
		return ErrGlobalCommitJournal
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.OpenFile(filepath.Join(dir, "."+base+".tmp"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err = tmp.Write(raw); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	dirHandle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer dirHandle.Close()
	return dirHandle.Sync()
}

func cloneJournalObjects(source []object.Object) []object.Object {
	out := make([]object.Object, len(source))
	for index, item := range source {
		out[index] = item
		out[index].Data = append([]byte(nil), item.Data...)
	}
	return out
}
