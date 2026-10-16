package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
)

type longRunConfigOutput struct {
	Epochs           uint64 `json:"epochs"`
	EpochsPerYear    uint64 `json:"epochs_per_year"`
	DirectionBandBps uint32 `json:"direction_band_bps"`
	CheckpointEvery  uint64 `json:"checkpoint_every"`
}

type longRunOutput struct {
	Version     uint16                                   `json:"version"`
	Shadow      bool                                     `json:"shadow"`
	Scenario    string                                   `json:"scenario"`
	Disclaimer  string                                   `json:"disclaimer"`
	Config      longRunConfigOutput                      `json:"config"`
	Summary     economics.LongHorizonSimulationSummary   `json:"summary"`
	Checkpoints []economics.LongHorizonEpochSnapshot     `json:"checkpoints"`
	Sensitivity []economics.LongHorizonSensitivityResult `json:"sensitivity"`
	Epochs      []economics.LongHorizonEpochSnapshot     `json:"epochs,omitempty"`
}

func main() {
	epochs := flag.Uint64("epochs", 3*365, "number of shadow epochs to simulate")
	checkpointEvery := flag.Uint64("checkpoint-every", 365, "include one compact output checkpoint every N epochs")
	includeEpochs := flag.Bool("include-epochs", false, "include the full per-epoch timeline in JSON output")
	outputPath := flag.String("output", "-", "JSON output path, or - for stdout")
	flag.Parse()

	if *epochs == 0 || *checkpointEvery == 0 {
		fatal(fmt.Errorf("epochs and checkpoint-every must be positive"))
	}
	config := economics.DefaultLongHorizonSimulationConfig()
	inputs, err := economics.BuildAdversarialLongHorizonInputs(*epochs)
	if err != nil {
		fatal(err)
	}
	result, err := economics.RunLongHorizonShadowSimulation(config, inputs)
	if err != nil {
		fatal(err)
	}
	sensitivity, err := economics.RunLongHorizonSensitivityMatrix(*epochs, economics.DefaultLongHorizonSensitivityCases())
	if err != nil {
		fatal(err)
	}

	output := longRunOutput{
		Version:    1,
		Shadow:     result.Shadow,
		Scenario:   "adversarial-stress-mix-v1",
		Disclaimer: "shadow-only simulation; no minting, levy, reward routing, fee split, supply mutation, or activation is performed",
		Config: longRunConfigOutput{
			Epochs:           *epochs,
			EpochsPerYear:    config.EpochsPerYear,
			DirectionBandBps: config.DirectionBandBps,
			CheckpointEvery:  *checkpointEvery,
		},
		Summary:     result.Summary,
		Checkpoints: selectCheckpoints(result.Epochs, *checkpointEvery),
		Sensitivity: sensitivity,
	}
	if *includeEpochs {
		output.Epochs = result.Epochs
	}

	writer := io.Writer(os.Stdout)
	var file *os.File
	if *outputPath != "-" {
		file, err = os.Create(*outputPath)
		if err != nil {
			fatal(err)
		}
		defer file.Close()
		writer = file
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fatal(err)
	}
}

func selectCheckpoints(
	epochs []economics.LongHorizonEpochSnapshot,
	every uint64,
) []economics.LongHorizonEpochSnapshot {
	if len(epochs) == 0 || every == 0 {
		return nil
	}
	capacity := 2
	if every <= uint64(len(epochs)) {
		capacity += int(uint64(len(epochs)) / every)
	}
	out := make([]economics.LongHorizonEpochSnapshot, 0, capacity)
	appendIndex := func(index int) {
		if index < 0 || index >= len(epochs) {
			return
		}
		if len(out) > 0 && out[len(out)-1].Epoch == epochs[index].Epoch {
			return
		}
		out = append(out, epochs[index])
	}
	appendIndex(0)
	for epoch := every; epoch <= uint64(len(epochs)); epoch += every {
		appendIndex(int(epoch - 1))
		if epoch > ^uint64(0)-every {
			break
		}
	}
	appendIndex(len(epochs) - 1)
	return out
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "zephyr-econ-longrun: %v\n", err)
	os.Exit(1)
}
