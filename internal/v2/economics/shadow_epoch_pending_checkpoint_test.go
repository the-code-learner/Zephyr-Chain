package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestPendingShadowEpochCheckpointRecovery(t *testing.T) {
	config := testShadowEpochConfig()
	network := types.NetworkID{1}
	engine, err := NewShadowEpochEngine(network, config)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := engine.PreviewCloseEpoch(
		[]ShardEpochMetrics{testEpochMetric(1, 0, 1_000, 500, 500)},
		[]compute.VerifiedWork{testVerifiedCPUWork(1, 200)},
		MonetaryBalanceSnapshot{TotalSupply: 1_000, StakedSupply: 400, ProtocolReserve: 100, BaseFee: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := preview.PendingCheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreShadowEpochPreview(raw, engine)
	if err != nil {
		t.Fatal(err)
	}
	if restored.State != preview.State || restored.Aggregate != preview.Aggregate || restored.ComputeIndex != preview.ComputeIndex {
		t.Fatal("pending checkpoint changed committed economics")
	}
	if len(restored.Consumed) != 0 || len(restored.Created) != 1 || restored.Created[0].ID != MonetaryStateObjectID(network) {
		t.Fatal("pending checkpoint did not rebuild the first monetary object delta")
	}
}

func TestPendingShadowEpochCheckpointRequiresAcceptedHistory(t *testing.T) {
	config := testShadowEpochConfig()
	network := types.NetworkID{1}
	engine, err := NewShadowEpochEngine(network, config)
	if err != nil {
		t.Fatal(err)
	}
	first, err := engine.PreviewCloseEpoch(
		[]ShardEpochMetrics{testEpochMetric(1, 0, 1_000, 500, 500)},
		[]compute.VerifiedWork{testVerifiedCPUWork(1, 200)},
		MonetaryBalanceSnapshot{TotalSupply: 1_000, StakedSupply: 400, ProtocolReserve: 100, BaseFee: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Accept(first); err != nil {
		t.Fatal(err)
	}
	second, err := engine.PreviewCloseEpoch(
		[]ShardEpochMetrics{testEpochMetric(2, 500, 0, 500, 0)},
		[]compute.VerifiedWork{testVerifiedCPUWork(3, 300)},
		MonetaryBalanceSnapshot{TotalSupply: 1_000, StakedSupply: 400, ProtocolReserve: 100, BaseFee: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := second.PendingCheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewShadowEpochEngine(network, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreShadowEpochPreview(raw, fresh); err == nil {
		t.Fatal("pending epoch restored without the accepted predecessor")
	}
	restored, err := RestoreShadowEpochPreview(raw, engine)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Consumed) != 1 || len(restored.Created) != 1 {
		t.Fatal("pending replacement delta was not rebuilt")
	}
}
