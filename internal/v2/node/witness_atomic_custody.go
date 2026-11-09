package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
)

// WitnessAtomicCustodyAttestation binds the EXACT checkpoint signature to a
// challenged remote reservation. The custody authority's signature covers
// the witness public key, the witness signature, the checkpoint and revision.
// This does not prove that the backend is globally linearizable or that its
// keys are isolated: those properties require independent remote audit.
type WitnessAtomicCustodyAttestation struct {
	Checkpoint        FinalityCheckpoint
	Challenge         [32]byte
	Revision          uint64
	WitnessPublicKey  []byte
	WitnessSignature  []byte
	AuthoritySignature []byte
}

// WitnessAtomicCustodian is an external, independently operated key-custody
// boundary. A production implementation MUST atomically reserve monotonically
// and sign under one globally serialized authority; it MUST NOT return a
// previously generated witness signature with a newly fabricated grant.
// The node cannot access the witness or authority private keys.
type WitnessAtomicCustodian interface {
	ReserveAndSignWitnessCheckpoint(context.Context, FinalityCheckpoint, [32]byte) (WitnessAtomicCustodyAttestation, error)
}

// WitnessAtomicCustodyDigest is a domain-separated authority attestation over
// all custody evidence, including the precise detached witness signature.
// It deliberately excludes only AuthoritySignature itself.
func WitnessAtomicCustodyDigest(a WitnessAtomicCustodyAttestation) ([32]byte, error) {
	if a.Checkpoint.Validate(a.Checkpoint.Network, a.Checkpoint.ValidatorRoot) != nil ||
		a.Revision == 0 || isZeroWitnessFenceChallenge(a.Challenge) ||
		len(a.WitnessPublicKey) != ed25519.PublicKeySize ||
		len(a.WitnessSignature) != ed25519.SignatureSize {
		return [32]byte{}, ErrWitnessSigningIntent
	}
	var w codec.Writer
	w.Fixed(a.Checkpoint.Network[:])
	w.Fixed(a.Checkpoint.ValidatorRoot[:])
	w.U64(a.Checkpoint.Height)
	w.Fixed(a.Checkpoint.HeaderHash[:])
	w.Fixed(a.Challenge[:])
	w.U64(a.Revision)
	w.Fixed(a.WitnessPublicKey)
	w.Fixed(a.WitnessSignature)
	return codec.DomainHash("zephyr/witness-atomic-custody-attestation/v1", w.BytesCopy()), nil
}

// VerifyWitnessAtomicCustodyAttestation verifies BOTH independently pinned
// signatures and their cryptographic linkage to the same challenge, network,
// root, finalized height and header. The remote authority and witness keys
// MUST be different. This does not authenticate the original QC ancestry.
func VerifyWitnessAtomicCustodyAttestation(a WitnessAtomicCustodyAttestation, cp FinalityCheckpoint, challenge [32]byte, authorityKey, witnessKey []byte) error {
	if cp.Validate(cp.Network, cp.ValidatorRoot) != nil ||
		a.Checkpoint != cp || a.Challenge != challenge ||
		isZeroWitnessFenceChallenge(challenge) ||
		len(authorityKey) != ed25519.PublicKeySize ||
		len(witnessKey) != ed25519.PublicKeySize ||
		len(a.AuthoritySignature) != ed25519.SignatureSize ||
		bytes.Equal(authorityKey, witnessKey) ||
		!bytes.Equal(a.WitnessPublicKey, witnessKey) {
		return ErrWitnessSigningIntent
	}
	cpDigest, err := WitnessCheckpointDigest(cp)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(witnessKey), cpDigest[:], a.WitnessSignature) {
		return ErrWitnessSigningIntent
	}
	digest, err := WitnessAtomicCustodyDigest(a)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(authorityKey), digest[:], a.AuthoritySignature) {
		return ErrWitnessSigningIntent
	}
	return nil
}

// ReserveAndVerifyAtomicWitnessCheckpoint is a NON-SIGNING witness client:
// QC verification -> durable intent -> unpredictable challenge -> external
// custody request -> BOTH signature verifications -> QC recheck. It returns
// only the externally signed evidence, never a private signing capability.
// A successful response proves the provided signatures are valid, NOT remote
// atomicity, hardware custody, independent operation or network freshness.
func ReserveAndVerifyAtomicWitnessCheckpoint(ctx context.Context, j *WitnessSigningIntentJournal, cp FinalityCheckpoint, remote WitnessAtomicCustodian, authorityKey, witnessKey []byte) (WitnessAtomicCustodyAttestation, error) {
	fail := func(err error) (WitnessAtomicCustodyAttestation, error) {
		return WitnessAtomicCustodyAttestation{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	if ctx == nil || j == nil || remote == nil ||
		len(authorityKey) != ed25519.PublicKeySize || len(witnessKey) != ed25519.PublicKeySize ||
		bytes.Equal(authorityKey, witnessKey) {
		return fail(nil)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.store == nil || j.verifier == nil || ctx.Err() != nil {
		return fail(nil)
	}
	if err := j.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if err := j.store.advance(cp); err != nil {
		return fail(err)
	}
	current, err := j.store.Current()
	if err != nil || current != cp || ctx.Err() != nil {
		return fail(err)
	}
	var challenge [32]byte
	if _, err := rand.Read(challenge[:]); err != nil || isZeroWitnessFenceChallenge(challenge) {
		return fail(err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a, err := remote.ReserveAndSignWitnessCheckpoint(requestCtx, cp, challenge)
	if err != nil || requestCtx.Err() != nil {
		return fail(err)
	}
	if err := VerifyWitnessAtomicCustodyAttestation(a, cp, challenge, authorityKey, witnessKey); err != nil {
		return fail(err)
	}
	if err := j.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return fail(err)
	}
	if ctx.Err() != nil || requestCtx.Err() != nil {
		return fail(ctx.Err())
	}
	return a, nil
}
