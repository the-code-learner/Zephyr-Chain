package worldstate

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/state"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	walMagic             = "ZWL2"
	checkpointMagic      = "ZCP2"
	persistenceVersion   = uint16(1)
	maxWALRecordBytes    = 64 << 20
	maxCheckpointBytes   = 1 << 30
	maxCheckpointObjects = 10_000_000
)

var (
	ErrPersistenceNetwork = errors.New("v2 persisted state belongs to another network")
	ErrPersistenceCorrupt = errors.New("v2 persisted state is corrupt")
	ErrPersistenceClosed  = errors.New("v2 persisted state is closed")
)

type Disk struct {
	mu      sync.Mutex
	dir     string
	network types.NetworkID
	mem     *Memory
	objects map[types.ObjectID]object.Object
	seq     uint64
	wal     *os.File
	closed  bool
}

func OpenDisk(dir string, network types.NetworkID) (*Disk, error) {
	if types.IsZero32([32]byte(network)) {
		return nil, ErrPersistenceNetwork
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	d := &Disk{
		dir: dir, network: network, mem: NewMemory(),
		objects: make(map[types.ObjectID]object.Object),
	}
	if err := d.loadCheckpoint(); err != nil {
		return nil, err
	}
	wal, err := os.OpenFile(filepath.Join(dir, "state.wal"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	d.wal = wal
	if err := d.replayWAL(); err != nil {
		_ = wal.Close()
		return nil, err
	}
	if _, err := wal.Seek(0, io.SeekEnd); err != nil {
		_ = wal.Close()
		return nil, err
	}
	return d, nil
}

func (d *Disk) Root() types.Hash { return d.mem.Root() }

func (d *Disk) GetObject(id types.ObjectID) (object.Object, bool) { return d.mem.GetObject(id) }

func (d *Disk) Proof(id types.ObjectID) (object.Object, state.Proof, bool) { return d.mem.Proof(id) }

func (d *Disk) Apply(consumed []types.ObjectID, created []object.Object) (types.Hash, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return d.mem.Root(), ErrPersistenceClosed
	}
	if err := d.prevalidate(consumed, created); err != nil {
		return d.mem.Root(), err
	}
	next := d.seq + 1
	payload := encodeWALPayload(d.network, next, consumed, created)
	if err := d.appendWAL(payload); err != nil {
		return d.mem.Root(), err
	}
	root, err := d.mem.Apply(consumed, created)
	if err != nil {
		return d.mem.Root(), ErrPersistenceCorrupt
	}
	d.applyMirror(consumed, created)
	d.seq = next
	return root, nil
}

func (d *Disk) Sequence() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seq
}

// Checkpoint atomically materializes the current object set and then resets the
// WAL. A crash before WAL truncation is safe because replay ignores records at
// or below the checkpoint sequence.
func (d *Disk) Checkpoint() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrPersistenceClosed
	}
	objects := make([]object.Object, 0, len(d.objects))
	for _, item := range d.objects {
		copyItem := item
		copyItem.Data = append([]byte(nil), item.Data...)
		objects = append(objects, copyItem)
	}
	sort.Slice(objects, func(i, j int) bool { return bytes.Compare(objects[i].ID[:], objects[j].ID[:]) < 0 })

	var payload codec.Writer
	payload.U16(persistenceVersion)
	payload.Fixed(d.network[:])
	payload.U64(d.seq)
	payload.U32(uint32(len(objects)))
	for _, item := range objects {
		payload.Bytes(item.CanonicalBytes())
	}
	root := d.mem.Root()
	payload.Fixed(root[:])
	rawPayload := payload.BytesCopy()
	digest := codec.DomainHash("zephyr/state-checkpoint/v2", rawPayload)
	var file bytes.Buffer
	file.WriteString(checkpointMagic)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(rawPayload)))
	file.Write(n[:])
	file.Write(rawPayload)
	file.Write(digest[:])

	tmp := filepath.Join(d.dir, "state.checkpoint.tmp")
	final := filepath.Join(d.dir, "state.checkpoint")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(file.Bytes()); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := syncDir(d.dir); err != nil {
		return err
	}
	if err := d.wal.Truncate(0); err != nil {
		return err
	}
	if _, err := d.wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return d.wal.Sync()
}

