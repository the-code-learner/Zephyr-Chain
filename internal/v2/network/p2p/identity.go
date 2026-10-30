package p2p

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	transportIdentityFile = "transport-identity.key"
	transportIdentityVersion uint16 = 1
	maxTransportIdentityBytes uint32 = 4096
)

var ErrTransportIdentity = errors.New("invalid or insecure persistent P2P transport identity")

// LoadOrCreateIdentity loads one durable Ed25519 libp2p identity, bound to
// a specific Zephyr NetworkID. It never silently regenerates an existing,
// malformed, cross-network, or improperly permissioned key.
//
// This provides stable peer addressing, NOT authority to submit compute jobs,
// act as a validator, or authenticate finalized chain state.
func LoadOrCreateIdentity(dir string, network types.NetworkID) (p2pcrypto.PrivKey, error) {
	if dir == "" || types.IsZero32([32]byte(network)) {
		return nil, ErrTransportIdentity
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTransportIdentity, err)
	}
	// Refuse symlinked ancestors rather than following an operator-supplied
	// path into an unrelated secret store.
	if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil && real != filepath.Dir(abs) {
		return nil, ErrTransportIdentity
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, ErrTransportIdentity
	}
	path := filepath.Join(abs, transportIdentityFile)
	if _, err := os.Lstat(path); err == nil {
		return readTransportIdentity(path, network)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	identity, _, err := p2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	raw, err := p2pcrypto.MarshalPrivateKey(identity)
	if err != nil || len(raw) == 0 || uint64(len(raw)) > uint64(maxTransportIdentityBytes) {
		return nil, ErrTransportIdentity
	}
	var writer codec.Writer
	writer.U16(transportIdentityVersion)
	writer.Fixed(network[:])
	writer.Bytes(raw)

	// O_EXCL prevents a concurrent process from replacing the first key.
	// The losing process must reopen the existing key (or fail closed if
	// the writer has not completed).
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return readTransportIdentity(path, network)
	}
	if err != nil {
		return nil, err
	}
	_, err = f.Write(writer.BytesCopy())
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		// Do not silently remove or replace a potentially damaged key.
		return nil, fmt.Errorf("%w: write failed: %v", ErrTransportIdentity, err)
	}
	if d, err := os.Open(abs); err == nil {
		if syncErr := d.Sync(); syncErr != nil {
			_ = d.Close()
			return nil, syncErr
		}
		_ = d.Close()
	}
	return identity, nil
}

func readTransportIdentity(path string, network types.NetworkID) (p2pcrypto.PrivKey, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 ||
		info.Size() <= 0 || info.Size() > int64(maxTransportIdentityBytes)+40 {
		return nil, ErrTransportIdentity
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := codec.NewReader(raw)
	version, err := r.U16()
	if err != nil || version != transportIdentityVersion {
		return nil, ErrTransportIdentity
	}
	networkRaw, err := r.Fixed(32)
	if err != nil {
		return nil, ErrTransportIdentity
	}
	var stored types.NetworkID
	copy(stored[:], networkRaw)
	if stored != network {
		return nil, ErrTransportIdentity
	}
	privateRaw, err := r.Bytes(maxTransportIdentityBytes)
	if err != nil || r.Done() != nil {
		return nil, ErrTransportIdentity
	}
	identity, err := p2pcrypto.UnmarshalPrivateKey(privateRaw)
	if err != nil || identity == nil || identity.Type() != p2pcrypto.Ed25519 {
		return nil, ErrTransportIdentity
	}
	return identity, nil
}
