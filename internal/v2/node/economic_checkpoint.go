package node

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	economicCheckpointVersion  uint16 = 1
	economicCheckpointMagic           = "ZEC2"
	maxEconomicCheckpointBytes        = 64 << 20
)

var ErrEconomicCheckpoint = errors.New("invalid Zephyr v2 economic runtime checkpoint")

func (r *Runtime) EconomicCheckpointBytes() ([]byte, error) {
	if r == nil {
		return nil, ErrEconomicCheckpoint
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.economicCheckpointBytesLocked()
}

func (r *Runtime) economicCheckpointBytesLocked() ([]byte, error) {
	if r.economicCollector == nil || r.economicCollector.LastHeight() != r.Height {
		return nil, ErrEconomicCheckpoint
	}
	collectorRaw, err := r.economicCollector.CheckpointBytes()
	if err != nil {
		return nil, err
	}
	var payload codec.Writer
	payload.U16(economicCheckpointVersion)
	payload.Fixed(r.Network[:])
	payload.Fixed(r.NativeToken[:])
	payload.Fixed(r.ValidatorRoot[:])
	payload.U32(r.ShardCount)
	payload.U64(r.Height)
	payload.Fixed(r.ParentHash[:])
	payload.U64(r.economicEpochLength)
	payload.U64(r.economicBalances.TotalSupply)
	payload.U64(r.economicBalances.StakedSupply)
	payload.U64(r.economicBalances.ProtocolReserve)
	payload.U64(r.economicBalances.BaseFee)
	payload.Bytes(collectorRaw)
	payload.Bool(r.economicEngine != nil)
	if r.economicEngine != nil {
		engineRaw, err := r.economicEngine.CheckpointBytes()
		if err != nil {
			return nil, err
		}
		payload.Bytes(engineRaw)
	}
	payload.Bool(r.pendingEconomic != nil)
	if r.pendingEconomic != nil {
		if r.economicEngine == nil {
			return nil, ErrEconomicCheckpoint
		}
		pendingRaw, err := r.pendingEconomic.PendingCheckpointBytes()
		if err != nil {
			return nil, err
		}
		payload.Bytes(pendingRaw)
	}
	rawPayload := payload.BytesCopy()
	if len(rawPayload) == 0 || len(rawPayload) > maxEconomicCheckpointBytes {
		return nil, ErrEconomicCheckpoint
	}
	digest := codec.DomainHash("zephyr/economic-runtime-checkpoint/v2", rawPayload)
	var file bytes.Buffer
	file.WriteString(economicCheckpointMagic)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(rawPayload)))
	file.Write(length[:])
	file.Write(rawPayload)
	file.Write(digest[:])
	return file.Bytes(), nil
}

// RestoreEconomicCheckpointBytes restores only the economic subsystem. The
// caller must first recover the normal consensus/world-state runtime to the
// exact same height and ParentHash. A stale checkpoint therefore fails closed
// instead of moving consensus height or silently replaying economics against a
// different state root.
func (r *Runtime) RestoreEconomicCheckpointBytes(data []byte, registry *compute.WorkRegistry, engineConfig economics.ShadowEpochEngineConfig) error {
	if r == nil {
		return ErrEconomicCheckpoint
	}
	decoded, err := decodeEconomicCheckpoint(data, registry, engineConfig)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.economicCollector != nil || r.economicEngine != nil || r.pendingEconomic != nil ||
		decoded.network != r.Network || decoded.nativeToken != r.NativeToken || decoded.validatorRoot != r.ValidatorRoot ||
		decoded.shardCount != r.ShardCount || decoded.height != r.Height || decoded.parentHash != r.ParentHash ||
		decoded.collector.LastHeight() != r.Height {
		return ErrEconomicCheckpoint
	}
	if err := validateRecoveredEconomics(decoded.collector, decoded.engine, decoded.pending, decoded.epochLength, decoded.balances); err != nil {
		return err
	}
	r.economicCollector = decoded.collector
	r.economicEngine = decoded.engine
	r.pendingEconomic = decoded.pending
	r.economicEpochLength = decoded.epochLength
	r.economicBalances = decoded.balances
	return nil
}

func (r *Runtime) SaveEconomicCheckpoint(path string) error {
	raw, err := r.EconomicCheckpointBytes()
	if err != nil {
		return err
	}
	return writeAtomicEconomicCheckpoint(path, raw)
}

func (r *Runtime) RestoreEconomicCheckpoint(path string, registry *compute.WorkRegistry, engineConfig economics.ShadowEpochEngineConfig) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) > maxEconomicCheckpointBytes+8+32 {
		return ErrEconomicCheckpoint
	}
	return r.RestoreEconomicCheckpointBytes(raw, registry, engineConfig)
}

type decodedEconomicCheckpoint struct {
	network       types.NetworkID
	nativeToken   types.TokenID
	validatorRoot types.Hash
	shardCount    uint32
	height        uint64
	parentHash    types.Hash
	epochLength   uint64
	balances      economics.MonetaryBalanceSnapshot
	collector     *economics.EpochCollector
	engine        *economics.ShadowEpochEngine
	pending       *economics.ShadowEpochPreview
}

