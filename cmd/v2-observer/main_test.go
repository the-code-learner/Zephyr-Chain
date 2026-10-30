//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/bootstrap"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
	v2node "github.com/zephyr-chain/zephyr-chain/internal/v2/node"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
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

func TestV2ObserverExternalHighWaterStoreCLI(t *testing.T) {
	genesisPath, network, root := observerCLIConfig(t)
	dataDir := filepath.Join(t.TempDir(), "observer")
	anchorParent := t.TempDir()
	if err := os.Chmod(anchorParent, 0700); err != nil {
		t.Fatal(err)
	}
	anchor := filepath.Join(anchorParent, "minimum-finalized.anchor")
	base := []string{"-genesis", genesisPath, "-expected-network-id", network, "-expected-validator-root", root,
		"-data-dir", dataDir, "-external-anchor-file", anchor}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := run(ctx, base, &out); err != nil {
		t.Fatalf("separately held anchor CLI startup failed: %v", err)
	}
	var info startInfo
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(info.RollbackGuard, "UNANCHORED") || info.FinalizedHeight != 0 || info.TrustedHeight != 0 {
		t.Fatalf("incorrect external anchor status: %+v", info)
	}
	if _, err := os.Stat(anchor); err != nil {
		t.Fatalf("CLI did not persist private separate guard: %v", err)
	}
	if err := os.Remove(anchor); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, base, &bytes.Buffer{}); err == nil {
		t.Fatal("CLI recreated missing externally held anchor on observer restart")
	}
}

func TestV2ObserverCLIVerifiesWitnessQuorumBeforeStorage(t *testing.T) {
	genesisPath, networkHex, rootHex := observerCLIConfig(t)
	n, err := parsePinned32(networkHex)
	if err != nil {
		t.Fatal(err)
	}
	r, err := parsePinned32(rootHex)
	if err != nil {
		t.Fatal(err)
	}
	cp := v2node.FinalityCheckpoint{Network: types.NetworkID(n), ValidatorRoot: r, Height: 2, HeaderHash: types.Hash{4}}
	digest, err := v2node.WitnessCheckpointDigest(cp)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 4)
	privates := make([]ed25519.PrivateKey, 4)
	bundle := v2node.SignedWitnessCheckpoint{NetworkID: networkHex, ValidatorRoot: rootHex, Height: 2, HeaderHash: hex.EncodeToString(cp.HeaderHash[:])}
	for i := 0; i < 4; i++ {
		seed := make([]byte, ed25519.SeedSize)
		seed[0] = byte(i + 1)
		privates[i] = ed25519.NewKeyFromSeed(seed)
		keys[i] = hex.EncodeToString(privates[i].Public().(ed25519.PublicKey))
		if i < 3 {
			bundle.Signatures = append(bundle.Signatures, v2node.WitnessSignature{
				PublicKey: keys[i], Signature: hex.EncodeToString(ed25519.Sign(privates[i], digest[:])),
			})
		}
	}
	sort.Slice(bundle.Signatures, func(i, j int) bool { return bundle.Signatures[i].PublicKey < bundle.Signatures[j].PublicKey })
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "trusted.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(t.TempDir(), "fresh")
	args := []string{"-genesis", genesisPath, "-expected-network-id", networkHex, "-expected-validator-root", rootHex,
		"-data-dir", dataDir, "-witness-checkpoint-json", path, "-witness-public-keys", strings.Join(keys, ",")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, args, &bytes.Buffer{}); !errors.Is(err, bootstrap.ErrObserverRecovery) {
		t.Fatalf("correctly signed already-finalized checkpoint should refuse empty genesis and reach recovery gate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "observer-session-started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("signed existing-height checkpoint incorrectly marked an unverified genesis session")
	}
	missingKeys := args[:len(args)-2]
	missingKeys = append(append([]string{}, missingKeys...), "-witness-checkpoint-json", path)
	if err := run(ctx, missingKeys, &bytes.Buffer{}); !errors.Is(err, bootstrap.ErrObserverConfig) {
		t.Fatalf("witness bundle without independent keys accepted: %v", err)
	}
	forged := bundle
	forged.Signatures = append([]v2node.WitnessSignature(nil), bundle.Signatures...)
	forged.Signatures[0].Signature = hex.EncodeToString(make([]byte, ed25519.SignatureSize))
	mutated, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, mutated, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, args, &bytes.Buffer{}); !errors.Is(err, v2node.ErrWitnessCheckpoint) {
		t.Fatalf("forged witness quorum accepted: %v", err)
	}
}
