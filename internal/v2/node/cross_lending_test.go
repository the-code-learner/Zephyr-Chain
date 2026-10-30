package node

import (
	"crypto/ecdsa"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/lending"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCrossShardLendingFinalizesThroughReceiptProofs(t *testing.T) {
	const shardCount = uint32(2)
	network := types.NetworkID(types.HashBytes("network", []byte("cross-lending")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	lenderKey, lender := accountOnShard(t, 0, shardCount)
	borrowerKey, borrower := accountOnShard(t, 1, shardCount)
	shard0 := worldstate.NewMemory()
	shard1 := worldstate.NewMemory()

	lenderCoin := seedNodeCoin(t, shard0, lender, native, 200, 0, "cross-lender")
	borrowerCoin := seedNodeCoin(t, shard1, borrower, native, 200, 0, "cross-borrower")

	validatorKey, validators := singleValidatorSet(t, network)
	validatorRoot, err := validators.Root()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: shard0, 1: shard1}, 2)
	if err != nil {
		t.Fatal(err)
	}

	terms := lending.Terms{
		Lender: lender, Borrower: borrower,
		Principal: 50, Collateral: 30, TermRateBps: -1_000, MaturityHeight: 10,
	}
	termsRaw, err := terms.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	lenderChangeSpec, _ := object.NewCoinOutput(lender, native, 149)
	offerTx := signedNodeTx(t, shard0, lenderKey, network, 0, []types.ObjectID{lenderCoin}, []object.OutputSpec{lenderChangeSpec}, tx.OpCrossLoanOffer, termsRaw, 1)
	candidate1, finalized1, cert1 := finalizeNodeBlock(t, runtime, validators, validatorKey, 1, map[uint32]ShardBatch{0: {Transactions: []tx.Transaction{offerTx}}})
	if len(candidate1.Receipts[0]) != 1 || candidate1.Receipts[0][0].Output.Kind != object.KindCrossLoanOffer {
		t.Fatalf("unexpected offer receipts: %+v", candidate1.Receipts[0])
	}
	offerImport := buildReceiptImport(t, candidate1, finalized1, cert1, validators, 0, 0)

	_, _, _ = finalizeNodeBlock(t, runtime, validators, validatorKey, 2, map[uint32]ShardBatch{1: {Imports: []ReceiptImport{offerImport}}})
	offerObject, err := candidate1.Receipts[0][0].DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shard1.GetObject(offerObject.ID); !ok {
		t.Fatal("cross-shard offer was not materialized")
	}

	borrowerChangeSpec, _ := object.NewCoinOutput(borrower, native, 169)
	offerRef, _ := (lending.Ref{ObjectID: offerObject.ID}).MarshalBinary()
	acceptTx := signedNodeTx(t, shard1, borrowerKey, network, 1,
		[]types.ObjectID{offerObject.ID, borrowerCoin}, []object.OutputSpec{borrowerChangeSpec},
		tx.OpCrossLoanAccept, offerRef, 3)
	candidate3, finalized3, cert3 := finalizeNodeBlock(t, runtime, validators, validatorKey, 3, map[uint32]ShardBatch{1: {Transactions: []tx.Transaction{acceptTx}}})
	if len(candidate3.Receipts[1]) != 1 || candidate3.Receipts[1][0].Output.Kind != object.KindCrossLoanAcceptance {
		t.Fatalf("unexpected acceptance receipts: %+v", candidate3.Receipts[1])
	}
	acceptanceImport := buildReceiptImport(t, candidate3, finalized3, cert3, validators, 1, 0)
	_, _, _ = finalizeNodeBlock(t, runtime, validators, validatorKey, 4, map[uint32]ShardBatch{0: {Imports: []ReceiptImport{acceptanceImport}}})

	acceptanceObject, err := candidate3.Receipts[1][0].DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shard0.GetObject(acceptanceObject.ID); !ok {
		t.Fatal("cross-shard acceptance was not materialized")
	}
	activationID := types.ObjectIDForShard(acceptTx.ID(), lending.CrossActivationObjectIndex, 1)
	principalID := types.ObjectIDForShard(acceptTx.ID(), lending.CrossPrincipalObjectIndex, 1)
	if _, ok := shard1.GetObject(activationID); !ok {
		t.Fatal("borrower activation mirror missing")
	}
	principalObject, ok := shard1.GetObject(principalID)
	if !ok {
		t.Fatal("borrower principal missing")
	}
	principalCoin, err := object.ParseCoin(principalObject.Data)
	if err != nil || principalCoin.Amount != 50 || principalObject.Owner != borrower {
		t.Fatalf("invalid borrower principal: %+v %v", principalObject, err)
	}

	repayChangeSpec, _ := object.NewCoinOutput(borrower, native, 4)
	activationRef, _ := (lending.Ref{ObjectID: activationID}).MarshalBinary()
	repayTx := signedNodeTx(t, shard1, borrowerKey, network, 1,
		[]types.ObjectID{activationID, principalID}, []object.OutputSpec{repayChangeSpec},
		tx.OpCrossLoanRepay, activationRef, 5)
	candidate5, finalized5, cert5 := finalizeNodeBlock(t, runtime, validators, validatorKey, 5, map[uint32]ShardBatch{1: {Transactions: []tx.Transaction{repayTx}}})
	if len(candidate5.Receipts[1]) != 1 || candidate5.Receipts[1][0].Output.Kind != object.KindCrossLoanRepayment {
		t.Fatalf("unexpected repayment receipts: %+v", candidate5.Receipts[1])
	}
	repaymentImport := buildReceiptImport(t, candidate5, finalized5, cert5, validators, 1, 0)
	_, _, _ = finalizeNodeBlock(t, runtime, validators, validatorKey, 6, map[uint32]ShardBatch{0: {Imports: []ReceiptImport{repaymentImport}}})
	repaymentObject, err := candidate5.Receipts[1][0].DestinationObject()
	if err != nil {
		t.Fatal(err)
	}

	lenderChangeID := types.ObjectIDForShard(offerTx.ID(), 0, 0)
	settleChangeSpec, _ := object.NewCoinOutput(lender, native, 148)
	settleTx := signedNodeTx(t, shard0, lenderKey, network, 0,
		[]types.ObjectID{acceptanceObject.ID, repaymentObject.ID, lenderChangeID}, []object.OutputSpec{settleChangeSpec},
		tx.OpCrossLoanSettle, nil, 7)
	candidate7, finalized7, cert7 := finalizeNodeBlock(t, runtime, validators, validatorKey, 7, map[uint32]ShardBatch{0: {Transactions: []tx.Transaction{settleTx}}})
	if len(candidate7.Receipts[0]) != 1 || candidate7.Receipts[0][0].Output.Kind != object.KindCoin {
		t.Fatalf("unexpected settlement receipts: %+v", candidate7.Receipts[0])
	}
	collateralImport := buildReceiptImport(t, candidate7, finalized7, cert7, validators, 0, 0)
	_, _, _ = finalizeNodeBlock(t, runtime, validators, validatorKey, 8, map[uint32]ShardBatch{1: {Imports: []ReceiptImport{collateralImport}}})

	lenderPayoutID := types.ObjectIDForShard(settleTx.ID(), lending.PayoutObjectIndex, 0)
	lenderPayout, ok := shard0.GetObject(lenderPayoutID)
	if !ok {
		t.Fatal("lender repayment payout missing")
	}
	lenderPayoutCoin, err := object.ParseCoin(lenderPayout.Data)
	if err != nil || lenderPayoutCoin.Amount != 45 || lenderPayout.Owner != lender {
		t.Fatalf("invalid lender repayment: %+v %v", lenderPayout, err)
	}
	collateralObject, err := candidate7.Receipts[0][0].DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	collateralReturn, ok := shard1.GetObject(collateralObject.ID)
	if !ok {
		t.Fatal("borrower collateral return missing")
	}
	collateralCoin, err := object.ParseCoin(collateralReturn.Data)
	if err != nil || collateralCoin.Amount != 30 || collateralReturn.Owner != borrower {
		t.Fatalf("invalid collateral return: %+v %v", collateralReturn, err)
	}
	if _, ok := shard0.GetObject(acceptanceObject.ID); ok {
		t.Fatal("settled acceptance collateral remained spendable")
	}
	if _, ok := shard0.GetObject(repaymentObject.ID); ok {
		t.Fatal("settled repayment remained spendable")
	}
	if _, ok := shard1.GetObject(activationID); ok {
		t.Fatal("repaid activation remained spendable")
	}

	if _, err := runtime.BuildCandidate(9, map[uint32]ShardBatch{1: {Imports: []ReceiptImport{collateralImport}}}); err != sharding.ErrReceiptReplay {
		t.Fatalf("expected collateral receipt replay rejection, got %v", err)
	}
}

