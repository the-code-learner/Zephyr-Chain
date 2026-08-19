package execution

import (
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/assets"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const tokenMintOutputIndex uint32 = 0x81000000

func (e Engine) executeMintToken(t tx.Transaction, payload []byte) (Result, error) {
	request, err := assets.ParseMintToken(payload)
	if err != nil {
		return Result{}, err
	}
	definitionObject, definition, err := tokenDefinitionWitness(t, request.DefinitionObject)
	if err != nil || definition.TokenID == e.NativeToken || definition.MintAuthority != t.Sender {
		return Result{}, ErrTokenPolicy
	}
	next, err := definition.Mint(request.Amount)
	if err != nil {
		return Result{}, ErrTokenPolicy
	}

	created, consumed, err := e.feeOnlyOutputsExcluding(t, request.DefinitionObject, nil)
	if err != nil {
		return Result{}, err
	}
	nextRaw, err := next.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	updatedDefinition := definitionObject
	updatedDefinition.Version++
	updatedDefinition.Data = nextRaw
	created = append(created, updatedDefinition)
	consumed = append(consumed, definitionObject.ID)

	minted, err := object.NewCoinOutput(request.Recipient, definition.TokenID, request.Amount)
	if err != nil {
		return Result{}, err
	}
	router := sharding.Router{ShardCount: e.ShardCount}
	destination, err := router.ShardForAccount(request.Recipient)
	if err != nil || destination != t.ShardID {
		return Result{}, ErrTokenPolicy
	}
	txID := t.ID()
	created = append(created, object.Object{
		ID: types.ObjectIDForShard(txID, tokenMintOutputIndex, t.ShardID), Version: 1,
		Owner: minted.Owner, Kind: minted.Kind, Data: minted.Data,
	})
	return Result{Consumed: consumed, Created: created, TxID: txID}, nil
}

func (e Engine) executeBurnToken(t tx.Transaction, payload []byte) (Result, error) {
	request, err := assets.ParseBurnToken(payload)
	if err != nil {
		return Result{}, err
	}
	definitionObject, definition, err := tokenDefinitionWitness(t, request.DefinitionObject)
	if err != nil || definition.TokenID == e.NativeToken || !definition.Burnable {
		return Result{}, ErrTokenPolicy
	}
	next, err := definition.Burn(request.Amount)
	if err != nil {
		return Result{}, ErrTokenPolicy
	}

	var nativeIn, tokenIn uint64
	consumed := make([]types.ObjectID, 0, len(t.Inputs))
	for _, witness := range t.Witnesses {
		if witness.Object.ID == request.DefinitionObject {
			continue
		}
		if witness.Object.Kind != object.KindCoin || witness.Object.Owner != t.Sender {
			return Result{}, ErrOwnership
		}
		coin, err := object.ParseCoin(witness.Object.Data)
		if err != nil {
			return Result{}, err
		}
		switch coin.Token {
		case e.NativeToken:
			if math.MaxUint64-nativeIn < coin.Amount {
				return Result{}, ErrOverflow
			}
			nativeIn += coin.Amount
		case definition.TokenID:
			if math.MaxUint64-tokenIn < coin.Amount {
				return Result{}, ErrOverflow
			}
			tokenIn += coin.Amount
		default:
			return Result{}, ErrConservation
		}
		consumed = append(consumed, witness.Object.ID)
	}

	var nativeOut, tokenOut uint64
	created := make([]object.Object, 0, len(t.Outputs)+1)
	router := sharding.Router{ShardCount: e.ShardCount}
	for i, spec := range t.Outputs {
		if spec.Kind != object.KindCoin {
			return Result{}, ErrConservation
		}
		destination, err := router.ShardForAccount(spec.Owner)
		if err != nil || destination != t.ShardID {
			return Result{}, ErrShard
		}
		coin, err := object.ParseCoin(spec.Data)
		if err != nil {
			return Result{}, err
		}
		switch coin.Token {
		case e.NativeToken:
			if math.MaxUint64-nativeOut < coin.Amount {
				return Result{}, ErrOverflow
			}
			nativeOut += coin.Amount
		case definition.TokenID:
			if spec.Owner != t.Sender || math.MaxUint64-tokenOut < coin.Amount {
				return Result{}, ErrTokenPolicy
			}
			tokenOut += coin.Amount
		default:
			return Result{}, ErrConservation
		}
		created = append(created, object.Object{
			ID: types.ObjectIDForShard(t.ID(), uint32(i), t.ShardID), Version: 1,
			Owner: spec.Owner, Kind: spec.Kind, Data: append([]byte(nil), spec.Data...),
		})
	}
	if math.MaxUint64-nativeOut < t.Fee || nativeIn != nativeOut+t.Fee {
		return Result{}, ErrConservation
	}
	if math.MaxUint64-tokenOut < request.Amount || tokenIn != tokenOut+request.Amount {
		return Result{}, ErrConservation
	}

	nextRaw, err := next.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	updatedDefinition := definitionObject
	updatedDefinition.Version++
	updatedDefinition.Data = nextRaw
	created = append(created, updatedDefinition)
	consumed = append(consumed, definitionObject.ID)
	return Result{Consumed: consumed, Created: created, TxID: t.ID()}, nil
}

func tokenDefinitionWitness(t tx.Transaction, id types.ObjectID) (object.Object, assets.Definition, error) {
	for _, witness := range t.Witnesses {
		if witness.Object.ID != id {
			continue
		}
		if witness.Object.Kind != object.KindTokenDefinition {
			return object.Object{}, assets.Definition{}, ErrTokenPolicy
		}
		definition, err := assets.ParseDefinition(witness.Object.Data)
		if err != nil {
			return object.Object{}, assets.Definition{}, err
		}
		return witness.Object, definition, nil
	}
	return object.Object{}, assets.Definition{}, ErrTokenPolicy
}