func (d *Disk) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	if err := d.wal.Sync(); err != nil {
		_ = d.wal.Close()
		return err
	}
	return d.wal.Close()
}

func (d *Disk) prevalidate(consumed []types.ObjectID, created []object.Object) error {
	seenConsumed := make(map[types.ObjectID]struct{}, len(consumed))
	for _, id := range consumed {
		if _, duplicate := seenConsumed[id]; duplicate {
			return ErrObjectNotFound
		}
		seenConsumed[id] = struct{}{}
		if _, ok := d.objects[id]; !ok {
			return ErrObjectNotFound
		}
	}
	seenCreated := make(map[types.ObjectID]struct{}, len(created))
	for _, item := range created {
		if err := item.Validate(); err != nil {
			return err
		}
		if _, duplicate := seenCreated[item.ID]; duplicate {
			return ErrObjectExists
		}
		seenCreated[item.ID] = struct{}{}
		if _, exists := d.objects[item.ID]; exists {
			if _, replacing := seenConsumed[item.ID]; !replacing {
				return ErrObjectExists
			}
		}
	}
	return nil
}

func (d *Disk) appendWAL(payload []byte) error {
	if len(payload) == 0 || len(payload) > maxWALRecordBytes {
		return ErrPersistenceCorrupt
	}
	var header [8]byte
	copy(header[:4], walMagic)
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	checksum := crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli))
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], checksum)
	if _, err := d.wal.Write(header[:]); err != nil {
		return err
	}
	if _, err := d.wal.Write(payload); err != nil {
		return err
	}
	if _, err := d.wal.Write(sum[:]); err != nil {
		return err
	}
	return d.wal.Sync()
}

func encodeWALPayload(network types.NetworkID, seq uint64, consumed []types.ObjectID, created []object.Object) []byte {
	var w codec.Writer
	w.U16(persistenceVersion)
	w.Fixed(network[:])
	w.U64(seq)
	w.U32(uint32(len(consumed)))
	for _, id := range consumed {
		w.Fixed(id[:])
	}
	w.U32(uint32(len(created)))
	for _, item := range created {
		w.Bytes(item.CanonicalBytes())
	}
	return w.BytesCopy()
}

func parseWALPayload(payload []byte) (types.NetworkID, uint64, []types.ObjectID, []object.Object, error) {
	r := codec.NewReader(payload)
	version, err := r.U16()
	if err != nil || version != persistenceVersion {
		return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
	}
	networkBytes, err := r.Fixed(32)
	if err != nil {
		return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
	}
	var network types.NetworkID
	copy(network[:], networkBytes)
	seq, err := r.U64()
	if err != nil || seq == 0 {
		return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
	}
	consumedCount, err := r.U32()
	if err != nil || consumedCount > 1_000_000 {
		return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
	}
	consumed := make([]types.ObjectID, int(consumedCount))
	for i := range consumed {
		raw, err := r.Fixed(32)
		if err != nil {
			return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
		}
		copy(consumed[i][:], raw)
	}
	createdCount, err := r.U32()
	if err != nil || createdCount > 1_000_000 {
		return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
	}
	created := make([]object.Object, int(createdCount))
	for i := range created {
		raw, err := r.Bytes(object.MaxObjectDataBytes + 128)
		if err != nil {
			return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
		}
		created[i], err = object.ParseObject(raw)
		if err != nil {
			return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
		}
	}
	if err := r.Done(); err != nil {
		return types.NetworkID{}, 0, nil, nil, ErrPersistenceCorrupt
	}
	return network, seq, consumed, created, nil
}