func seedNodeCoin(t *testing.T, store *worldstate.Memory, owner types.AccountID, token types.TokenID, amount, height uint64, label string) types.ObjectID {
	t.Helper()
	spec, err := object.NewCoinOutputAtHeight(owner, token, amount, height)
	if err != nil {
		t.Fatal(err)
	}
	id := types.ObjectIDForShard(types.HashBytes("node-cross-lending-seed", []byte(label)), 0, types.AccountShard(owner, 2))
	if _, err := store.Apply(nil, []object.Object{{ID: id, Version: 1, Owner: owner, Kind: spec.Kind, Data: spec.Data}}); err != nil {
		t.Fatal(err)
	}
	return id
}

func signedNodeTx(
	t *testing.T,
	store *worldstate.Memory,
	key *ecdsa.PrivateKey,
	network types.NetworkID,
	shard uint32,
	inputIDs []types.ObjectID,
	outputs []object.OutputSpec,
	op uint16,
	payload []byte,
	salt byte,
) tx.Transaction {
	t.Helper()
	inputs := make([]tx.InputRef, 0, len(inputIDs))
	witnesses := make([]tx.Witness, 0, len(inputIDs))
	for _, id := range inputIDs {
		item, proof, ok := store.Proof(id)
		if !ok {
			t.Fatalf("missing transaction input %s", id)
		}
		inputs = append(inputs, tx.InputRef{ObjectID: id, Version: item.Version, ObjectHash: item.Hash()})
		witnesses = append(witnesses, tx.Witness{Object: item, Proof: proof})
	}
	transaction := tx.Transaction{
		Version: tx.Version, Network: network, ShardID: shard, StateRoot: store.Root(),
		Inputs: inputs, Outputs: outputs, Operations: []tx.Operation{{Kind: op, Payload: payload}},
		Fee: 1, ValidUntilHeight: 100, Witnesses: witnesses,
	}
	transaction.Salt[0] = salt
	if err := transaction.Sign(key); err != nil {
		t.Fatal(err)
	}
	return transaction
}

