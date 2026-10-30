// Command v2-observer runs a deliberately restricted Protocol v2
// finalized-commit observer. It never holds a validator voting key and
// must not be deployed as an audited production/mainnet node.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/bootstrap"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const maxGenesisBytes = 1 << 20

func parsePinned32(value string) (types.Hash, error) {
	var zero types.Hash
	if len(value) != 64 || value != strings.TrimSpace(value) {
		return zero, bootstrap.ErrObserverConfig
	}
	b, err := hex.DecodeString(value)
	if err != nil || len(b) != 32 {
		return zero, bootstrap.ErrObserverConfig
	}
	copy(zero[:], b)
	if types.IsZero32([32]byte(zero)) {
		return types.Hash{}, bootstrap.ErrObserverConfig
	}
	return zero, nil
}

func parseGenesisFile(path string) (genesis.Config, error) {
	if path == "" {
		return genesis.Config{}, bootstrap.ErrObserverConfig
	}
	f, err := os.Open(path)
	if err != nil {
		return genesis.Config{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxGenesisBytes+1))
	if err != nil {
		return genesis.Config{}, err
	}
	if len(raw) == 0 || len(raw) > maxGenesisBytes {
		return genesis.Config{}, bootstrap.ErrObserverConfig
	}
	return genesis.ParseJSON(raw)
}

type startInfo struct {
	Mode            string   `json:"mode"`
	NetworkID       string   `json:"networkId"`
	ValidatorRoot   string   `json:"validatorRoot"`
	PeerID          string   `json:"peerId"`
	Addresses       []string `json:"addresses"`
	InitialShards   uint32   `json:"initialShards"`
	FinalizedHeight uint64   `json:"finalizedHeight"`
	RecoveryMode    string   `json:"recoveryMode"`
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("v2-observer", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	genesisPath := flags.String("genesis", "", "canonical JSON genesis file")
	expectedNetwork := flags.String("expected-network-id", "", "independently pinned 64-digit hex NetworkID")
	expectedRoot := flags.String("expected-validator-root", "", "independently pinned 64-digit hex validator root")
	dataDir := flags.String("data-dir", "", "private, dedicated observer state directory")
	listen := flags.String("listen", "/ip4/127.0.0.1/udp/0/quic-v1", "QUIC listen multiaddr (loopback by default)")
	peerList := flags.String("peers", "", "comma-separated full /p2p/<ID> trusted bootstrap multiaddrs")
	workers := flags.Int("workers", 2, "deterministic state executor workers")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *dataDir == "" || *workers < 0 {
		return bootstrap.ErrObserverConfig
	}
	netHash, err := parsePinned32(*expectedNetwork)
	if err != nil {
		return err
	}
	rootHash, err := parsePinned32(*expectedRoot)
	if err != nil {
		return err
	}
	g, err := parseGenesisFile(*genesisPath)
	if err != nil {
		return err
	}
	var peers []string
	if strings.TrimSpace(*peerList) != "" {
		peers = strings.Split(*peerList, ",")
	}
	remote, err := bootstrap.ParsePinnedPeers(peers)
	if err != nil {
		return err
	}
	obs, err := bootstrap.OpenObserver(bootstrap.Config{
		Genesis: g, ExpectedNetwork: types.NetworkID(netHash),
		ExpectedValidatorRoot: rootHash, DataDir: *dataDir,
		ListenAddrs: []string{*listen}, Workers: *workers,
	})
	if err != nil {
		return err
	}
	defer obs.Close()
	if len(remote) != 0 {
		dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := obs.ConnectPeers(dialCtx, remote); err != nil {
			return fmt.Errorf("trusted peer connect: %w", err)
		}
	}
	addresses := make([]string, 0, len(obs.Transport.Addrs()))
	for _, addr := range obs.Transport.Addrs() {
		addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr.String(), obs.Transport.ID()))
	}
	info := startInfo{Mode: "observer-only", NetworkID: obs.Network.String(),
		ValidatorRoot: rootHash.String(), PeerID: obs.Transport.ID().String(),
		Addresses: addresses, InitialShards: g.InitialShardCount,
		FinalizedHeight: 0, RecoveryMode: "single-session; post-genesis restart forbidden"}
	if err := json.NewEncoder(output).Encode(info); err != nil {
		return err
	}
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "v2 observer startup refused:", err)
		os.Exit(1)
	}
}
