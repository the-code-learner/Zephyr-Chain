//go:build linux || darwin

package bootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
	p2p "github.com/zephyr-chain/zephyr-chain/internal/v2/network/p2p"
	v2node "github.com/zephyr-chain/zephyr-chain/internal/v2/node"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func observerFixture(t *testing.T, dir string) Config {
	t.Helper()
	pub := elliptic.Marshal(elliptic.P256(), elliptic.P256().Params().Gx, elliptic.P256().Params().Gy)
	g := genesis.Config{
		Version: genesis.ProtocolVersion, ChainName: "verified-observer-test",
		GenesisUnix: 1, InitialShardCount: 2, MaxShardCount: 2, NativeSymbol: "ZPH",
		Validators:  []genesis.Validator{{ID: types.ValidatorIDFromPublicKey(pub), ConsensusPublicKey: pub, VotingPower: 100}},
		Allocations: []genesis.Allocation{{Owner: types.AccountID{1}, Amount: 1_000}, {Owner: types.AccountID{2}, Amount: 500}},
	}
	anchor, err := g.TrustAnchor()
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		Genesis: g, ExpectedNetwork: anchor.Network,
		ExpectedValidatorRoot: anchor.ValidatorRoot, DataDir: dir,
		ListenAddrs: []string{"/ip4/127.0.0.1/udp/0/quic-v1"}, Workers: 1,
	}
}

func TestPinnedObserverRequiresIndependentTrustAndIsNeverValidator(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "operator")
	cfg := observerFixture(t, dir)
	wrong := cfg
	wrong.ExpectedNetwork = types.NetworkID{5}
	if _, err := OpenObserver(wrong); !errors.Is(err, ErrObserverConfig) {
		t.Fatalf("untrusted network accepted: %v", err)
	}
	wrong = cfg
	wrong.ExpectedValidatorRoot = types.Hash{7}
	if _, err := OpenObserver(wrong); !errors.Is(err, ErrObserverConfig) {
		t.Fatalf("untrusted validator root accepted: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrong trust anchor left disk state behind")
	}
	obs, err := OpenObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Runtime.Height != 0 || obs.Runtime.ShardCount != cfg.Genesis.InitialShardCount {
		t.Fatal("unexpected observer genesis runtime")
	}
	if _, err := obs.Service.Propose(context.Background()); !errors.Is(err, v2node.ErrNotValidator) {
		t.Fatalf("observer unexpectedly allowed to propose: %v", err)
	}
	for shard := uint32(0); shard < cfg.Genesis.InitialShardCount; shard++ {
		store, ok := obs.Service.ShardState(shard)
		if !ok {
			t.Fatalf("missing shard %d", shard)
		}
		if _, ok := store.GetObject(genesis.GenesisMarkerID(cfg.ExpectedNetwork, shard)); !ok {
			t.Fatalf("genesis marker absent from shard %d", shard)
		}
	}
	transportID := obs.Transport.ID()
	if transportID == "" {
		t.Fatal("libp2p observer has no identity")
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("concurrent observer startup allowed: %v", err)
	}
	if err := obs.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("state was reset to height 0 after network session: %v", err)
	}
}

func TestPinnedObserverRefusesPostGenesisStateWithoutMarker(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "operator")
	cfg := observerFixture(t, dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	states := make(map[uint32]worldstate.Backend)
	disks := make([]*worldstate.Disk, 0)
	for shard := uint32(0); shard < cfg.Genesis.InitialShardCount; shard++ {
		disk, err := worldstate.OpenDisk(filepath.Join(dir, fmt.Sprintf("shard-%04d", shard)), cfg.ExpectedNetwork)
		if err != nil {
			t.Fatal(err)
		}
		disks = append(disks, disk)
		states[shard] = disk
	}
	if err := genesis.SeedStates(cfg.Genesis, states); err != nil {
		t.Fatal(err)
	}
	native, err := cfg.Genesis.NativeTokenID()
	if err != nil {
		t.Fatal(err)
	}
	coin, err := object.NewCoinOutput(types.AccountID{3}, native, 1)
	if err != nil {
		t.Fatal(err)
	}
	fake := object.Object{ID: types.ObjectIDForShard(types.Hash{8}, 0, 0), Version: 1,
		Owner: types.AccountID{3}, Kind: coin.Kind, Data: coin.Data}
	if _, err := disks[0].Apply(nil, []object.Object{fake}); err != nil {
		t.Fatal(err)
	}
	for _, disk := range disks {
		if err := disk.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("post-genesis state treated as fresh genesis: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, sessionMarkerName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed preflight must not mark session started")
	}
}