func finalizeNodeBlock(
	t *testing.T,
	runtime *Runtime,
	validators v2consensus.ValidatorSet,
	validatorKey *ecdsa.PrivateKey,
	height uint64,
	batches map[uint32]ShardBatch,
) (Candidate, sharding.GlobalHeader, v2consensus.Certificate) {
	t.Helper()
	candidate, err := runtime.BuildCandidate(height, batches)
	if err != nil {
		t.Fatal(err)
	}
	_, certificate := certifyCandidate(t, validators, validatorKey, candidate)
	finalized, err := runtime.Commit(candidate, certificate, validators)
	if err != nil {
		t.Fatal(err)
	}
	return candidate, finalized, certificate
}

func buildReceiptImport(
	t *testing.T,
	candidate Candidate,
	finalized sharding.GlobalHeader,
	certificate v2consensus.Certificate,
	validators v2consensus.ValidatorSet,
	sourceShard uint32,
	receiptIndex int,
) ReceiptImport {
	t.Helper()
	receipts := candidate.Receipts[sourceShard]
	if receiptIndex < 0 || receiptIndex >= len(receipts) {
		t.Fatalf("receipt index %d out of range", receiptIndex)
	}
	receipt := receipts[receiptIndex]
	commitment, commitmentProof, err := sharding.CommitmentProof(candidate.Commitments, sourceShard)
	if err != nil {
		t.Fatal(err)
	}
	receiptProof, err := (sharding.ReceiptBatch{Receipts: receipts}).Proof(receipt)
	if err != nil {
		t.Fatal(err)
	}
	return ReceiptImport{
		Header: finalized, Certificate: certificate, Validators: validators,
		Commitment: commitment, CommitmentProof: commitmentProof,
		Receipt: receipt, ReceiptProof: receiptProof,
	}
}
