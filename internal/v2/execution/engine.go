package execution

import (
	"errors"
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/assets"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/contracts"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var (
	ErrUnsupportedOperation = errors.New("unsupported v2 operation")
	ErrOwnership            = errors.New("transaction does not own an input object")
	ErrConservation         = errors.New("token conservation failed")
	ErrOverflow             = errors.New("token amount overflow")
	ErrShard                = errors.New("transaction routed to wrong shard")
	ErrTokenPolicy          = errors.New("native token policy rejected operation")
)

type OutboundOutput struct {
	DestinationShard uint32
	OutputIndex      uint32
	Output           object.OutputSpec
}

type Result struct {
	Consumed []types.ObjectID
	Created  []object.Object
	Outbound []OutboundOutput
	TxID     types.Hash
}

type Engine struct {
	Network          types.NetworkID
	NativeToken      types.TokenID
	ShardCount       uint32
	Height           uint64
	ContractRuntimes map[string]contracts.MeteredRuntime
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
	router := sharding.Router{ShardCount: e.ShardCount}
	senderShard, err := router.ShardForAccount(t.Sender)
	if err != nil || t.ShardID != senderShard {
		return Result{}, ErrShard
	}
	for _, in := range t.Inputs {
		inputShard, err := router.ShardForObject(in.ObjectID)
		if err != nil || inputShard != t.ShardID {
			return Result{}, ErrShard
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
		return e.executeExtended(t, t.Operations[0])
	}
}

func (e Engine) executeTransfer(t tx.Transaction) (Result, error) {
	inputTotals := map[types.TokenID]uint64{}
	definitions := map[types.TokenID]assets.Definition{}
	consumed := make([]types.ObjectID, 0, len(t.Inputs))
	for _, w := range t.Witnesses {
		switch w.Object.Kind {
		case object.KindCoin:
			if w.Object.Owner != t.Sender {
				return Result{}, ErrOwnership
			}
			coin, err := object.ParseCoin(w.Object.Data)
			if err != nil {
				return Result{}, err
			}
			if err := add(inputTotals, coin.Token, coin.Amount); err != nil {
				return Result{}, err
			}
			consumed = append(consumed, w.Object.ID)
		case object.KindTokenDefinition:
			definition, err := assets.ParseDefinition(w.Object.Data)
			if err != nil || definition.TokenID == e.NativeToken {
				return Result{}, ErrTokenPolicy
			}
			if _, duplicate := definitions[definition.TokenID]; duplicate {
				return Result{}, ErrTokenPolicy
			}
			definitions[definition.TokenID] = definition
		default:
			return Result{}, ErrOwnership
		}
	}

	outputTotals := map[types.TokenID]uint64{}
	txID := t.ID()
	created := make([]object.Object, 0, len(t.Outputs))
	outbound := make([]OutboundOutput, 0)
	router := sharding.Router{ShardCount: e.ShardCount}
	for i, spec := range t.Outputs {
		if spec.Kind != object.KindCoin {
			return Result{}, ErrConservation
		}
		coin, err := object.ParseCoin(spec.Data)
		if err != nil {
			return Result{}, err
		}
		if coin.Token != e.NativeToken {
			definition, ok := definitions[coin.Token]
			if !ok || !definition.Transferable {
				return Result{}, ErrTokenPolicy
			}
		}
		if err := add(outputTotals, coin.Token, coin.Amount); err != nil {
			return Result{}, err
		}
		destination, err := router.ShardForAccount(spec.Owner)
		if err != nil {
			return Result{}, ErrShard
		}
		if destination == t.ShardID {
			created = append(created, object.Object{
				ID: types.ObjectIDForShard(txID, uint32(i), destination), Version: 1,
				Owner: spec.Owner, Kind: spec.Kind, Data: append([]byte(nil), spec.Data...),
			})
		} else {
			outbound = append(outbound, OutboundOutput{DestinationShard: destination, OutputIndex: uint32(i), Output: spec})
		}
	}

	for token, inAmount := range inputTotals {
		required := outputTotals[token]
		if token == e.NativeToken {
			if math.MaxUint64-required < t.Fee {
				return Result{}, ErrOverflow
			}
			required += t.Fee
		} else {
			definition, ok := definitions[token]
			if !ok || !definition.Transferable {
				return Result{}, ErrTokenPolicy
			}
		}
		if inAmount != required {
			return Result{}, ErrConservation
		}
		delete(outputTotals, token)
	}
	if len(outputTotals) != 0 {
		return Result{}, ErrConservation
	}

	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: txID}, nil
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
	outbound := make([]OutboundOutput, 0)
	router := sharding.Router{ShardCount: e.ShardCount}
	for i, spec := range t.Outputs {
		coin, err := object.ParseCoin(spec.Data)
		if err != nil || spec.Kind != object.KindCoin || coin.Token != e.NativeToken {
			return Result{}, ErrConservation
		}
		if math.MaxUint64-nativeOut < coin.Amount {
			return Result{}, ErrOverflow
		}
		nativeOut += coin.Amount
		destination, err := router.ShardForAccount(spec.Owner)
		if err != nil {
			return Result{}, ErrShard
		}
		if destination == t.ShardID {
			created = append(created, object.Object{
				ID: types.ObjectIDForShard(txID, uint32(i), destination), Version: 1,
				Owner: spec.Owner, Kind: spec.Kind, Data: append([]byte(nil), spec.Data...),
			})
		} else {
			outbound = append(outbound, OutboundOutput{DestinationShard: destination, OutputIndex: uint32(i), Output: spec})
		}
	}
	if math.MaxUint64-nativeOut < t.Fee || nativeIn != nativeOut+t.Fee {
		return Result{}, ErrConservation
	}

	tokenID := types.TokenIDFromTransaction(txID, 0)
	definition := assets.Definition{
		TokenID: tokenID, Name: create.Name, Symbol: create.Symbol, Decimals: create.Decimals, SupplyPolicy: create.SupplyPolicy,
		MaxSupply: create.MaxSupply, CurrentSupply: create.InitialSupply,
		MintAuthority: create.MintAuthority, Burnable: create.Burnable, Transferable: create.Transferable,
	}
	defData, err := definition.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	defID := types.ObjectIDForShard(txID, 0x80000000, t.ShardID)
	created = append(created, object.Object{
		ID: defID, Version: 1, Owner: t.Sender, Kind: object.KindTokenDefinition, Data: defData,
	})
	initialCoin, err := object.NewCoinOutput(t.Sender, tokenID, create.InitialSupply)
	if err != nil {
		return Result{}, err
	}
	created = append(created, object.Object{
		ID: types.ObjectIDForShard(txID, 0x80000001, t.ShardID), Version: 1,
		Owner: initialCoin.Owner, Kind: initialCoin.Kind, Data: initialCoin.Data,
	})
	consumed := make([]types.ObjectID, len(t.Inputs))
	for i, in := range t.Inputs {
		consumed[i] = in.ObjectID
	}
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: txID}, nil
}

func add(totals map[types.TokenID]uint64, token types.TokenID, amount uint64) error {
	current := totals[token]
	if math.MaxUint64-current < amount {
		return ErrOverflow
	}
	totals[token] = current + amount
	return nil
}