func TestPinnedObserverRejectsInsecureDirAndUnboundedShardCount(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "operator")
	cfg := observerFixture(t, dir)
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverConfig) {
		t.Fatalf("world-accessible state directory accepted: %v", err)
	}
	cfg = observerFixture(t, filepath.Join(t.TempDir(), "other"))
	cfg.Genesis.InitialShardCount = MaxObserverShards + 1
	cfg.Genesis.MaxShardCount = MaxObserverShards + 1
	anchor, err := cfg.Genesis.TrustAnchor()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExpectedNetwork, cfg.ExpectedValidatorRoot = anchor.Network, anchor.ValidatorRoot
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverConfig) {
		t.Fatalf("unbounded shard startup accepted: %v", err)
	}
}

func TestPinnedPeerParsingRejectsAnonymousOrDuplicate(t *testing.T) {
	for _, values := range [][]string{
		{""}, {"/ip4/127.0.0.1/udp/9901/quic-v1"},
		{"/ip4/127.0.0.1/udp/9901/quic-v1/p2p/invalid"},
	} {
		if _, err := ParsePinnedPeers(values); !errors.Is(err, ErrObserverPeers) {
			t.Fatalf("invalid peer accepted %v: %v", values, err)
		}
	}
	if got, err := ParsePinnedPeers(nil); err != nil || len(got) != 0 {
		t.Fatal("empty offline peer list failed:", err)
	}
	// A fully pinned peer ID and QUIC transport address is acceptable.
	network := types.NetworkID{1}
	transport, err := p2p.New(p2p.Config{Network: network, ListenAddrs: []string{"/ip4/127.0.0.1/udp/0/quic-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	addr := fmt.Sprintf("%s/p2p/%s", transport.Addrs()[0], transport.ID())
	got, err := ParsePinnedPeers([]string{addr})
	if err != nil || len(got) != 1 || got[0].ID != transport.ID() {
		t.Fatalf("pinned peer was rejected: %v", err)
	}
	if _, err := ParsePinnedPeers([]string{addr, addr}); !errors.Is(err, ErrObserverPeers) {
		t.Fatalf("duplicate peer was accepted: %v", err)
	}
}

func appendSingleValidatorCertifiedEmptyBlock(t *testing.T, obs *Observer) {
	t.Helper()
	// Only the deterministic test fixture owns D=1 (base-point public key).
	// Production validators must never derive keys from public fixtures.
	curve := elliptic.P256()
	key := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: curve.Params().Gx, Y: curve.Params().Gy},
		D:         big.NewInt(1),
	}
	height := obs.Runtime.Height + 1
	candidate, err := obs.Runtime.BuildCandidate(height, nil)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := v2consensus.SignProposal(key, candidate.Header, 0)
	if err != nil {
		t.Fatal(err)
	}
	vote, err := v2consensus.SignVote(key, obs.Network, height, 0, v2consensus.HeaderConsensusHash(candidate.Header))
	if err != nil {
		t.Fatal(err)
	}
	set := obs.Service.Validators
	cert, err := set.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := obs.Runtime.Commit(candidate, cert, set); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedObserverRecoversOnlyCertifiedGenesisSuccessor(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "operator")
	cfg := observerFixture(t, dir)
	obs, err := OpenObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	wantParent := obs.Runtime.ParentHash
	if obs.Runtime.Height != 1 {
		t.Fatal("first quorum-certified height missing")
	}
	if err := obs.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := OpenObserver(cfg)
	if err != nil {
		t.Fatalf("valid genesis-successor recovery rejected: %v", err)
	}
	if recovered.Runtime.Height != 1 || recovered.Runtime.ParentHash != wantParent ||
		recovered.Runtime.RecoveryRequired() {
		t.Fatalf("inconsistent height/parent after QC recovery: %d %x", recovered.Runtime.Height, recovered.Runtime.ParentHash)
	}
	if _, err := recovered.Service.Propose(context.Background()); !errors.Is(err, v2node.ErrNotValidator) {
		t.Fatalf("recovered observer acquired voting authority: %v", err)
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := OpenObserver(cfg)
	if err != nil {
		t.Fatalf("idempotent recoverable anchor refused: %v", err)
	}
	if again.Runtime.Height != 1 || again.Runtime.ParentHash != wantParent {
		t.Fatal("idempotent recovery moved the finality anchor")
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedObserverReplaysTwoQCFinalizedHeightsIncludingEmptyBlocks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "operator")
	cfg := observerFixture(t, dir)
	obs, err := OpenObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	expectedParent := obs.Runtime.ParentHash
	if obs.Runtime.Height != 2 {
		t.Fatal("fixture did not reach height two")
	}
	if err := obs.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenObserver(cfg)
	if err != nil {
		t.Fatalf("two certified heights rejected after genesis-to-tip replay: %v", err)
	}
	if restarted.Runtime.Height != 2 || restarted.Runtime.ParentHash != expectedParent || restarted.Runtime.RecoveryRequired() {
		t.Fatalf("wrong recovered finality anchor at height %d", restarted.Runtime.Height)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := OpenObserver(cfg)
	if err != nil {
		t.Fatalf("second multiheight restart failed: %v", err)
	}
	if again.Runtime.Height != 2 || again.Runtime.ParentHash != expectedParent {
		t.Fatal("second restart changed certified finality anchor")
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedObserverRejectsTamperedCertifiedJournal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "operator")
	cfg := observerFixture(t, dir)
	obs, err := OpenObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	if err := obs.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "global-commit.journal")
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < 40 {
		t.Fatalf("invalid test journal: %v", err)
	}
	raw[len(raw)-1] ^= 0x01
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("tampered QC journal accepted: %v", err)
	}
}

func TestPinnedObserverRejectsPostCommitStateRootTampering(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "operator")
	cfg := observerFixture(t, dir)
	obs, err := OpenObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	if err := obs.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := worldstate.OpenDisk(filepath.Join(dir, "shard-0000"), cfg.ExpectedNetwork)
	if err != nil {
		t.Fatal(err)
	}
	added := object.Object{
		ID:   types.ObjectIDForShard(types.Hash{44}, 1, 0),
		Kind: object.KindSystem, Version: 1, Data: []byte("invalid-uncommitted-state"),
	}
	if _, err := disk.Apply(nil, []object.Object{added}); err != nil {
		t.Fatal(err)
	}
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("uncommitted state was accepted as certified: %v", err)
	}
}

