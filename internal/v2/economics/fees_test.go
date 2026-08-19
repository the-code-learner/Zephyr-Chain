package economics

import (
	"math"
	"testing"
)

func TestSplitFeeConservesEveryAtomicUnit(t *testing.T) {
	allocation, err := SplitFee(101, FeePolicy{BurnBps: 4_000, ValidatorBps: 5_000, ReserveBps: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Burn != 41 || allocation.Validators != 50 || allocation.Reserve != 10 {
		t.Fatalf("unexpected deterministic split: %#v", allocation)
	}
	if allocation.Burn+allocation.Validators+allocation.Reserve != allocation.Total {
		t.Fatal("fee split does not conserve value")
	}
}

func TestCompatibilityFeePolicyPreservesFullBurn(t *testing.T) {
	allocation, err := SplitFee(12345, CompatibilityFeePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Burn != 12345 || allocation.Validators != 0 || allocation.Reserve != 0 {
		t.Fatalf("compatibility policy changed existing semantics: %#v", allocation)
	}
}

func TestQuoteResourceFee(t *testing.T) {
	usage := ResourceUsage{
		BaseTransactions:       1,
		SignatureOps:           2,
		WitnessBytes:           100,
		StateReads:             3,
		StateWrites:            2,
		ContractFuel:           50,
		DataAvailabilityBytes: 20,
		CrossShardReceipts:     1,
	}
	prices := ResourcePrices{
		BaseTransaction:      10,
		SignatureOp:          2,
		WitnessByte:          1,
		StateRead:            3,
		StateWrite:           5,
		ContractFuel:         1,
		DataAvailabilityByte: 2,
		CrossShardReceipt:    7,
	}
	fee, err := QuoteResourceFee(usage, prices)
	if err != nil {
		t.Fatal(err)
	}
	if fee != 230 {
		t.Fatalf("unexpected resource fee %d", fee)
	}
}

func TestQuoteResourceFeeRejectsOverflow(t *testing.T) {
	_, err := QuoteResourceFee(ResourceUsage{BaseTransactions: math.MaxUint64}, ResourcePrices{BaseTransaction: 2})
	if err != ErrResourceFee {
		t.Fatalf("expected overflow rejection, got %v", err)
	}
}

func TestNextBaseFeeMovesTowardCongestionAndBounds(t *testing.T) {
	policy := BaseFeePolicy{TargetUsage: 100, AdjustmentDenominator: 8, MinBaseFee: 10, MaxBaseFee: 10_000}
	high, err := NextBaseFee(1_000, 200, policy)
	if err != nil {
		t.Fatal(err)
	}
	if high != 1_125 {
		t.Fatalf("unexpected congestion increase %d", high)
	}
	low, err := NextBaseFee(1_000, 0, policy)
	if err != nil {
		t.Fatal(err)
	}
	if low != 875 {
		t.Fatalf("unexpected utilization decrease %d", low)
	}
	floor, err := NextBaseFee(10, 0, policy)
	if err != nil || floor != 10 {
		t.Fatalf("minimum fee must be preserved: %d %v", floor, err)
	}
}
