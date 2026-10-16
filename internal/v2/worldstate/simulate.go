package worldstate

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Simulator is implemented by state backends that can calculate the root of a
// delta without mutating committed state.
type Simulator interface {
	Simulate(consumed []types.ObjectID, created []object.Object) (types.Hash, error)
}

func (m *Memory) Simulate(consumed []types.ObjectID, created []object.Object) (types.Hash, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seenConsumed := make(map[types.ObjectID]struct{}, len(consumed))
	for _, id := range consumed {
		if _, duplicate := seenConsumed[id]; duplicate {
			return m.tree.Root(), ErrObjectNotFound
		}
		seenConsumed[id] = struct{}{}
		if _, ok := m.objects[id]; !ok {
			return m.tree.Root(), ErrObjectNotFound
		}
	}
	seenCreated := make(map[types.ObjectID]struct{}, len(created))
	for _, item := range created {
		if err := item.Validate(); err != nil {
			return m.tree.Root(), err
		}
		if _, duplicate := seenCreated[item.ID]; duplicate {
			return m.tree.Root(), ErrObjectExists
		}
		seenCreated[item.ID] = struct{}{}
		if _, exists := m.objects[item.ID]; exists {
			if _, replacing := seenConsumed[item.ID]; !replacing {
				return m.tree.Root(), ErrObjectExists
			}
		}
	}

	updates := make(map[types.Hash][]byte, len(consumed)+len(created))
	for _, id := range consumed {
		updates[types.Hash(id)] = nil
	}
	for _, item := range created {
		hash := item.Hash()
		updates[types.Hash(item.ID)] = hash[:]
	}
	return m.tree.Preview(updates), nil
}

func (d *Disk) Simulate(consumed []types.ObjectID, created []object.Object) (types.Hash, error) {
	return d.mem.Simulate(consumed, created)
}