func TestPinnedObserverRefusesDeletedOrTruncatedCertifiedHistory(t *testing.T) {
	for _, mutate := range []struct {
		name string
		fn   func(string) error
	}{
		{"missing", func(path string) error { return os.Remove(path) }},
		{"truncated", func(path string) error {
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			defer f.Close()
			return f.Truncate(111)
		}},
		{"checksum", func(path string) error {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			raw[len(raw)-1] ^= 1
			return os.WriteFile(path, raw, 0600)
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			cfg := observerFixture(t, filepath.Join(t.TempDir(), "operator"))
			obs, err := OpenObserver(cfg)
			if err != nil {
				t.Fatal(err)
			}
			appendSingleValidatorCertifiedEmptyBlock(t, obs)
			appendSingleValidatorCertifiedEmptyBlock(t, obs)
			if err := obs.Close(); err != nil {
				t.Fatal(err)
			}
			if err := mutate.fn(filepath.Join(cfg.DataDir, "certified-history.log")); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
				t.Fatalf("unverifiable multiheight history accepted: %v", err)
			}
		})
	}
}

func TestPinnedObserverRejectsHistoryWithForgedIntermediateQC(t *testing.T) {
	// The lower-level history regression can rewrite a valid file checksum;
	// this startup test covers absent/incomplete intermediary history.
	cfg := observerFixture(t, filepath.Join(t.TempDir(), "operator"))
	obs, err := OpenObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	if err := obs.Close(); err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(cfg.DataDir, "certified-history.log")
	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	// Full history is not the latest single-commit journal: removing its
	// intermediate record must never re-anchor at the latest height alone.
	if err := os.WriteFile(historyPath, data[:len(data)/2], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("missing intermediate QC was accepted: %v", err)
	}
}

