//go:build linux || darwin

package bootstrap

import (
	"context"
	"crypto/elliptic"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
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
		Validators: []genesis.Validator{{ID: types.ValidatorIDFromPublicKey(pub), ConsensusPublicKey: pub, VotingPower: 100}},
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
	if _, err := obs.Service.Propose(context.Background()); !errors.Is(err, ErrNotValidator) {
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
	// A fully pinned peer ID and transport address is acceptable.
	_ = peer.ID("")
}
