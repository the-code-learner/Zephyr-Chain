// Command v2-lab-validator launches a deliberately restricted Protocol V2
// validator in its own OS process. It is NOT approved for mainnet.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/bootstrap"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const maxLabGenesisBytes = 1 << 20
const maxLabKeyBytes = 8192

var errLabCommand = errors.New("invalid lab-only validator arguments or private signing key")

func pinnedHash(value string) (types.Hash, error) {
	var h types.Hash
	if len(value) != 64 || value != strings.ToLower(value) {
		return h, errLabCommand
	}
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != len(h) {
		return h, errLabCommand
	}
	copy(h[:], raw)
	if types.IsZero32([32]byte(h)) {
		return types.Hash{}, errLabCommand
	}
	return h, nil
}

// Never place the private key in argv, env or this command's JSON output.
// The owner-only DER file is pre-provisioned by the local CI launcher.
func readLabPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errLabCommand
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 ||
		info.Size() <= 0 || info.Size() > maxLabKeyBytes {
		return nil, errLabCommand
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errLabCommand
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errLabCommand
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxLabKeyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxLabKeyBytes {
		return nil, errLabCommand
	}
	key, err := x509.ParseECPrivateKey(raw)
	if err != nil {
		untyped, e := x509.ParsePKCS8PrivateKey(raw)
		if e != nil {
			return nil, errLabCommand
		}
		var ok bool
		key, ok = untyped.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errLabCommand
		}
	}
	return key, nil
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("v2-lab-validator", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	allowLab := flags.Bool("acknowledge-lab-only", false, "required; never use for mainnet")
	genesisPath := flags.String("genesis", "", "canonical v2 genesis JSON")
	networkHex := flags.String("expected-network-id", "", "pinned 64-hex network ID")
	rootHex := flags.String("expected-validator-root", "", "pinned 64-hex validator root")
	stateDir := flags.String("data-dir", "", "private state directory")
	journalDir := flags.String("signing-journal-dir", "", "separate protected journal directory shared by all processes holding the key")
	keyPath := flags.String("private-key-der", "", "owner-only DER P-256 signing key file, never in argv")
	listen := flags.String("listen", "/ip4/127.0.0.1/udp/0/quic-v1", "libp2p QUIC listen multiaddr")
	peerList := flags.String("peers", "", "comma-separated full pinned peer multiaddrs")
	if err := flags.Parse(args); err != nil {
		return errLabCommand
	}
	if !*allowLab || flags.NArg() != 0 || *genesisPath == "" ||
		*stateDir == "" || *journalDir == "" || *keyPath == "" ||
		!filepath.IsAbs(*stateDir) || !filepath.IsAbs(*journalDir) {
		return errLabCommand
	}
	netHash, err := pinnedHash(*networkHex)
	if err != nil {
		return err
	}
	rootHash, err := pinnedHash(*rootHex)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(*genesisPath) {
		return errLabCommand
	}
	f, err := os.Open(*genesisPath)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, maxLabGenesisBytes+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || len(raw) == 0 || len(raw) > maxLabGenesisBytes {
		return errLabCommand
	}
	g, err := genesis.ParseJSON(raw)
	if err != nil {
		return err
	}
	// A network named for public production must not be launched using
	// this deliberately unfinished, test-only validator command.
	chain := strings.ToLower(g.ChainName)
	if !strings.Contains(chain, "lab") && !strings.Contains(chain, "devnet") {
		return errLabCommand
	}
	key, err := readLabPrivateKey(*keyPath)
	if err != nil {
		return err
	}
	var peers []string
	if *peerList != "" {
		peers = strings.Split(*peerList, ",")
	}
	pinnedPeers, err := bootstrap.ParsePinnedPeers(peers)
	if err != nil {
		return err
	}
	validator, err := bootstrap.OpenLabValidator(bootstrap.LabValidatorConfig{
		ObserverConfig: bootstrap.Config{
			Genesis: g, ExpectedNetwork: types.NetworkID(netHash),
			ExpectedValidatorRoot: rootHash, DataDir: *stateDir,
			ListenAddrs: []string{*listen}, Workers: 2,
		},
		Key: key, SigningJournalDir: *journalDir,
	})
	if err != nil {
		return err
	}
	defer validator.Close()
	if err := validator.ConnectPeers(ctx, pinnedPeers); err != nil {
		return err
	}
	ids := make([]peer.ID, 0, len(pinnedPeers))
	for _, peer := range pinnedPeers {
		ids = append(ids, peer.ID)
	}
	// The service must send votes only to explicitly configured peers.
	for _, peer := range pinnedPeers {
		if peer.ID == validator.Transport.ID() {
			return errLabCommand
		}
	}
	validator.Service.SetPeers(ids)
	addresses := make([]string, 0, len(validator.Transport.Addrs()))
	for _, addr := range validator.Transport.Addrs() {
		addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr, validator.Transport.ID()))
	}
	info := struct {
		Mode          string   `json:"mode"`
		Network       string   `json:"networkId"`
		ValidatorRoot string   `json:"validatorRoot"`
		PeerID        string   `json:"peerId"`
		Addresses     []string `json:"addresses"`
		Height        uint64   `json:"finalizedHeight"`
	}{Mode: "lab-validator-not-mainnet", Network: netHash.String(), ValidatorRoot: rootHash.String(),
		PeerID: validator.Transport.ID().String(), Addresses: addresses, Height: validator.Runtime.Height}
	if err := json.NewEncoder(out).Encode(info); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "v2 lab validator startup refused:", err)
		os.Exit(1)
	}
}
