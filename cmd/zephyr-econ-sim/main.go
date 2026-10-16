package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
)

type simulationInput struct {
	Epoch          uint64                           `json:"epoch,omitempty"`
	PriorTargetBps uint32                           `json:"priorTargetBps"`
	Metrics        economics.MonetaryMetrics        `json:"metrics"`
	Policy         *economics.MonetaryPolicy        `json:"policy,omitempty"`
	ComputeMarket  *economics.ComputeMarketMetrics  `json:"computeMarket,omitempty"`
	ScarcityConfig *economics.ComputeScarcityConfig `json:"scarcityConfig,omitempty"`
	FeedbackPolicy *economics.ComputeFeedbackPolicy `json:"feedbackPolicy,omitempty"`
}

type simulationOutput struct {
	Monetary economics.MonetaryDecision         `json:"monetary"`
	Scarcity *economics.ComputeScarcitySnapshot `json:"scarcity,omitempty"`
	Feedback *economics.ComputeFeedbackDecision `json:"feedback,omitempty"`
}

func main() {
	inputPath := flag.String("input", "-", "JSON input path, or - for stdin")
	flag.Parse()
	reader := io.Reader(os.Stdin)
	if *inputPath != "-" {
		file, err := os.Open(*inputPath)
		if err != nil {
			fatal(err)
		}
		defer file.Close()
		reader = file
	}
	var input simulationInput
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fatal(err)
	}
	policy := economics.DefaultShadowPolicy()
	if input.Policy != nil {
		policy = *input.Policy
	}
	decision, err := economics.EvaluateShadow(input.PriorTargetBps, input.Metrics, policy)
	if err != nil {
		fatal(err)
	}
	output := simulationOutput{Monetary: decision}
	if input.ComputeMarket != nil {
		if input.Epoch == 0 {
			fatal(fmt.Errorf("epoch is required when computeMarket is provided"))
		}
		scarcityConfig := economics.DefaultComputeScarcityConfig()
		if input.ScarcityConfig != nil {
			scarcityConfig = *input.ScarcityConfig
		}
		scarcity, err := economics.BuildComputeScarcity(input.Epoch, *input.ComputeMarket, scarcityConfig)
		if err != nil {
			fatal(err)
		}
		output.Scarcity = &scarcity
		feedbackPolicy := economics.DefaultComputeFeedbackPolicy(economics.ComputeFeedbackObserveOnly)
		if input.FeedbackPolicy != nil {
			feedbackPolicy = *input.FeedbackPolicy
		}
		feedback, err := economics.EvaluateComputeFeedback(decision, input.Metrics, policy, scarcity, feedbackPolicy)
		if err != nil {
			fatal(err)
		}
		output.Feedback = &feedback
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "zephyr-econ-sim: %v\n", err)
	os.Exit(1)
}