func decodeEconomicCheckpoint(data []byte, registry *compute.WorkRegistry, engineConfig economics.ShadowEpochEngineConfig) (decodedEconomicCheckpoint, error) {
	if len(data) < 8+32 || len(data) > maxEconomicCheckpointBytes+8+32 || string(data[:4]) != economicCheckpointMagic {
		return decodedEconomicCheckpoint{}, ErrEconomicCheckpoint
	}
	length := binary.BigEndian.Uint32(data[4:8])
	if length == 0 || int(length) > maxEconomicCheckpointBytes || len(data) != 8+int(length)+32 {
		return decodedEconomicCheckpoint{}, ErrEconomicCheckpoint
	}
	payload := data[8 : 8+int(length)]
	digest := codec.DomainHash("zephyr/economic-runtime-checkpoint/v2", payload)
	if !bytes.Equal(digest[:], data[8+int(length):]) {
		return decodedEconomicCheckpoint{}, ErrEconomicCheckpoint
	}
	r := codec.NewReader(payload)
	version, err := r.U16()
	if err != nil || version != economicCheckpointVersion {
		return decodedEconomicCheckpoint{}, ErrEconomicCheckpoint
	}
	out := decodedEconomicCheckpoint{}
	networkRaw, err := r.Fixed(32)
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	copy(out.network[:], networkRaw)
	nativeRaw, err := r.Fixed(32)
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	copy(out.nativeToken[:], nativeRaw)
	validatorRaw, err := r.Fixed(32)
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	copy(out.validatorRoot[:], validatorRaw)
	out.shardCount, err = r.U32()
	if err != nil || out.shardCount == 0 {
		return out, ErrEconomicCheckpoint
	}
	out.height, err = r.U64()
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	parentRaw, err := r.Fixed(32)
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	copy(out.parentHash[:], parentRaw)
	out.epochLength, err = r.U64()
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	out.balances.TotalSupply, err = r.U64()
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	out.balances.StakedSupply, err = r.U64()
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	out.balances.ProtocolReserve, err = r.U64()
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	out.balances.BaseFee, err = r.U64()
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	collectorRaw, err := r.Bytes(maxEconomicCheckpointBytes)
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	out.collector, err = economics.RestoreEpochCollector(collectorRaw, registry)
	if err != nil {
		return out, err
	}
	hasEngine, err := r.Bool()
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	if hasEngine {
		engineRaw, err := r.Bytes(maxEconomicCheckpointBytes)
		if err != nil {
			return out, ErrEconomicCheckpoint
		}
		out.engine, err = economics.RestoreShadowEpochEngine(engineRaw, out.network, engineConfig)
		if err != nil {
			return out, err
		}
	}
	hasPending, err := r.Bool()
	if err != nil {
		return out, ErrEconomicCheckpoint
	}
	if hasPending {
		if out.engine == nil {
			return out, ErrEconomicCheckpoint
		}
		pendingRaw, err := r.Bytes(maxEconomicCheckpointBytes)
		if err != nil {
			return out, ErrEconomicCheckpoint
		}
		pending, err := economics.RestoreShadowEpochPreview(pendingRaw, out.engine)
		if err != nil {
			return out, err
		}
		out.pending = &pending
	}
	if r.Done() != nil {
		return out, ErrEconomicCheckpoint
	}
	return out, nil
}

func validateRecoveredEconomics(collector *economics.EpochCollector, engine *economics.ShadowEpochEngine, pending *economics.ShadowEpochPreview, epochLength uint64, balances economics.MonetaryBalanceSnapshot) error {
	if collector == nil {
		return ErrEconomicCheckpoint
	}
	if engine == nil {
		if pending != nil || epochLength != 0 || balances != (economics.MonetaryBalanceSnapshot{}) {
			return ErrEconomicCheckpoint
		}
		return nil
	}
	if epochLength < 2 || balances.TotalSupply == 0 || balances.StakedSupply > balances.TotalSupply ||
		balances.ProtocolReserve > balances.TotalSupply || balances.BaseFee == 0 {
		return ErrEconomicCheckpoint
	}
	previous, hasPrevious := engine.PreviousState()
	if pending != nil {
		if pending.State.Epoch+1 != collector.Epoch() {
			return ErrEconomicCheckpoint
		}
		if hasPrevious {
			if previous.Epoch+1 != pending.State.Epoch {
				return ErrEconomicCheckpoint
			}
		} else if pending.State.Epoch != 1 {
			return ErrEconomicCheckpoint
		}
		return nil
	}
	if hasPrevious {
		if previous.Epoch+1 != collector.Epoch() {
			return ErrEconomicCheckpoint
		}
	} else if collector.Epoch() != 1 {
		return ErrEconomicCheckpoint
	}
	return nil
}

func writeAtomicEconomicCheckpoint(path string, raw []byte) error {
	if path == "" || len(raw) == 0 || len(raw) > maxEconomicCheckpointBytes+8+32 {
		return ErrEconomicCheckpoint
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