func (d *Disk) replayWAL() error {
	if _, err := d.wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(d.wal)
	var offset int64
	for {
		var header [8]byte
		n, err := io.ReadFull(reader, header[:])
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			if err := d.wal.Truncate(offset); err != nil {
				return err
			}
			break
		}
		if err != nil || n != len(header) || string(header[:4]) != walMagic {
			return ErrPersistenceCorrupt
		}
		length := binary.BigEndian.Uint32(header[4:])
		if length == 0 || length > maxWALRecordBytes {
			return ErrPersistenceCorrupt
		}
		payload := make([]byte, int(length))
		if _, err := io.ReadFull(reader, payload); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				if err := d.wal.Truncate(offset); err != nil {
					return err
				}
				break
			}
			return err
		}
		var checksumRaw [4]byte
		if _, err := io.ReadFull(reader, checksumRaw[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				if err := d.wal.Truncate(offset); err != nil {
					return err
				}
				break
			}
			return err
		}
		expected := binary.BigEndian.Uint32(checksumRaw[:])
		actual := crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli))
		if expected != actual {
			return ErrPersistenceCorrupt
		}
		network, seq, consumed, created, err := parseWALPayload(payload)
		if err != nil {
			return err
		}
		if network != d.network {
			return ErrPersistenceNetwork
		}
		offset += int64(8 + len(payload) + 4)
		if seq <= d.seq {
			continue
		}
		if seq != d.seq+1 {
			return ErrPersistenceCorrupt
		}
		if err := d.prevalidate(consumed, created); err != nil {
			return ErrPersistenceCorrupt
		}
		if _, err := d.mem.Apply(consumed, created); err != nil {
			return ErrPersistenceCorrupt
		}
		d.applyMirror(consumed, created)
		d.seq = seq
	}
	_, err := d.wal.Seek(0, io.SeekEnd)
	return err
}

func (d *Disk) loadCheckpoint() error {
	path := filepath.Join(d.dir, "state.checkpoint")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(raw) < 4+4+32 || string(raw[:4]) != checkpointMagic {
		return ErrPersistenceCorrupt
	}
	length := binary.BigEndian.Uint32(raw[4:8])
	if length == 0 || length > maxCheckpointBytes || len(raw) != 8+int(length)+32 {
		return ErrPersistenceCorrupt
	}
	payload := raw[8 : 8+int(length)]
	digest := codec.DomainHash("zephyr/state-checkpoint/v2", payload)
	if !bytes.Equal(digest[:], raw[8+int(length):]) {
		return ErrPersistenceCorrupt
	}
	r := codec.NewReader(payload)
	version, err := r.U16()
	if err != nil || version != persistenceVersion {
		return ErrPersistenceCorrupt
	}
	networkBytes, err := r.Fixed(32)
	if err != nil {
		return ErrPersistenceCorrupt
	}
	var network types.NetworkID
	copy(network[:], networkBytes)
	if network != d.network {
		return ErrPersistenceNetwork
	}
	seq, err := r.U64()
	if err != nil {
		return ErrPersistenceCorrupt
	}
	count, err := r.U32()
	if err != nil || count > maxCheckpointObjects {
		return ErrPersistenceCorrupt
	}
	objects := make([]object.Object, int(count))
	for i := range objects {
		itemBytes, err := r.Bytes(object.MaxObjectDataBytes + 128)
		if err != nil {
			return ErrPersistenceCorrupt
		}
		objects[i], err = object.ParseObject(itemBytes)
		if err != nil {
			return ErrPersistenceCorrupt
		}
	}
	rootBytes, err := r.Fixed(32)
	if err != nil || r.Done() != nil {
		return ErrPersistenceCorrupt
	}
	var expectedRoot types.Hash
	copy(expectedRoot[:], rootBytes)
	if len(objects) > 0 {
		if _, err := d.mem.Apply(nil, objects); err != nil {
			return ErrPersistenceCorrupt
		}
	}
	if d.mem.Root() != expectedRoot {
		return ErrPersistenceCorrupt
	}
	for _, item := range objects {
		d.objects[item.ID] = item
	}
	d.seq = seq
	return nil
}

func (d *Disk) applyMirror(consumed []types.ObjectID, created []object.Object) {
	for _, id := range consumed {
		delete(d.objects, id)
	}
	for _, item := range created {
		copyItem := item
		copyItem.Data = append([]byte(nil), item.Data...)
		d.objects[item.ID] = copyItem
	}
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
