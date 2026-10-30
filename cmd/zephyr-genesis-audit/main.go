// Command zephyr-genesis-audit checks a genesis file against independently
// obtained, pinned Zephyr v2 network and validator-root commitments.
// It does not create a chain or certify future validator transitions.
package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const maxGenesisAuditBytes = 1 << 20

var ErrAuditGenesis = errors.New("invalid or untrusted Zephyr genesis audit input")

type auditReport struct {
	NetworkID      string `json:"networkId"`
	ValidatorRoot  string `json:"validatorRoot"`
	NativeTokenID  string `json:"nativeTokenId"`
	NativeSymbol   string `json:"nativeSymbol"`
	InitialSupply  uint64 `json:"initialSupply"`
	ValidatorCount int    `json:"validatorCount"`
	InitialShards  uint32 `json:"initialShardCount"`
	MaxShards      uint32 `json:"maxShardCount"`
	AnchorsMatched bool   `json:"anchorsMatched"`
}

func auditGenesis(raw []byte, expectedNetwork, expectedRoot string, inspectOnly bool) (auditReport, error) {
	if len(raw) == 0 || len(raw) > maxGenesisAuditBytes {
		return auditReport{}, ErrAuditGenesis
	}
	if !inspectOnly && (expectedNetwork == "" || expectedRoot == "") {
		return auditReport{}, ErrAuditGenesis
	}
	if inspectOnly && (expectedNetwork != "" || expectedRoot != "") {
		return auditReport{}, ErrAuditGenesis
	}
	cfg, err := genesis.ParseJSON(raw)
	if err != nil {
		return auditReport{}, err
	}
	anchor, err := cfg.TrustAnchor()
	if err != nil {
		return auditReport{}, err
	}
	native, err := cfg.NativeTokenID()
	if err != nil {
		return auditReport{}, err
	}
	var initialSupply uint64
	for _, a := range cfg.Allocations {
		if math.MaxUint64-initialSupply < a.Amount {
			return auditReport{}, genesis.ErrAllocationSupply
		}
		initialSupply += a.Amount
	}
	if !inspectOnly {
		network, err := parseExpected32(expectedNetwork)
		if err != nil || types.Hash(anchor.Network) != network {
			return auditReport{}, ErrAuditGenesis
		}
		root, err := parseExpected32(expectedRoot)
		if err != nil || anchor.ValidatorRoot != root {
			return auditReport{}, ErrAuditGenesis
		}
	}
	return auditReport{
		NetworkID:      hex.EncodeToString(anchor.Network[:]),
		ValidatorRoot:  anchor.ValidatorRoot.String(),
		NativeTokenID:  hex.EncodeToString(native[:]),
		NativeSymbol:   cfg.NativeSymbol,
		InitialSupply:  initialSupply,
		ValidatorCount: len(cfg.Validators),
		InitialShards:  cfg.InitialShardCount,
		MaxShards:      cfg.MaxShardCount,
		AnchorsMatched: !inspectOnly,
	}, nil
}

func parseExpected32(value string) (types.Hash, error) {
	var out types.Hash
	if strings.TrimSpace(value) != value || len(value) != 64 {
		return out, ErrAuditGenesis
	}
	b, err := hex.DecodeString(value)
	if err != nil || len(b) != len(out) {
		return out, ErrAuditGenesis
	}
	copy(out[:], b)
	if types.IsZero32([32]byte(out)) {
		return types.Hash{}, ErrAuditGenesis
	}
	return out, nil
}

func main() {
	genesisPath := flag.String("genesis", "", "path to v2 genesis JSON")
	network := flag.String("expected-network-id", "", "independently pinned 64-char v2 NetworkID")
	root := flag.String("expected-validator-root", "", "independently pinned 64-char genesis validator root")
	inspect := flag.Bool("inspect-only", false, "display computed values without authenticating them (NOT for deployment)")
	flag.Parse()
	if flag.NArg() != 0 || strings.TrimSpace(*genesisPath) == "" {
		fmt.Fprintln(os.Stderr, "provide -genesis and two independent expected hashes, or -inspect-only")
		os.Exit(2)
	}
	file, err := os.Open(*genesisPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maxGenesisAuditBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "cannot read complete genesis file")
		os.Exit(1)
	}
	report, err := auditGenesis(raw, *network, *root, *inspect)
	if err != nil {
		fmt.Fprintln(os.Stderr, "genesis audit refused:", err)
		os.Exit(1)
	}
	if *inspect {
		fmt.Fprintln(os.Stderr, "INSPECT ONLY: hashes were not pinned against an independent trust anchor")
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
