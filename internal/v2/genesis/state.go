package genesis

import (
	"encoding/binary"
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

var ErrGenesisState = errors.New("invalid or partially initialized Zephyr v2 genesis state")

func SeedStates(g Config, states map[uint32]worldstate.Backend) error {
	if err := g.Validate(); err != nil || len(states) != int(g.InitialShardCount) {
		return ErrGenesisState
	}
	network, err := g.NetworkID()
	if err != nil {
		return err
	}
	native, err := g.NativeTokenID()
	if err != nil {
		return err
	}
	canonical, err := g.CanonicalBytes()
	if err != nil {
		return err
	}
	genesisHash := types.Hash(codec.DomainHash("zephyr/genesis/state/v2", canonical))
	emptyRoot := worldstate.NewMemory().Root()

	for shard := uint32(0); shard < g.InitialShardCount; shard++ {
		store, ok := states[shard]
		if !ok || store == nil {
			return ErrGenesisState
		}
		markerID := GenesisMarkerID(network, shard)
		if marker, exists := store.GetObject(markerID); exists {
			if marker.Kind != object.KindSystem || len(marker.Data) != 36 || string(marker.Data[:32]) != string(genesisHash[:]) || binary.BigEndian.Uint32(marker.Data[32:]) != shard {
				return ErrGenesisState
			}
			continue
		}
		if store.Root() != emptyRoot {
			return ErrGenesisState
		}
		created := make([]object.Object, 0)
		for _, allocation := range g.Allocations {
			if types.AccountShard(allocation.Owner, g.InitialShardCount) != shard {
				continue
			}
			coin, err := object.NewCoinOutput(allocation.Owner, native, allocation.Amount)
			if err != nil {
				return err
			}
			created = append(created, object.Object{ID: GenesisAllocationID(network, allocation.Owner, shard), Version: 1, Owner: allocation.Owner, Kind: coin.Kind, Data: coin.Data})
		}
		markerData := make([]byte, 36)
		copy(markerData[:32], genesisHash[:])
		binary.BigEndian.PutUint32(markerData[32:], shard)
		created = append(created, object.Object{ID: markerID, Version: 1, Kind: object.KindSystem, Data: markerData})
		if _, err := store.Apply(nil, created); err != nil {
			return err
		}
	}
	return nil
}

func GenesisMarkerID(network types.NetworkID, shard uint32) types.ObjectID {
	return types.ObjectIDForShard(types.Hash(network), 0xfffffff0, shard)
}

func GenesisAllocationID(network types.NetworkID, owner types.AccountID, shard uint32) types.ObjectID {
	var w codec.Writer
	w.Fixed(network[:])
	w.Fixed(owner[:])
	seed := types.Hash(codec.DomainHash("zephyr/genesis/allocation/v2", w.BytesCopy()))
	return types.ObjectIDForShard(seed, 0, shard)
}