func TestPinnedObserverRefusesOldValidHistoryAgainstExternalCheckpoint(t *testing.T) {
	cfg := observerFixture(t, filepath.Join(t.TempDir(), "operator"))
	obs, err := OpenObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	pinnedTwo := obs.Runtime.ParentHash
	if err := obs.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.TrustedCheckpoint = &v2node.FinalityCheckpoint{
		Network: cfg.ExpectedNetwork, ValidatorRoot: cfg.ExpectedValidatorRoot,
		Height: 2, HeaderHash: pinnedTwo,
	}
	verified, err := OpenObserver(cfg)
	if err != nil {
		t.Fatalf("externally pinned QC ancestor rejected: %v", err)
	}
	if verified.Runtime.Height != 2 || verified.Runtime.ParentHash != pinnedTwo {
		t.Fatal("checkpoint did not bind recovered height and hash")
	}
	if err := verified.Close(); err != nil {
		t.Fatal(err)
	}
	older := cfg
	older.TrustedCheckpoint = &v2node.FinalityCheckpoint{
		Network: cfg.ExpectedNetwork, ValidatorRoot: cfg.ExpectedValidatorRoot,
		Height: 3, HeaderHash: types.Hash{7},
	}
	if _, err := OpenObserver(older); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("stale but valid QC history bypassed external checkpoint: %v", err)
	}
	forged := cfg
	forged.TrustedCheckpoint = &v2node.FinalityCheckpoint{
		Network: cfg.ExpectedNetwork, ValidatorRoot: cfg.ExpectedValidatorRoot,
		Height: 2, HeaderHash: types.Hash{8},
	}
	if _, err := OpenObserver(forged); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("same-height forked checkpoint accepted: %v", err)
	}
}

func TestPinnedObserverRefusesExternalCheckpointWithoutExistingVerifiedHistory(t *testing.T) {
	cfg := observerFixture(t, filepath.Join(t.TempDir(), "new-observer"))
	cfg.TrustedCheckpoint = &v2node.FinalityCheckpoint{
		Network: cfg.ExpectedNetwork, ValidatorRoot: cfg.ExpectedValidatorRoot,
		Height: 1, HeaderHash: types.Hash{9},
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("fresh genesis was allowed despite externally pinned finalized height: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, sessionMarkerName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unverified fresh session incorrectly marked active")
	}
	cfg.TrustedCheckpoint.Network = types.NetworkID{4}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverConfig) {
		t.Fatalf("foreign-network external anchor accepted: %v", err)
	}
}

func TestObserverWithSeparateFinalityAnchorPreservesHighWaterOnRestart(t *testing.T) {
	cfg := observerFixture(t, filepath.Join(t.TempDir(), "state"))
	external := t.TempDir()
	cfg.ExternalAnchorPath = filepath.Join(external, "finality.anchor")
	obs, err := OpenObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	appendSingleValidatorCertifiedEmptyBlock(t, obs)
	tip := obs.Runtime.ParentHash
	if err := obs.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenObserver(cfg)
	if err != nil {
		t.Fatalf("separately preserved anchor refused certified restart: %v", err)
	}
	if restarted.Runtime.Height != 2 || restarted.Runtime.ParentHash != tip {
		t.Fatalf("unexpected restored tip: %d %x", restarted.Runtime.Height, restarted.Runtime.ParentHash)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.ExternalAnchorPath); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("missing external high-water mark silently recreated: %v", err)
	}
}

func TestObserverRejectsNonIndependentAnchorPathAndMixedTrust(t *testing.T) {
	cfg := observerFixture(t, filepath.Join(t.TempDir(), "state"))
	cfg.ExternalAnchorPath = filepath.Join(cfg.DataDir, "finality.anchor")
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverConfig) {
		t.Fatalf("anchor inside rollback directory accepted: %v", err)
	}
	cfg.ExternalAnchorPath = "relative-anchor"
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverConfig) {
		t.Fatalf("relative ambiguous anchor accepted: %v", err)
	}
	cfg.ExternalAnchorPath = filepath.Join(t.TempDir(), "outside")
	cfg.TrustedCheckpoint = &v2node.FinalityCheckpoint{
		Network: cfg.ExpectedNetwork, ValidatorRoot: cfg.ExpectedValidatorRoot, Height: 1, HeaderHash: types.Hash{9},
	}
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverConfig) {
		t.Fatalf("two non-comparable trust pins accepted without checking both: %v", err)
	}
}

func TestObserverRejectsWorldReadableAnchorParent(t *testing.T) {
	cfg := observerFixture(t, filepath.Join(t.TempDir(), "state"))
	public := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	cfg.ExternalAnchorPath = filepath.Join(public, "finality.anchor")
	if _, err := OpenObserver(cfg); !errors.Is(err, ErrObserverRecovery) {
		t.Fatalf("publicly readable external anchor parent accepted: %v", err)
	}
}
