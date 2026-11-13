//go:build linux || darwin

package bootstrap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func labP256Key(d int64) *ecdsa.PrivateKey {
	secret := big.NewInt(d)
	x, y := elliptic.P256().ScalarBaseMult(secret.Bytes())
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y},
		D: secret,
	}
}

func labConfig(t *testing.T) LabValidatorConfig {
	t.Helper()
	root := t.TempDir()
	return LabValidatorConfig{
		ObserverConfig: observerFixture(t, filepath.Join(root, "state")),
		Key: labP256Key(1), SigningJournalDir: filepath.Join(root, "signer"),
	}
}

func TestProtectedLabValidatorPinnedStartupAndExclusiveSignerLock(t *testing.T) {
	cfg := labConfig(t)
	lab, err := OpenLabValidator(cfg)
	if err != nil {
		t.Fatalf("protected validator startup: %v", err)
	}
	if lab.Observer.Service.Key == nil || lab.Observer.Transport.ID() == "" {
		t.Fatal("protected lab validator did not initialize key or transport")
	}
	pub := elliptic.Marshal(elliptic.P256(), cfg.Key.PublicKey.X, cfg.Key.PublicKey.Y)
	id := types.ValidatorIDFromPublicKey(pub)
	if !lab.Guard.Matches(cfg.ObserverConfig.ExpectedNetwork, id) {
		t.Fatal("validator guard not pinned to expected genesis and key")
	}
	// A second *state* directory must not permit another OS process to
	// sign using the same key and its held journal directory.
	other := cfg
	other.ObserverConfig.DataDir = filepath.Join(filepath.Dir(cfg.ObserverConfig.DataDir), "second-state")
	if _, err := OpenLabValidator(other); !errors.Is(err, v2consensus.ErrSigningGuardBusy) {
		t.Fatalf("second key holder admitted with a locked journal: %v", err)
	}
	if _, err := os.Stat(other.ObserverConfig.DataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected second key holder created state before signer lock")
	}
	if err := lab.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedLabValidatorRejectsUntrustedPinsAndKeyBeforeOpeningState(t *testing.T) {
	tests := []struct{
		name string
		change func(*LabValidatorConfig)
	}{
		{"wrong network", func(c *LabValidatorConfig) { c.ObserverConfig.ExpectedNetwork = types.NetworkID{8} }},
		{"wrong validator root", func(c *LabValidatorConfig) { c.ObserverConfig.ExpectedValidatorRoot = types.Hash{9} }},
		{"key not in genesis", func(c *LabValidatorConfig) { c.Key = labP256Key(2) }},
		{"mismatched public and private key", func(c *LabValidatorConfig) { c.Key.PublicKey = labP256Key(2).PublicKey }},
		{"zero private key", func(c *LabValidatorConfig) { c.Key = labP256Key(0) }},
		{"missing signer path", func(c *LabValidatorConfig) { c.SigningJournalDir = "" }},
		{"journal inside state tree", func(c *LabValidatorConfig) { c.SigningJournalDir = filepath.Join(c.ObserverConfig.DataDir, "journal") }},
		{"state inside journal tree", func(c *LabValidatorConfig) { c.ObserverConfig.DataDir = filepath.Join(c.SigningJournalDir, "state") }},
		{"relative journal directory", func(c *LabValidatorConfig) { c.SigningJournalDir = "relative-journal" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := labConfig(t)
			tc.change(&cfg)
			if _, err := OpenLabValidator(cfg); !errors.Is(err, ErrLabValidatorConfig) {
				t.Fatalf("invalid validator configuration accepted: %v", err)
			}
			if _, err := os.Stat(cfg.ObserverConfig.DataDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected validator created state directory: %v", err)
			}
		})
	}
}

func TestProtectedLabValidatorRejectsSymlinkedJournalParent(t *testing.T) {
	cfg := labConfig(t)
	root := filepath.Dir(cfg.ObserverConfig.DataDir)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	cfg.SigningJournalDir = filepath.Join(alias, "signer")
	if _, err := OpenLabValidator(cfg); !errors.Is(err, ErrLabValidatorConfig) {
		t.Fatalf("symlinked signing custody ancestor admitted: %v", err)
	}
}
