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
	PriorTargetBps uint32                    `json:"priorTargetBps"`
	Metrics        economics.MonetaryMetrics `json:"metrics"`
	Policy         *economics.MonetaryPolicy `json:"policy,omitempty"`
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
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(decision); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "zephyr-econ-sim: %v\n", err)
	os.Exit(1)
}
