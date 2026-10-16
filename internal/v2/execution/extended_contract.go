package execution

import (
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/contracts"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	contractObjectIndex  uint32 = 0xA0000000
	contractStateIndex   uint32 = 0xA0000001
	contractReceiptIndex uint32 = 0xA0000010
)

func (e Engine) executeExtended(t tx.Transaction, op tx.Operation) (Result, error) {
	switch op.Kind {
	case tx.OpDeployContract:
		return e.executeDeployContract(t, op.Payload)
	case tx.OpContractCall:
		return e.executeContractCall(t, op.Payload)
	case tx.OpMintToken:
		return e.executeMintToken(t, op.Payload)
	case tx.OpBurnToken:
		return e.executeBurnToken(t, op.Payload)
	case tx.OpComputeOffer, tx.OpComputeJob, tx.OpComputeResult, tx.OpComputeAccept, tx.OpComputeIngestAssignment, tx.OpComputeIngestResult, tx.OpComputeFinalize, tx.OpComputeResolveReplicated, tx.OpComputeExpire:
		return e.executeCompute(t, op)
	default:
		return Result{}, ErrUnsupportedOperation
	}
}

func (e Engine) contractRuntime(name string) (contracts.MeteredRuntime, bool) {
	if e.ContractRuntimes != nil {
		if runtime, ok := e.ContractRuntimes[name]; ok && runtime.Inner != nil {
			return runtime, true
		}
	}
	if name == contracts.RuntimeZephyrScriptV1 {
		return contracts.MeteredRuntime{Inner: contracts.ScriptRuntime{}}, true
	}
	return contracts.MeteredRuntime{}, false
}

func (e Engine) executeDeployContract(t tx.Transaction, payload []byte) (Result, error) {
	deployment, err := contracts.ParseDeployment(payload)
	if err != nil || deployment.UpgradeAuthority != t.Sender {
		return Result{}, ErrOwnership
	}
	runtime, ok := e.contractRuntime(deployment.Runtime)
	if !ok {
		return Result{}, ErrUnsupportedOperation
	}
	if err := runtime.ValidateModule(deployment.Code); err != nil {
		return Result{}, err
	}
	created, consumed, err := e.feeOnlyOutputs(t)
	if err != nil {
		return Result{}, err
	}
	txID := t.ID()
	contractID := types.ContractIDFromTransaction(txID, 0)
	storedRaw, err := (contracts.StoredContract{ID: contractID, Deployment: deployment}).MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	created = append(created, object.Object{ID: types.ObjectIDForShard(txID, contractObjectIndex, t.ShardID), Version: 1, Owner: t.Sender, Kind: object.KindContract, Data: storedRaw})
	if len(deployment.InitialState) > 0 {
		created = append(created, object.Object{ID: types.ObjectIDForShard(txID, contractStateIndex, t.ShardID), Version: 1, Owner: t.Sender, Kind: object.KindContractState, Data: append([]byte(nil), deployment.InitialState...)})
	}
	return Result{Consumed: consumed, Created: created, TxID: txID}, nil
}

