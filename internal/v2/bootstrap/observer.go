// Package bootstrap provides an explicitly bounded Protocol v2 observer
// startup. It is not a production validator or a finalized-history recovery
// implementation. Once a network session has been started, restart requires
// an independently authenticated recovery procedure which is not yet present.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
	p2p "github.com/zephyr-chain/zephyr-chain/internal/v2/network/p2p"
	v2node "github.com/zephyr-chain/zephyr-chain/internal/v2/node"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

const (
	MaxObserverShards = 16
	sessionMarkerName = "observer-session-started"
)

var (
	ErrObserverConfig   = errors.New("invalid pinned Protocol v2 observer configuration")
	ErrObserverRecovery = errors.New("Protocol v2 observer requires verified finalized-state recovery before restart")
	ErrObserverPeers    = errors.New("invalid Protocol v2 observer bootstrap peer")
)

type Config struct {
	Genesis               genesis.Config
	ExpectedNetwork       types.NetworkID
	ExpectedValidatorRoot types.Hash
	DataDir               string
	ListenAddrs           []string
	Workers               int
}

type Observer struct {
	Network   types.NetworkID
	Transport *p2p.Node
	Service   *v2node.Service
	Runtime   *v2node.Runtime
	stores    []*worldstate.Disk
}

// OpenObserver creates a QUIC/libp2p observer (NO consensus signer) from
// independently pinned genesis and validator-root commitments. It can
// participate in the existing certified-commit protocol during its first
// session. It intentionally refuses a later session on the same data dir,
// because a durable finalized header/checkpoint replay is not yet available.
func OpenObserver(cfg Config) (out *Observer, err error) {
	if cfg.DataDir == "" || cfg.Workers < 0 ||
		types.IsZero32([32]byte(cfg.ExpectedNetwork)) ||
		types.IsZero32([32]byte(cfg.ExpectedValidatorRoot)) {
		return nil, ErrObserverConfig
	}
	anchor, err := cfg.Genesis.TrustAnchor()
	if err != nil || anchor.Network != cfg.ExpectedNetwork ||
		anchor.ValidatorRoot != cfg.ExpectedValidatorRoot {
		return nil, ErrObserverConfig
	}
	if cfg.Genesis.InitialShardCount > MaxObserverShards {
		return nil, ErrObserverConfig
	}
	native, err := cfg.Genesis.NativeTokenID()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(cfg.DataDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0077 != 0 {
		return nil, ErrObserverConfig
	}
	markerPath := filepath.Join(cfg.DataDir, sessionMarkerName)
	if _, err := os.Lstat(markerPath); err == nil {
		return nil, ErrObserverRecovery
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Derive independent expected genesis roots in memory. A persisted state
	// that moved past genesis must never be represented as height 0 on restart.
	expected := make(map[uint32]types.Hash, cfg.Genesis.InitialShardCount)
	memory := make(map[uint32]worldstate.Backend, cfg.Genesis.InitialShardCount)
	for shard := uint32(0); shard < cfg.Genesis.InitialShardCount; shard++ {
		memory[shard] = worldstate.NewMemory()
	}
	if err := genesis.SeedStates(cfg.Genesis, memory); err != nil {
		return nil, err
	}
	for shard, store := range memory {
		expected[shard] = store.Root()
	}

	stores := make([]*worldstate.Disk, 0, cfg.Genesis.InitialShardCount)
	defer func() {
		if err != nil {
			for _, store := range stores {
				_ = store.Close()
			}
		}
	}()
	backends := make(map[uint32]worldstate.Backend, cfg.Genesis.InitialShardCount)
	for shard := uint32(0); shard < cfg.Genesis.InitialShardCount; shard++ {
		dir := filepath.Join(cfg.DataDir, fmt.Sprintf("shard-%04d", shard))
		disk, openErr := worldstate.OpenDisk(dir, cfg.ExpectedNetwork)
		if openErr != nil {
			return nil, openErr
		}
		stores = append(stores, disk)
		backends[shard] = disk
	}
	if err := genesis.SeedStates(cfg.Genesis, backends); err != nil {
		return nil, err
	}
	for shard, disk := range stores {
		if disk.Root() != expected[uint32(shard)] {
			return nil, ErrObserverRecovery
		}
	}
	runtime, err := v2node.NewRuntime(cfg.ExpectedNetwork, native, cfg.ExpectedValidatorRoot, backends, cfg.Workers)
	if err != nil {
		return nil, err
	}
	if err := runtime.EnableGlobalCommitJournal(filepath.Join(cfg.DataDir, "global-commit.journal")); err != nil {
		return nil, err
	}
	if runtime.RecoveryRequired() {
		return nil, ErrObserverRecovery
	}
	validators, err := cfg.Genesis.ValidatorSet()
	if err != nil {
		return nil, err
	}

	// A durable single-use marker is synced before the observer advertises
	// a network listener. It stays in place even after clean shutdown:
	// finalized empty blocks would not change state roots, yet would advance
	// consensus height and cannot be replayed from state roots alone.
	marker, err := os.OpenFile(markerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrObserverRecovery
		}
		return nil, err
	}
	_, err = marker.Write([]byte("v2 observer active: verified finalized recovery required for reuse\n"))
	if err == nil {
		err = marker.Sync()
	}
	closeErr := marker.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	dir, err := os.Open(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	syncErr := dir.Sync()
	closeErr = dir.Close()
	if syncErr != nil {
		return nil, syncErr
	}
	if closeErr != nil {
		return nil, closeErr
	}

	identity, err := p2p.LoadOrCreateIdentity(filepath.Join(cfg.DataDir, "transport"), cfg.ExpectedNetwork)
	if err != nil {
		return nil, err
	}
	addrs := cfg.ListenAddrs
	if len(addrs) == 0 {
		addrs = []string{"/ip4/127.0.0.1/udp/0/quic-v1"}
	}
	transport, err := p2p.New(p2p.Config{
		Network: cfg.ExpectedNetwork, Identity: identity, ListenAddrs: addrs,
		MaxMessageBytes: v2node.MaxNetworkMessageBytes,
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = transport.Close()
		}
	}()
	service, err := v2node.NewService(runtime, validators, nil, transport)
	if err != nil {
		return nil, err
	}
	return &Observer{
		Network: cfg.ExpectedNetwork, Transport: transport,
		Service: service, Runtime: runtime, stores: stores,
	}, nil
}

func (o *Observer) Close() error {
	if o == nil {
		return nil
	}
	var err error
	if o.Transport != nil {
		err = errors.Join(err, o.Transport.Close())
	}
	for _, store := range o.stores {
		err = errors.Join(err, store.Close())
	}
	return err
}

// ConnectPeers dials operator-pinned libp2p peer IDs (not anonymous DNS
// endpoints). A peer identity authenticates transport, not finalized state:
// every received commit still requires a valid QC and deterministic replay.
func (o *Observer) ConnectPeers(ctx context.Context, peers []peer.AddrInfo) error {
	if o == nil || o.Transport == nil {
		return ErrObserverConfig
	}
	for _, remote := range peers {
		if remote.ID == "" || remote.ID == o.Transport.ID() || len(remote.Addrs) == 0 {
			return ErrObserverPeers
		}
		if err := o.Transport.Connect(ctx, remote); err != nil {
			return err
		}
	}
	return nil
}

func ParsePinnedPeers(values []string) ([]peer.AddrInfo, error) {
	seen := make(map[peer.ID]struct{}, len(values))
	peers := make([]peer.AddrInfo, 0, len(values))
	for _, value := range values {
		maddr, err := ma.NewMultiaddr(value)
		if err != nil {
			return nil, ErrObserverPeers
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil || info.ID == "" || len(info.Addrs) == 0 {
			return nil, ErrObserverPeers
		}
		if _, exists := seen[info.ID]; exists {
			return nil, ErrObserverPeers
		}
		seen[info.ID] = struct{}{}
		peers = append(peers, *info)
	}
	return peers, nil
}
