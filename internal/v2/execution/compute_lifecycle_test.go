package execution

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCrossShardComputeEscrowAssignmentResultAndSettlement(t *testing.T) {
	const shardCount = uint32(2)
	networkID := types.NetworkID(types.HashBytes("network", []byte("compute-lifecycle")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	ownerKey, owner := computeAccountOnShard(t, 1, shardCount)
	providerKey1, provider1 := computeAccountOnShard(t, 0, shardCount)
	providerKey2, provider2 := computeAccountOnShard(t, 0, shardCount)
	ownerStore := worldstate.NewMemory()
	providerStores := []*worldstate.Memory{worldstate.NewMemory(), worldstate.NewMemory()}

	ownerCoin := seedComputeCoin(t, ownerStore, owner, native, 300, "owner")
	providerCoins := []types.ObjectID{
		seedComputeCoin(t, providerStores[0], provider1, native, 200, "provider-1"),
		seedComputeCoin(t, providerStores[1], provider2, native, 200, "provider-2"),
	}
	providers := []types.AccountID{provider1, provider2}
	providerKeys := []*ecdsa.PrivateKey{providerKey1, providerKey2}
	resources := compute.Resources{CPUCores: 8, MemoryMiB: 16384, GPUCount: 1, GPUMemoryMiB: 12288, StorageMiB: 1024, BandwidthMbps: 100, Capabilities: []string{"render"}}
	job := compute.Job{Owner: owner, WorkloadHash: types.HashBytes("workload", []byte("scene")), InputRoot: types.HashBytes("input", []byte("scene-data")), Resources: resources, MaxPrice: 100, CollateralRequired: 20, Verification: compute.VerificationReplicated, DeadlineHeight: 100, Replicas: 2}
	jobPayload, err := job.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	jobTx := computeSignedTx(t, ownerStore, ownerKey, networkID, native, 1, []types.ObjectID{ownerCoin}, 199, tx.OpComputeJob, jobPayload, 1)
	jobResult, err := (Engine{Network: networkID, NativeToken: native, ShardCount: shardCount, Height: 1}).Execute(jobTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ownerStore.Apply(jobResult.Consumed, jobResult.Created); err != nil {
		t.Fatal(err)
	}
	jobObject := objectOfKind(t, jobResult.Created, object.KindComputeJob)
	ownerCoin = objectOfKind(t, jobResult.Created, object.KindCoin).ID
	record, err := compute.ParseOnChainJob(jobObject.Data)
	if err != nil {
		t.Fatal(err)
	}

	assignmentObjects := make([]object.Object, 0, 2)
	for i := range providers {
		offer := compute.Offer{Provider: providers[i], Resources: resources, PricePerUnit: 10, Collateral: 20, Verification: []compute.VerificationMode{compute.VerificationReplicated}, ValidUntilHeight: 80}
		offerPayload, _ := offer.MarshalBinary()
		offerTx := computeSignedTx(t, providerStores[i], providerKeys[i], networkID, native, 0, []types.ObjectID{providerCoins[i]}, 179, tx.OpComputeOffer, offerPayload, byte(10+i))
		offerResult, err := (Engine{Network: networkID, NativeToken: native, ShardCount: shardCount, Height: 2}).Execute(offerTx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := providerStores[i].Apply(offerResult.Consumed, offerResult.Created); err != nil {
			t.Fatal(err)
		}
		offerObject := objectOfKind(t, offerResult.Created, object.KindComputeOffer)
		providerCoins[i] = objectOfKind(t, offerResult.Created, object.KindCoin).ID
		message := compute.AssignmentMessage{JobID: record.ID, JobOwner: owner, JobShard: 1, OfferID: types.Hash(offerObject.ID), Offer: offer, Job: job}
		messageRaw, _ := message.MarshalBinary()
		acceptTx := computeSignedTx(t, providerStores[i], providerKeys[i], networkID, native, 0, []types.ObjectID{offerObject.ID, providerCoins[i]}, 178, tx.OpComputeAccept, messageRaw, byte(20+i))
		acceptResult, err := (Engine{Network: networkID, NativeToken: native, ShardCount: shardCount, Height: 3}).Execute(acceptTx)
		if err != nil {
			t.Fatal(err)
		}
		if len(acceptResult.Outbound) != 1 || acceptResult.Outbound[0].DestinationShard != 1 || acceptResult.Outbound[0].Output.Kind != object.KindComputeAssignment {
			t.Fatalf("assignment not routed cross-shard: %+v", acceptResult.Outbound)
		}
		if _, err := providerStores[i].Apply(acceptResult.Consumed, acceptResult.Created); err != nil {
			t.Fatal(err)
		}
		providerCoins[i] = objectOfKind(t, acceptResult.Created, object.KindCoin).ID
		outbound := acceptResult.Outbound[0]
		assignmentObjects = append(assignmentObjects, object.Object{ID: types.ObjectIDForShard(acceptResult.TxID, outbound.OutputIndex, 1), Version: 1, Owner: outbound.Output.Owner, Kind: outbound.Output.Kind, Data: outbound.Output.Data})
	}
	if _, err := ownerStore.Apply(nil, assignmentObjects); err != nil {
		t.Fatal(err)
	}

	for i, assignment := range assignmentObjects {
		refRaw, _ := (compute.IngestRef{JobObject: jobObject.ID, MessageObject: assignment.ID}).MarshalBinary()
		ingestTx := computeSignedTx(t, ownerStore, ownerKey, networkID, native, 1, []types.ObjectID{jobObject.ID, assignment.ID, ownerCoin}, uint64(198-i), tx.OpComputeIngestAssignment, refRaw, byte(30+i))
		ingestResult, err := (Engine{Network: networkID, NativeToken: native, ShardCount: shardCount, Height: uint64(4 + i)}).Execute(ingestTx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ownerStore.Apply(ingestResult.Consumed, ingestResult.Created); err != nil {
			t.Fatal(err)
		}
		jobObject = objectOfKind(t, ingestResult.Created, object.KindComputeJob)
		ownerCoin = objectOfKind(t, ingestResult.Created, object.KindCoin).ID
	}
	record, err = compute.ParseOnChainJob(jobObject.Data)
	if err != nil || record.Status != compute.JobAssigned || len(record.Assignments) != 2 {
		t.Fatalf("job not fully assigned: %+v %v", record, err)
	}

	resultRoot := types.HashBytes("result", []byte("rendered-scene"))
	resultObjects := make([]object.Object, 0, 2)
	for i := range providers {
		message := compute.ResultMessage{JobID: record.ID, JobOwner: owner, JobShard: 1, Result: compute.Result{JobID: record.ID, Provider: providers[i], ResultRoot: resultRoot, CompletedHeight: uint64(10 + i)}}
		messageRaw, _ := message.MarshalBinary()
		resultTx := computeSignedTx(t, providerStores[i], providerKeys[i], networkID, native, 0, []types.ObjectID{providerCoins[i]}, 177, tx.OpComputeResult, messageRaw, byte(40+i))
		result, err := (Engine{Network: networkID, NativeToken: native, ShardCount: shardCount, Height: uint64(10 + i)}).Execute(resultTx)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Outbound) != 1 || result.Outbound[0].Output.Kind != object.KindComputeResult {
			t.Fatal("compute result not routed to job shard")
		}
		if _, err := providerStores[i].Apply(result.Consumed, result.Created); err != nil {
			t.Fatal(err)
		}
		providerCoins[i] = objectOfKind(t, result.Created, object.KindCoin).ID
		outbound := result.Outbound[0]
		resultObjects = append(resultObjects, object.Object{ID: types.ObjectIDForShard(result.TxID, outbound.OutputIndex, 1), Version: 1, Owner: outbound.Output.Owner, Kind: outbound.Output.Kind, Data: outbound.Output.Data})
	}
	if _, err := ownerStore.Apply(nil, resultObjects); err != nil {
		t.Fatal(err)
	}
	for i, resultObject := range resultObjects {
		refRaw, _ := (compute.IngestRef{JobObject: jobObject.ID, MessageObject: resultObject.ID}).MarshalBinary()
		ingestTx := computeSignedTx(t, ownerStore, ownerKey, networkID, native, 1, []types.ObjectID{jobObject.ID, resultObject.ID, ownerCoin}, uint64(196-i), tx.OpComputeIngestResult, refRaw, byte(50+i))
		ingestResult, err := (Engine{Network: networkID, NativeToken: native, ShardCount: shardCount, Height: uint64(20 + i)}).Execute(ingestTx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ownerStore.Apply(ingestResult.Consumed, ingestResult.Created); err != nil {
			t.Fatal(err)
		}
		jobObject = objectOfKind(t, ingestResult.Created, object.KindComputeJob)
		ownerCoin = objectOfKind(t, ingestResult.Created, object.KindCoin).ID
	}
	record, err = compute.ParseOnChainJob(jobObject.Data)
	if err != nil || record.Status != compute.JobAwaitingVerification || len(record.Results) != 2 {
		t.Fatalf("results not ready for verification: %+v %v", record, err)
	}
	jobRef, _ := (compute.JobRef{JobObject: jobObject.ID}).MarshalBinary()
	finalTx := computeSignedTx(t, ownerStore, ownerKey, networkID, native, 1, []types.ObjectID{jobObject.ID, ownerCoin}, 194, tx.OpComputeFinalize, jobRef, 60)
	finalResult, err := (Engine{Network: networkID, NativeToken: native, ShardCount: shardCount, Height: 30}).Execute(finalTx)
	if err != nil {
		t.Fatal(err)
	}
	if len(finalResult.Outbound) != 2 {
		t.Fatalf("expected two provider payouts, got %d", len(finalResult.Outbound))
	}
	var ownerRefund uint64
	for _, item := range finalResult.Created {
		if item.Kind == object.KindCoin && item.Owner == owner {
			coin, _ := object.ParseCoin(item.Data)
			ownerRefund += coin.Amount
		}
	}
	// 194 fee-change + 80 unused escrow refund.
	if ownerRefund != 274 {
		t.Fatalf("unexpected owner value after settlement: %d", ownerRefund)
	}
	for _, outbound := range finalResult.Outbound {
		coin, err := object.ParseCoin(outbound.Output.Data)
		if err != nil || coin.Amount != 30 {
			t.Fatalf("provider payment+collateral must equal 30: %+v %v", outbound, err)
		}
	}
}

func computeAccountOnShard(t *testing.T, shard, count uint32) (*ecdsa.PrivateKey, types.AccountID) {
	t.Helper()
	for i := 0; i < 10000; i++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		account := types.AccountIDFromPublicKey(pub)
		if types.AccountShard(account, count) == shard {
			return key, account
		}
	}
	t.Fatal("failed to find account on requested shard")
	return nil, types.AccountID{}
}

func seedComputeCoin(t *testing.T, store *worldstate.Memory, owner types.AccountID, token types.TokenID, amount uint64, label string) types.ObjectID {
	t.Helper()
	spec, err := object.NewCoinOutput(owner, token, amount)
	if err != nil {
		t.Fatal(err)
	}
	id := types.ObjectIDForShard(types.HashBytes("seed", []byte(label)), 0, types.AccountShard(owner, 2))
	if _, err := store.Apply(nil, []object.Object{{ID: id, Version: 1, Owner: owner, Kind: spec.Kind, Data: spec.Data}}); err != nil {
		t.Fatal(err)
	}
	return id
}

func computeSignedTx(t *testing.T, store *worldstate.Memory, key *ecdsa.PrivateKey, networkID types.NetworkID, native types.TokenID, shard uint32, inputIDs []types.ObjectID, change uint64, op uint16, payload []byte, salt byte) tx.Transaction {
	t.Helper()
	inputs := make([]tx.InputRef, 0, len(inputIDs))
	witnesses := make([]tx.Witness, 0, len(inputIDs))
	for _, id := range inputIDs {
		item, proof, ok := store.Proof(id)
		if !ok {
			t.Fatalf("missing input %s", id)
		}
		h := item.Hash()
		inputs = append(inputs, tx.InputRef{ObjectID: id, Version: item.Version, ObjectHash: h})
		witnesses = append(witnesses, tx.Witness{Object: item, Proof: proof})
	}
	pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	owner := types.AccountIDFromPublicKey(pub)
	outputs := []object.OutputSpec{}
	if change > 0 {
		spec, err := object.NewCoinOutput(owner, native, change)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, spec)
	}
	transaction := tx.Transaction{Version: tx.Version, Network: networkID, ShardID: shard, StateRoot: store.Root(), Inputs: inputs, Outputs: outputs, Operations: []tx.Operation{{Kind: op, Payload: payload}}, Fee: 1, ValidUntilHeight: 200, Witnesses: witnesses}
	transaction.Salt[0] = salt
	if err := transaction.Sign(key); err != nil {
		t.Fatal(err)
	}
	return transaction
}

func objectOfKind(t *testing.T, objects []object.Object, kind object.Kind) object.Object {
	t.Helper()
	for _, item := range objects {
		if item.Kind == kind {
			return item
		}
	}
	t.Fatalf("object kind %d not found", kind)
	return object.Object{}
}