func (e Engine) executeContractCall(t tx.Transaction, payload []byte) (Result, error) {
	call, err := contracts.ParseCall(payload)
	if err != nil {
		return Result{}, err
	}
	witnesses := make(map[types.ObjectID]tx.Witness, len(t.Witnesses))
	for _, witness := range t.Witnesses {
		witnesses[witness.Object.ID] = witness
	}
	contractWitness, ok := witnesses[call.ContractObject]
	if !ok || contractWitness.Object.Kind != object.KindContract {
		return Result{}, ErrOwnership
	}
	stored, err := contracts.ParseStoredContract(contractWitness.Object.Data)
	if err != nil {
		return Result{}, err
	}
	runtime, ok := e.contractRuntime(stored.Deployment.Runtime)
	if !ok {
		return Result{}, ErrUnsupportedOperation
	}
	readValues := make(map[types.ObjectID][]byte, len(call.Accesses))
	for _, access := range call.Accesses {
		witness, ok := witnesses[access.ObjectID]
		if !ok || witness.Object.Kind != object.KindContractState {
			return Result{}, ErrOwnership
		}
		readValues[access.ObjectID] = append([]byte(nil), witness.Object.Data...)
	}
	result, err := runtime.Execute(contracts.Request{ContractID: stored.ID, Runtime: stored.Deployment.Runtime, Code: stored.Deployment.Code, Entrypoint: call.Entrypoint, Arguments: call.Arguments, Accesses: call.Accesses, ReadValues: readValues, FuelLimit: call.FuelLimit})
	if err != nil {
		return Result{}, err
	}
	created, consumed, err := e.feeOnlyOutputsExcluding(t, call.ContractObject, call.Accesses)
	if err != nil {
		return Result{}, err
	}
	for id, value := range result.Writes {
		witness := witnesses[id]
		consumed = append(consumed, id)
		updated := witness.Object
		updated.Version++
		updated.Data = append([]byte(nil), value...)
		created = append(created, updated)
	}
	eventLeaves := make([]types.Hash, len(result.Events))
	for i, event := range result.Events {
		eventLeaves[i] = merkle.Leaf("contract-event", event)
	}
	receipt := contracts.Receipt{ContractID: stored.ID, FuelUsed: result.FuelUsed, ReturnHash: types.Hash(codec.DomainHash("zephyr/contract-return/v2", result.ReturnData)), EventRoot: merkle.Root(eventLeaves)}
	txID := t.ID()
	created = append(created, object.Object{ID: types.ObjectIDForShard(txID, contractReceiptIndex, t.ShardID), Version: 1, Owner: t.Sender, Kind: object.KindSystem, Data: receipt.MarshalBinary()})
	return Result{Consumed: consumed, Created: created, TxID: txID}, nil
}

func (e Engine) feeOnlyOutputs(t tx.Transaction) ([]object.Object, []types.ObjectID, error) {
	return e.feeOnlyOutputsExcluding(t, types.ObjectID{}, nil)
}

func (e Engine) feeOnlyOutputsExcluding(t tx.Transaction, contractObject types.ObjectID, accesses []contracts.Access) ([]object.Object, []types.ObjectID, error) {
	nonCoin := make(map[types.ObjectID]bool, len(accesses)+1)
	if !types.IsZero32([32]byte(contractObject)) {
		nonCoin[contractObject] = true
	}
	for _, access := range accesses {
		nonCoin[access.ObjectID] = true
	}
	var nativeIn uint64
	consumed := make([]types.ObjectID, 0)
	for _, witness := range t.Witnesses {
		if nonCoin[witness.Object.ID] {
			continue
		}
		if witness.Object.Kind != object.KindCoin || witness.Object.Owner != t.Sender {
			return nil, nil, ErrOwnership
		}
		coin, err := object.ParseCoin(witness.Object.Data)
		if err != nil || coin.Token != e.NativeToken {
			return nil, nil, ErrConservation
		}
		if math.MaxUint64-nativeIn < coin.Amount {
			return nil, nil, ErrOverflow
		}
		nativeIn += coin.Amount
		consumed = append(consumed, witness.Object.ID)
	}
	var nativeOut uint64
	created := make([]object.Object, 0, len(t.Outputs))
	router := sharding.Router{ShardCount: e.ShardCount}
	for i, spec := range t.Outputs {
		if spec.Kind != object.KindCoin {
			return nil, nil, ErrConservation
		}
		destination, err := router.ShardForAccount(spec.Owner)
		if err != nil || destination != t.ShardID {
			return nil, nil, ErrShard
		}
		coin, err := object.ParseCoin(spec.Data)
		if err != nil || coin.Token != e.NativeToken {
			return nil, nil, ErrConservation
		}
		if math.MaxUint64-nativeOut < coin.Amount {
			return nil, nil, ErrOverflow
		}
		nativeOut += coin.Amount
		stamped, err := stampCoinOutput(spec, e.Height)
		if err != nil {
			return nil, nil, err
		}
		created = append(created, object.Object{ID: types.ObjectIDForShard(t.ID(), uint32(i), t.ShardID), Version: 1, Owner: stamped.Owner, Kind: stamped.Kind, Data: append([]byte(nil), stamped.Data...)})
	}
	if math.MaxUint64-nativeOut < t.Fee || nativeIn != nativeOut+t.Fee {
		return nil, nil, ErrConservation
	}
	return created, consumed, nil
}
