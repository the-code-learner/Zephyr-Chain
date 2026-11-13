// Command v2-lab-fixture creates EPHEMERAL per-process test keys.
// It must never be used to provision a mainnet or persistent operator keys.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type validatorSpec struct {
	ConsensusPublicKey string `json:"consensusPublicKey"`
	VotingPower        string `json:"votingPower"`
}

type manifestValidator struct {
	Index      int    `json:"index"`
	KeyFile    string `json:"keyFile"`
	DataDir    string `json:"dataDir"`
	JournalDir string `json:"journalDir"`
	Listen     string `json:"listen"`
}

type manifest struct {
	Mode          string              `json:"mode"`
	NetworkID     string              `json:"networkId"`
	ValidatorRoot string              `json:"validatorRoot"`
	GenesisFile   string              `json:"genesisFile"`
	Validators    []manifestValidator `json:"validators"`
}

func createFixture(dir string, count int) (manifest, error) {
	if count < 4 || count > 7 || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return manifest{}, errors.New("invalid ephemeral lab fixture configuration")
	}
	// Must be a new private directory: do not overwrite or import keys.
	if err := os.Mkdir(dir, 0700); err != nil {
		return manifest{}, err
	}
	g := genesis.Config{
		Version: genesis.ProtocolVersion, ChainName: "github-v2-lab-devnet",
		GenesisUnix: 1, InitialShardCount: 1, MaxShardCount: 1,
		NativeSymbol: "ZPH",
	}
	validators := make([]validatorSpec, 0, count)
	result := manifest{Mode: "ephemeral-ci-keys-not-mainnet", Validators: make([]manifestValidator, 0, count)}
	for i := 0; i < count; i++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return manifest{}, err
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		g.Validators = append(g.Validators, genesis.Validator{
			ID: types.ValidatorIDFromPublicKey(pub), ConsensusPublicKey: pub, VotingPower: 100,
		})
		validators = append(validators, validatorSpec{ConsensusPublicKey: hex.EncodeToString(pub), VotingPower: "100"})
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return manifest{}, err
		}
		keyPath := filepath.Join(dir, fmt.Sprintf("validator-%02d.der", i))
		file, err := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return manifest{}, err
		}
		_, writeErr := file.Write(der)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return manifest{}, errors.Join(writeErr, closeErr)
		}
		result.Validators = append(result.Validators, manifestValidator{
			Index: i, KeyFile: keyPath,
			DataDir: filepath.Join(dir, fmt.Sprintf("node-%02d", i)),
			JournalDir: filepath.Join(dir, fmt.Sprintf("journal-%02d", i)),
			Listen: fmt.Sprintf("/ip4/127.0.0.1/udp/%d/quic-v1", 19000+i),
		})
	}
	anchor, err := g.TrustAnchor()
	if err != nil {
		return manifest{}, err
	}
	result.NetworkID = anchor.Network.String()
	result.ValidatorRoot = anchor.ValidatorRoot.String()
	result.GenesisFile = filepath.Join(dir, "genesis.json")
	raw, err := json.Marshal(struct {
		Version           uint16          `json:"version"`
		ChainName         string          `json:"chainName"`
		GenesisUnix       uint64          `json:"genesisUnix"`
		InitialShardCount uint32          `json:"initialShardCount"`
		MaxShardCount     uint32          `json:"maxShardCount"`
		NativeSymbol      string          `json:"nativeSymbol"`
		Validators        []validatorSpec `json:"validators"`
		Allocations       []any           `json:"allocations"`
	}{g.Version, g.ChainName, g.GenesisUnix, g.InitialShardCount,
		g.MaxShardCount, g.NativeSymbol, validators, []any{}})
	if err != nil {
		return manifest{}, err
	}
	if err := os.WriteFile(result.GenesisFile, raw, 0600); err != nil {
		return manifest{}, err
	}
	return result, nil
}

func main() {
	var dir string
	var validators int
	var acknowledge bool
	flag.StringVar(&dir, "dir", "", "new private absolute fixture directory")
	flag.IntVar(&validators, "validators", 4, "ephemeral lab validator count [4,7]")
	flag.BoolVar(&acknowledge, "acknowledge-lab-only", false, "required: generated keys are never production-grade")
	flag.Parse()
	if !acknowledge || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "explicit lab-only acknowledgement required")
		os.Exit(2)
	}
	m, err := createFixture(dir, validators)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lab fixture refused:", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(m); err != nil {
		fmt.Fprintln(os.Stderr, "unable to describe lab fixture:", err)
		os.Exit(1)
	}
}
