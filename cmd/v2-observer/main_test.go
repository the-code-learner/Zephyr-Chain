//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
)

func observerCLIConfig(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	public := elliptic.Marshal(elliptic.P256(), elliptic.P256().Params().Gx, elliptic.P256().Params().Gy)
	owner := make([]byte, 32)
	owner[0] = 1
	raw := []byte(fmt.Sprintf(`{"version":2,"chainName":"cli-observer","genesisUnix":1,"initialShardCount":1,"maxShardCount":1,"nativeSymbol":"ZPH","validators":[{"consensusPublicKey":"%s","votingPower":"100"}],"allocations":[{"owner":"%s","amount":"250"}]}`, hex.EncodeToString(public), hex.EncodeToString(owner)))
	g, err := genesis.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := g.TrustAnchor()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "genesis.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, anchor.Network.String(), anchor.ValidatorRoot.String()
}

func TestV2ObserverCLIRequiresExternalTrustAndRefusesRestart(t *testing.T) {
	path, network, root := observerCLIConfig(t)
	dataDir := filepath.Join(t.TempDir(), "observer")
	base := []string{"-genesis", path, "-expected-network-id", network, "-expected-validator-root", root, "-data-dir", dataDir}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, []string{"-genesis", path, "-data-dir", dataDir}, &bytes.Buffer{}); err == nil {
		t.Fatal("untrusted CLI startup accepted")
	}
	wrong := append(append([]string(nil), base...), "-peers", "not-a-pinned-peer")
	if err := run(ctx, wrong, &bytes.Buffer{}); err == nil {
		t.Fatal("anonymous peer bootstrap accepted")
	}
	var out bytes.Buffer
	if err := run(ctx, base, &out); err != nil {
		t.Fatalf("pinned observer failed first launch: %v", err)
	}
	var info startInfo
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Mode != "observer-only" || info.NetworkID != network ||
		info.ValidatorRoot != root || info.FinalizedHeight != 0 ||
		info.PeerID == "" || len(info.Addresses) == 0 {
		t.Fatalf("incorrect startup identity/anchor report: %+v", info)
	}
	if err := run(ctx, base, &bytes.Buffer{}); err == nil {
		t.Fatal("CLI silently restarted without finalized checkpoint recovery")
	}
}

func TestV2ObserverCLIRejectsMalformedHashesAndHugeGenesis(t *testing.T) {
	for _, value := range []string{"", "a", " 123", string(bytes.Repeat([]byte{'0'}, 64)), string(bytes.Repeat([]byte{'z'}, 64))} {
		if _, err := parsePinned32(value); err == nil {
			t.Fatalf("invalid expected hash accepted: %q", value)
		}
	}
	if _, err := parsePinned32("1" + string(bytes.Repeat([]byte{'0'}, 63))); err != nil {
		t.Fatalf("well-formed anchor rejected: %v", err)
	}
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, maxGenesisBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseGenesisFile(path); err == nil {
		t.Fatal("unbounded genesis file accepted")
	}
}

func TestV2ObserverCLIRequiresPairedExternalCheckpointAndRejectsFreshGenesis(t *testing.T) {
	path, network, root := observerCLIConfig(t)
	dataDir := filepath.Join(t.TempDir(), "anchored-observer")
	base := []string{"-genesis", path, "-expected-network-id", network, "-expected-validator-root", root, "-data-dir", dataDir}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	missingHash := append(append([]string{}, base...), "-trusted-finalized-height", "2")
	if err := run(ctx, missingHash, &bytes.Buffer{}); err == nil {
		t.Fatal("height-only trust pin accepted")
	}
	missingHeight := append(append([]string{}, base...), "-trusted-finalized-hash", "1"+string(bytes.Repeat([]byte{'0'}, 63)))
	if err := run(ctx, missingHeight, &bytes.Buffer{}); err == nil {
		t.Fatal("hash-only trust pin accepted")
	}
	invalidHash := append(append([]string{}, base...), "-trusted-finalized-height", "2", "-trusted-finalized-hash", "invalid")
	if err := run(ctx, invalidHash, &bytes.Buffer{}); err == nil {
		t.Fatal("invalid external hash accepted")
	}
	validPair := append(append([]string{}, base...), "-trusted-finalized-height", "2", "-trusted-finalized-hash", "1"+string(bytes.Repeat([]byte{'0'}, 63)))
	if err := run(ctx, validPair, &bytes.Buffer{}); err == nil {
		t.Fatal("fresh genesis accepted despite externally finalized checkpoint")
	}
	// Optional trust pins are for a local experimental devnet only:
	// the startup report must explicitly expose missing anti-rollback.
	var out bytes.Buffer
	if err := run(ctx, base, &out); err != nil {
		t.Fatal(err)
	}
	var status startInfo
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.TrustedHeight != 0 || !strings.Contains(status.RollbackGuard, "UNANCHORED") {
		t.Fatalf("missing external trust warning: %+v", status)
	}
}
