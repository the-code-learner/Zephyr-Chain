package node

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
)

// WitnessFencingGrant is a challenge-bound remote reservation receipt.
// The authority key MUST be independently pinned and distinct from the
// checkpoint quorum keys. A signed receipt attests what its authority
// asserted; it does NOT itself prove atomic storage or grant permission
// to sign with a witness private key.
type WitnessFencingGrant struct {
	Checkpoint FinalityCheckpoint
	Challenge  [32]byte
	Revision   uint64
	Signature  []byte
}

// WitnessRemoteFenceAuthority is the boundary for a remotely operated,
// globally linearizable checkpoint reservation authority. Implementations
// MUST atomically reject lower heights and conflicting same-height hashes
// across hosts, crashes, old backups and signing key migrations. They must
// bind the challenge, checkpoint and monotonic revision when signing grants.
//
// This interface does not implement that remote durability/fencing property.
// No witness key or signing operation is exposed through this API.
type WitnessRemoteFenceAuthority interface {
	ReserveWitnessFence(context.Context, FinalityCheckpoint, [32]byte) (WitnessFencingGrant, error)
}

// WitnessFencingGrantDigest defines a domain-separated v1 attestation over
// chain identity, finalized checkpoint, unpredictable request challenge and
// remote revision. The digest is public and carries NO signing capability.
func WitnessFencingGrantDigest(grant WitnessFencingGrant) ([32]byte, error) {
	if grant.Checkpoint.Validate(grant.Checkpoint.Network, grant.Checkpoint.ValidatorRoot) != nil ||
		grant.Revision == 0 || isZeroWitnessFenceChallenge(grant.Challenge) {
		return [32]byte{}, ErrWitnessSigningIntent
	}
	var w codec.Writer
	w.Fixed(grant.Checkpoint.Network[:])
	w.Fixed(grant.Checkpoint.ValidatorRoot[:])
	w.U64(grant.Checkpoint.Height)
	w.Fixed(grant.Checkpoint.HeaderHash[:])
	w.Fixed(grant.Challenge[:])
	w.U64(grant.Revision)
	return codec.DomainHash("zephyr/witness-remote-fencing-grant/v1", w.BytesCopy()), nil
}

func isZeroWitnessFenceChallenge(challenge [32]byte) bool {
	return challenge == [32]byte{}
}

// VerifyWitnessFencingGrant binds the remote acknowledgement to this exact
// QC checkpoint and freshly generated challenge under a pinned authority key.
// A replayed grant from an earlier request fails even if its checkpoint and
// remote revision match. A response cannot authorize any private-key use.
func VerifyWitnessFencingGrant(grant WitnessFencingGrant, checkpoint FinalityCheckpoint, challenge [32]byte, pinnedAuthorityKey []byte) error {
	if len(pinnedAuthorityKey) != ed25519.PublicKeySize ||
		len(grant.Signature) != ed25519.SignatureSize ||
		checkpoint.Validate(checkpoint.Network, checkpoint.ValidatorRoot) != nil ||
		grant.Checkpoint != checkpoint || grant.Challenge != challenge ||
		isZeroWitnessFenceChallenge(challenge) {
		return ErrWitnessSigningIntent
	}
	digest, err := WitnessFencingGrantDigest(grant)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pinnedAuthorityKey), digest[:], grant.Signature) {
		return ErrWitnessSigningIntent
	}
	return nil
}

// ReserveAndVerifyWitnessFence is a NON-SIGNING, fail-closed pre-sign guard.
// It first verifies independently QC-finalized history, fsyncs the local
// intent, and only then challenges the remote fencing authority. The grant
// must be signed by the independently pinned authority and echo the exact
// unpredictable request challenge. Finality is re-verified after remote IO.
//
// A return of nil is NOT permission to use a witness signing key. Production
// signing requires the separate private-key custodian to enforce reservation
// and signing atomically under the SAME remotely linearizable authority;
// otherwise even a valid acknowledgement can race another host's signer.
func ReserveAndVerifyWitnessFence(ctx context.Context, j *WitnessSigningIntentJournal, checkpoint FinalityCheckpoint, remote WitnessRemoteFenceAuthority, pinnedAuthorityKey []byte) error {
	if ctx == nil || j == nil || remote == nil || len(pinnedAuthorityKey) != ed25519.PublicKeySize {
		return ErrWitnessSigningIntent
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.store == nil || j.verifier == nil || ctx.Err() != nil {
		return ErrWitnessSigningIntent
	}
	if err := j.verifier.VerifyFinalizedCheckpoint(checkpoint); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	if err := j.store.advance(checkpoint); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	current, err := j.store.Current()
	if err != nil || current != checkpoint {
		return ErrWitnessSigningIntent
	}
	var challenge [32]byte
	if _, err := rand.Read(challenge[:]); err != nil || isZeroWitnessFenceChallenge(challenge) {
		return ErrWitnessSigningIntent
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	grant, err := remote.ReserveWitnessFence(requestCtx, checkpoint, challenge)
	if err != nil || requestCtx.Err() != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	if err := VerifyWitnessFencingGrant(grant, checkpoint, challenge, pinnedAuthorityKey); err != nil {
		return err
	}
	if err := j.verifier.VerifyFinalizedCheckpoint(checkpoint); err != nil {
		return errors.Join(ErrWitnessSigningIntent, err)
	}
	// A verifier may perform blocking IO or cancel the parent context while
	// checking finality; never report a successful reservation after that.
	if ctx.Err() != nil || requestCtx.Err() != nil {
		return ErrWitnessSigningIntent
	}
	return nil
}
