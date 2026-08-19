package execution

import (
	"errors"
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/assets"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var (
	ErrUnsupportedOperation = errors.New("unsupported v2 operation")
	ErrOwnership            = errors.New("transaction does not own an input object")
	ErrConservation         = errors.New("token conservation failed")
	ErrOverflow             = errors.New("token amount overflow")
	ErrShard                = errors.New("transaction routed to wrong shard")
)

type Result struct {
	Consumed []types.ObjectID
	Created  []object.Object
	TxID     types.Hash
}

type Engine struct {
	Network     types.NetworkID
	NativeToken types.TokenID
	ShardCount  uint32
}

func (e Engine) Execute(t tx.Transaction) (Result, error) {
	if err := t.VerifyForNetwork(e.Network); err != nil {
		return Result{}, err
	}
	if err := t.VerifyWitnesses(); err != nil {
		return Result{}, err
	}
	if e.ShardCount == 0 {
		e.ShardCount = 1
	}
	if len(t.Inputs) > 0 {
		expected := shardForObject(t.Inputs[0].ObjectID, e.ShardCount)
		if t.ShardID != expected {
			return Result{}, ErrShard
		}
		for _, in := range t.Inputs[1:] {
			if shardForObject(in.ObjectID, e.ShardCount) != expected {
				return Result{}, ErrShard
			}
		}
	}
	if len(t.Operations) != 1 {
		return Result{}, ErrUnsupportedOperation
	}
	switch t.Operations[0].Kind {
	case tx.OpTransfer:
		return e.executeTransfer(t)
	case tx.OpCreateToken:
		return e.executeCreateToken(t, t.Operations[0].Payload)
	default:
		return Result{}, ErrUnsupportedOperation
	}
}

func (e Engine) executeTransfer(t tx.Transaction) (Result, error) {
	inputTotals := map[types.TokenID]uint64{}
	for _, w := range t.Witnesses {
		if w.Object.Owner != t.Sender || w.Object.Kind != object.KindCoin {
			return Result{}, ErrOwnership
		}
		coin, err := object.ParseCoin(w.Object.Data)
		if err != nil {
			return Result{}, err
		}
		if err := add(inputTotals, coin.Token, coin.Amount); err != nil {
			return Result{}, err
		}
	}

	outputTotals := map[types.TokenID]uint64{}
	txID := t.ID()
	created := make([]object.Object, 0, len(t.Outputs))
	for i, spec := range t.Outputs {
		if spec.Kind != object.KindCoin {
			return Result{}, ErrConservation
		}
		coin, err := object.ParseCoin(spec.Data)
		if err != nil {
			return Result{}, err
		}
		if err := add(outputTotals, coin.Token, coin.Amount); err != nil {
			return Result{}, err
		}
		created = append(created, object.Object{
			ID: types.ObjectIDFromTransaction(txID, uint32(i)), Version: 1,
			Owner: spec.Owner, Kind: spec.Kind, Data: append([]byte(nil), spec.Data...),
		})
	}

	for token, inAmount := range inputTotals {
		required := outputTotals[token]
		if token == e.NativeToken {
			if math.MaxUint64-required < t.Fee {
				return Result{}, ErrOverflow
			}
			required += t.Fee
		}
		if inAmount != required {
			return Result{}, ErrConservation
		}
		delete(outputTotals, token)
	}
	if len(outputTotals) != 0 {
		return Result{}, ErrConservation
	}

	consumed := make([]types.ObjectID, len(t.Inputs))
	for i, in := range t.Inputs {
		consumed[i] = in.ObjectID
	}
	return Result{Consumed: consumed, Created: created, TxID: txID}, nil
}

func (e Engine) executeCreateToken(t tx.Transaction, payload []byte) (Result, error) {
	create, err := assets.ParseCreateToken(payload)
	if err != nil {
		return Result{}, err
	}
	if create.MintAuthority != t.Sender {
		return Result{}, ErrOwnership
	}
	var nativeIn uint64
	for _, w := range t.Witnesses {
		if w.Object.Owner != t.Sender || w.Object.Kind != object.KindCoin {
			return Result{}, ErrOwnership
		}
		coin, err := object.ParseCoin(w.Object.Data)
		if err != nil {
			return Result{}, err
		}
		if coin.Token != e.NativeToken {
			return Result{}, ErrConservation
		}
		if math.MaxUint64-nativeIn < coin.Amount {
			return Result{}, ErrOverflow
		}
		nativeIn += coin.Amount
	}
	var nativeOut uint64
	txID := t.ID()
	created := make([]object.Object, 0, len(t.Outputs)+2)
	for i, spec := range t.Outputs {
		coin, err := object.ParseCoin(spec.Data)
		if err != nil || spec.Kind != object.KindCoin || coin.Token != e.NativeToken {
			return Result{}, ErrConservation
		}
		if math.MaxUint64-nativeOut < coin.Amount {
			return Result{}, ErrOverflow
		}
		nativeOut += coin.Amount
		created = append(created, object.Object{
			ID: types.ObjectIDFromTransaction(txID, uint32(i)), Version: 1,
			Owner: spec.Owner, Kind: spec.Kind, Data: append([]byte(nil), spec.Data...),
		})
	}
	if math.MaxUint64-nativeOut < t.Fee || nativeIn != nativeOut+t.Fee {
		return Result{}, ErrConservation
	}

	tokenID := types.TokenIDFromTransaction(txID, 0)
	definition := assets.Definition{
		TokenID: tokenID, Name: create.Name, Symbol: create.Symbol, Decimals: create.Decimals,
		MaxSupply: create.MaxSupply, CurrentSupply: create.InitialSupply,
		MintAuthority: create.MintAuthority, Burnable: create.Burnable, Transferable: create.Transferable,
	}
	defData, err := definition.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	defID := types.ObjectIDFromTransaction(txID, 0x80000000)
	created = append(created, object.Object{
		ID: defID, Version: 1, Owner: t.Sender, Kind: object.KindTokenDefinition, Data: defData,
	})
	initialCoin, err := object.NewCoinOutput(t.Sender, tokenID, create.InitialSupply)
	if err != nil {
		return Result{}, err
	}
	created = append(created, object.Object{
		ID: types.ObjectIDFromTransaction(txID, 0x80000001), Version: 1,
		Owner: initialCoin.Owner, Kind: initialCoin.Kind, Data: initialCoin.Data,
	})
	consumed := make([]types.ObjectID, len(t.Inputs))
	for i, in := range t.Inputs {
		consumed[i] = in.ObjectID
	}
	return Result{Consumed: consumed, Created: created, TxID: txID}, nil
}

func add(totals map[types.TokenID]uint64, token types.TokenID, amount uint64) error {
	current := totals[token]
	if math.MaxUint64-current < amount {
		return ErrOverflow
	}
	totals[token] = current + amount
	return nil
}

func shardForObject(id types.ObjectID, shardCount uint32) uint32 {
	if shardCount <= 1 {
		return 0
	}
	raw := types.Hash(id)
	v := uint64(raw[0])<<56 | uint64(raw[1])<<48 | uint64(raw[2])<<40 | uint64(raw[3])<<32 |
		uint64(raw[4])<<24 | uint64(raw[5])<<16 | uint64(raw[6])<<8 | uint64(raw[7])
	return uint32(v % uint64(shardCount))
}
