package worldstate

import (
	"errors"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/state"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var (
	ErrObjectNotFound = errors.New("object not found")
	ErrObjectExists   = errors.New("object already exists")
)

type Backend interface {
	Root() types.Hash
	GetObject(id types.ObjectID) (object.Object, bool)
	Proof(id types.ObjectID) (object.Object, state.Proof, bool)
	Apply(consumed []types.ObjectID, created []object.Object) (types.Hash, error)
}

type Memory struct {
	mu      sync.RWMutex
	tree    *state.Tree
	objects map[types.ObjectID]object.Object
}

func NewMemory() *Memory {
	return &Memory{tree: state.NewTree(), objects: make(map[types.ObjectID]object.Object)}
}

func (m *Memory) Root() types.Hash {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tree.Root()
}

func (m *Memory) GetObject(id types.ObjectID) (object.Object, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.objects[id]
	if !ok {
		return object.Object{}, false
	}
	o.Data = append([]byte(nil), o.Data...)
	return o, true
}

func (m *Memory) Proof(id types.ObjectID) (object.Object, state.Proof, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.objects[id]
	if !ok {
		return object.Object{}, m.tree.Prove(types.Hash(id)), false
	}
	o.Data = append([]byte(nil), o.Data...)
	return o, m.tree.Prove(types.Hash(id)), true
}

func (m *Memory) Apply(consumed []types.ObjectID, created []object.Object) (types.Hash, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	seenConsumed := map[types.ObjectID]struct{}{}
	for _, id := range consumed {
		if _, duplicate := seenConsumed[id]; duplicate {
			return m.tree.Root(), ErrObjectNotFound
		}
		seenConsumed[id] = struct{}{}
		if _, ok := m.objects[id]; !ok {
			return m.tree.Root(), ErrObjectNotFound
		}
	}
	seenCreated := map[types.ObjectID]struct{}{}
	for _, o := range created {
		if err := o.Validate(); err != nil {
			return m.tree.Root(), err
		}
		if _, duplicate := seenCreated[o.ID]; duplicate {
			return m.tree.Root(), ErrObjectExists
		}
		seenCreated[o.ID] = struct{}{}
		if _, ok := m.objects[o.ID]; ok {
			if _, replacingConsumed := seenConsumed[o.ID]; !replacingConsumed {
				return m.tree.Root(), ErrObjectExists
			}
		}
	}

	updates := make(map[types.Hash][]byte, len(consumed)+len(created))
	for _, id := range consumed {
		delete(m.objects, id)
		updates[types.Hash(id)] = nil
	}
	for _, o := range created {
		copyObject := o
		copyObject.Data = append([]byte(nil), o.Data...)
		m.objects[o.ID] = copyObject
		h := o.Hash()
		updates[types.Hash(o.ID)] = h[:]
	}
	return m.tree.Apply(updates), nil
}
