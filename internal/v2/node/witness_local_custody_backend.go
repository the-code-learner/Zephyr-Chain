package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// WitnessCustodySigningProvider is an isolated signing boundary. Its keys
// must never be returned to the custody adapter or requesting node.
// Implementations are responsible for protecting the signing keys.
type WitnessCustodySigningProvider interface {
	SignWitnessCheckpointDigest(context.Context, [32]byte) ([]byte, error)
	SignCustodyAttestationDigest(context.Context, [32]byte) ([]byte, error)
}

// LocalWitnessCustodyBackend is a deliberately LOCAL, single-writer reference
// implementation of WitnessAtomicCustodian. Its fsynced file can be rolled
// back as a whole. It does NOT provide a global HSM counter, quorum lease,
// geographically independent custody or mainnet-grade antirollback.
// Only a separately audited monotonic signing authority can provide those.
type LocalWitnessCustodyBackend struct {
	mu           sync.Mutex
	store        *FinalityAnchorStore
	verifier     WitnessFinalityVerifier
	signer       WitnessCustodySigningProvider
	witnessKey   ed25519.PublicKey
	authorityKey ed25519.PublicKey
	closed       bool
}

// OpenLocalWitnessCustodyBackend keeps signing keys outside the node's
// process-facing interface, and pins separate, fixed public identities.
// create=true is intended only for initial explicit provisioning. Restarts
// must use create=false and the same exclusive durable anchor path.
func OpenLocalWitnessCustodyBackend(path string, network types.NetworkID, root types.Hash, create bool, verifier WitnessFinalityVerifier, signer WitnessCustodySigningProvider, witnessKey, authorityKey []byte) (*LocalWitnessCustodyBackend, error) {
	if verifier == nil || signer == nil ||
		len(witnessKey) != ed25519.PublicKeySize || len(authorityKey) != ed25519.PublicKeySize ||
		bytes.Equal(witnessKey, authorityKey) {
		return nil, ErrWitnessSigningIntent
	}
	store, err := OpenFinalityAnchorStore(path, network, root, create)
	if err != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, err)
	}
	return &LocalWitnessCustodyBackend{
		store: store, verifier: verifier, signer: signer,
		witnessKey:   append(ed25519.PublicKey(nil), witnessKey...),
		authorityKey: append(ed25519.PublicKey(nil), authorityKey...),
	}, nil
}

// ReserveAndSignWitnessCheckpoint serializes independent QC verification,
// durable monotonic reservation, and BOTH signing-provider calls. A failed
// signing call never rolls back the already-fsynced reservation; retries of
// an identical checkpoint remain idempotent. Every returned signature is
// verified against the two pins before evidence leaves this boundary.
func (b *LocalWitnessCustodyBackend) ReserveAndSignWitnessCheckpoint(ctx context.Context, cp FinalityCheckpoint, challenge [32]byte) (WitnessAtomicCustodyAttestation, error) {
	fail := func(err error) (WitnessAtomicCustodyAttestation, error) {
		return WitnessAtomicCustodyAttestation{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	if b == nil || ctx == nil || ctx.Err() != nil || isZeroWitnessFenceChallenge(challenge) {
		return fail(nil)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.store == nil || b.verifier == nil || b.signer == nil || ctx.Err() != nil {
		return fail(nil)
	}
	if err := b.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if err := b.store.advance(cp); err != nil {
		return fail(err)
	}
	current, err := b.store.Current()
	if err != nil || current != cp || ctx.Err() != nil || b.store.count == 0 {
		return fail(err)
	}
	digest, err := WitnessCheckpointDigest(cp)
	if err != nil {
		return fail(err)
	}
	sigWitness, err := b.signer.SignWitnessCheckpointDigest(ctx, digest)
	if err != nil || ctx.Err() != nil || !ed25519.Verify(b.witnessKey, digest[:], sigWitness) {
		return fail(err)
	}
	a := WitnessAtomicCustodyAttestation{
		Checkpoint: cp, Challenge: challenge, Revision: uint64(b.store.count),
		WitnessPublicKey: append([]byte(nil), b.witnessKey...),
		WitnessSignature: append([]byte(nil), sigWitness...),
	}
	attestationDigest, err := WitnessAtomicCustodyDigest(a)
	if err != nil {
		return fail(err)
	}
	sigAuthority, err := b.signer.SignCustodyAttestationDigest(ctx, attestationDigest)
	if err != nil || ctx.Err() != nil {
		return fail(err)
	}
	a.AuthoritySignature = append([]byte(nil), sigAuthority...)
	if err := VerifyWitnessAtomicCustodyAttestation(a, cp, challenge, b.authorityKey, b.witnessKey); err != nil {
		return fail(err)
	}
	if err := b.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	return a, nil
}

// Current returns the fsynced local high-water mark; it is NOT an
// externally authenticated nonrollbackable proof.
func (b *LocalWitnessCustodyBackend) Current() (FinalityCheckpoint, error) {
	if b == nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.store == nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	return b.store.Current()
}

func (b *LocalWitnessCustodyBackend) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	return b.store.Close()
}
