package economics

import (
	"bytes"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestShadowEpochEngineCheckpointRoundTrip(t *testing.T) {
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
	if err := engine.Accept(preview); err != nil {
		t.Fatal(err)
	}
	raw, err := engine.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreShadowEpochEngine(raw, network, config)
	if err != nil {
		t.Fatal(err)
	}
	rawAgain, err := restored.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, rawAgain) {
		t.Fatal("shadow epoch checkpoint is not canonical across restore")
	}
	state, ok := restored.PreviousState()
	if !ok || state != preview.State {
		t.Fatalf("restored engine lost prior monetary state: %#v", state)
	}
	if restored.PriorComputeIndex() != preview.ComputeIndex {
		t.Fatal("restored engine lost prior compute index")
	}
}

func TestShadowEpochEngineCheckpointRejectsPolicyChange(t *testing.T) {
	config := testShadowEpochConfig()
	network := types.NetworkID{1}
	engine, err := NewShadowEpochEngine(network, config)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := engine.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	changed := config
	changed.Monetary.TargetInflationBps++
	if _, err := RestoreShadowEpochEngine(raw, network, changed); err == nil {
		t.Fatal("checkpoint restored under a different monetary policy")
	}
	changed = config
	changed.ComputeScarcity.MinSupplyUnits++
	if _, err := RestoreShadowEpochEngine(raw, network, changed); err == nil {
		t.Fatal("checkpoint restored under a different ZCSI policy")
	}
	changed = config
	changed.ComputeFeedback.Mode = ComputeFeedbackMonetaryBand
	if _, err := RestoreShadowEpochEngine(raw, network, changed); err == nil {
		t.Fatal("checkpoint restored under a different compute-feedback mode")
	}
}

func TestShadowEpochEngineCheckpointRejectsWrongNetwork(t *testing.T) {
	config := testShadowEpochConfig()
	engine, err := NewShadowEpochEngine(types.NetworkID{1}, config)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := engine.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreShadowEpochEngine(raw, types.NetworkID{2}, config); err == nil {
		t.Fatal("checkpoint restored on a different network")
	}
}
