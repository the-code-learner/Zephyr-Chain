// Package bootstrap exposes a strictly lab-scoped Protocol v2 validator
// bootstrap. This is not an independently audited production validator.
package bootstrap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"os"
	"path/filepath"
	"strings"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	v2node "github.com/zephyr-chain/zephyr-chain/internal/v2/node"
)

// ErrLabValidatorConfig includes invalid local key custody and an unpinned
// validator roster. The actual guard/observer errors remain inspectable.
var ErrLabValidatorConfig = errors.New("invalid protected v2 lab validator configuration")

type LabValidatorConfig struct {
	ObserverConfig Config
	// Key must come from a separately protected source, never the genesis
	// JSON or a command-line argument and never a checked-in test fixture.
	Key *ecdsa.PrivateKey
	// SigningJournalDir is an owner-only *separate* directory. All processes
	// sharing the key MUST share this journal and its OS locking boundary.
	SigningJournalDir string
}

type LabValidator struct {
	*Observer
	Guard *v2consensus.DurableSigningGuard
}

func (v *LabValidator) Close() error {
	if v == nil {
		return nil
	}
	return errors.Join(v.Observer.Close(), v.Guard.Close())
}

// OpenLabValidator shares the observer's pinned genesis, persisted state and
// QC-history recovery path, but replaces its non-voting P2P consensus handler
// with a protected signer backed by the durable signing-intent journal.
// No network service is exposed until both pins and signing custody pass.
func OpenLabValidator(cfg LabValidatorConfig) (*LabValidator, error) {
	if cfg.Key == nil || cfg.Key.Curve != elliptic.P256() ||
		cfg.Key.D == nil || cfg.Key.D.Sign() <= 0 ||
		cfg.Key.D.Cmp(elliptic.P256().Params().N) >= 0 ||
		!elliptic.P256().IsOnCurve(cfg.Key.PublicKey.X, cfg.Key.PublicKey.Y) {
		return nil, ErrLabValidatorConfig
	}
	x, y := elliptic.P256().ScalarBaseMult(cfg.Key.D.Bytes())
	if x.Cmp(cfg.Key.PublicKey.X) != 0 || y.Cmp(cfg.Key.PublicKey.Y) != 0 {
		return nil, ErrLabValidatorConfig
	}
	if cfg.SigningJournalDir == "" || cfg.ObserverConfig.DataDir == "" ||
		!isolatedLabCustodyPaths(cfg.ObserverConfig.DataDir, cfg.SigningJournalDir) {
		return nil, ErrLabValidatorConfig
	}
	anchor, err := cfg.ObserverConfig.Genesis.TrustAnchor()
	if err != nil || anchor.Network != cfg.ObserverConfig.ExpectedNetwork ||
		anchor.ValidatorRoot != cfg.ObserverConfig.ExpectedValidatorRoot {
		return nil, ErrLabValidatorConfig
	}
	set, err := cfg.ObserverConfig.Genesis.ValidatorSet()
	if err != nil {
		return nil, ErrLabValidatorConfig
	}
	pub := elliptic.Marshal(elliptic.P256(), x, y)
	id, err := v2consensus.SigningValidatorID(pub)
	if err != nil {
		return nil, ErrLabValidatorConfig
	}
	found := false
	for _, validator := range set.Validators {
		if validator.ID == id {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrLabValidatorConfig
	}
	// The guard is locked before the observer allocates state or starts a
	// listener, so a concurrent key holder cannot cause a partial session.
	guard, err := v2consensus.OpenDurableSigningGuard(cfg.SigningJournalDir, anchor.Network, id)
	if err != nil {
		return nil, err
	}
	obs, err := OpenObserver(cfg.ObserverConfig)
	if err != nil {
		return nil, errors.Join(err, guard.Close())
	}
	protected, err := v2node.NewProtectedService(obs.Runtime, set, cfg.Key, obs.Transport, guard)
	if err != nil {
		return nil, errors.Join(err, obs.Close(), guard.Close())
	}
	obs.Service = protected
	return &LabValidator{Observer: obs, Guard: guard}, nil
}

func isolatedLabCustodyPaths(dataDir, signingDir string) bool {
	if !filepath.IsAbs(dataDir) || !filepath.IsAbs(signingDir) ||
		filepath.Clean(dataDir) != dataDir || filepath.Clean(signingDir) != signingDir {
		return false
	}
	// Parent symlinks could otherwise alias an apparently separate tree.
	for _, path := range []string{dataDir, signingDir} {
		parent := filepath.Dir(path)
		resolved, err := filepath.EvalSymlinks(parent)
		if err != nil || resolved != parent {
			return false
		}
		if info, err := os.Lstat(path); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	rel, err := filepath.Rel(dataDir, signingDir)
	if err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return false
	}
	rel, err = filepath.Rel(signingDir, dataDir)
	return err == nil && rel != "." &&
		(rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
