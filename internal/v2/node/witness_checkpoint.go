package node

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	maxWitnessBundleBytes = 16 << 10
	minWitnessKeys        = 3
	maxWitnessKeys        = 16
)

var ErrWitnessCheckpoint = errors.New("invalid, untrusted or insufficiently signed external finality checkpoint")

// WitnessSignature is a detached signature from an independently pinned
// Ed25519 key. Witnesses attest a specific FINALIZED height and consensus
// header hash, not the local state directory or a peer RPC response.
type WitnessSignature struct {
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
}

// SignedWitnessCheckpoint is a bounded canonical JSON interchange envelope.
// Public keys in this envelope NEVER authorize themselves; policy keys must
// come from an independently authenticated, operator-pinned trust channel.
type SignedWitnessCheckpoint struct {
	NetworkID     string             `json:"networkId"`
	ValidatorRoot string             `json:"validatorRoot"`
	Height        uint64             `json:"height"`
	HeaderHash    string             `json:"headerHash"`
	Signatures    []WitnessSignature `json:"signatures"`
}

// WitnessCheckpointDigest is the exact domain-separated message that remote
// witness signers must sign using Ed25519. The hash binds network and original
// validator root to prevent reuse across chains, and has no timestamp or
// freshness claim.
func WitnessCheckpointDigest(checkpoint FinalityCheckpoint) ([32]byte, error) {
	if err := checkpoint.Validate(checkpoint.Network, checkpoint.ValidatorRoot); err != nil {
		return [32]byte{}, ErrWitnessCheckpoint
	}
	var writer codec.Writer
	writer.Fixed(checkpoint.Network[:])
	writer.Fixed(checkpoint.ValidatorRoot[:])
	writer.U64(checkpoint.Height)
	writer.Fixed(checkpoint.HeaderHash[:])
	return codec.DomainHash("zephyr/external-finalized-witness-checkpoint/v1", writer.BytesCopy()), nil
}

// ParseSignedWitnessCheckpoint verifies a threshold of >2/3 of at least
// three *independently pinned* distinct witness keys. Every provided
// signature must be valid: bad/foreign/duplicate signers fail closed even
// if the valid signatures would separately exceed the threshold.
//
// This proves signature provenance, NOT remote witness independence,
// freshness, live reachability, QC correctness or monotonic storage. The
// local certified-history replay checks the independently signed ancestor
// against actual genesis-anchored validator QCs before any WAL recovery.
func ParseSignedWitnessCheckpoint(data []byte, trustedKeys [][]byte, network types.NetworkID, validatorRoot types.Hash) (*FinalityCheckpoint, error) {
	if len(data) == 0 || len(data) > maxWitnessBundleBytes ||
		len(trustedKeys) < minWitnessKeys || len(trustedKeys) > maxWitnessKeys ||
		types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(validatorRoot)) {
		return nil, ErrWitnessCheckpoint
	}
	allowed := make(map[string]ed25519.PublicKey, len(trustedKeys))
	for _, raw := range trustedKeys {
		if len(raw) != ed25519.PublicKeySize {
			return nil, ErrWitnessCheckpoint
		}
		key := hex.EncodeToString(raw)
		if _, exists := allowed[key]; exists {
			return nil, ErrWitnessCheckpoint
		}
		allowed[key] = ed25519.PublicKey(raw)
	}
	var envelope SignedWitnessCheckpoint
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, ErrWitnessCheckpoint
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, ErrWitnessCheckpoint
	}
	// Exactly one compact, canonically serialized envelope: no duplicate
	// JSON members or alternative parser interpretations, even when a
	// secondary implementation is less strict about duplicate keys.
	canonical, err := json.Marshal(envelope)
	if err != nil || !bytes.Equal(bytes.TrimSpace(data), canonical) {
		return nil, ErrWitnessCheckpoint
	}
	if envelope.NetworkID != hex.EncodeToString(network[:]) ||
		envelope.ValidatorRoot != hex.EncodeToString(validatorRoot[:]) ||
		len(envelope.HeaderHash) != 64 || envelope.Height == 0 {
		return nil, ErrWitnessCheckpoint
	}
	headerBytes, err := hex.DecodeString(envelope.HeaderHash)
	if err != nil || len(headerBytes) != 32 || envelope.HeaderHash != hex.EncodeToString(headerBytes) {
		return nil, ErrWitnessCheckpoint
	}
	var header types.Hash
	copy(header[:], headerBytes)
	checkpoint := FinalityCheckpoint{Network: network, ValidatorRoot: validatorRoot, Height: envelope.Height, HeaderHash: header}
	digest, err := WitnessCheckpointDigest(checkpoint)
	if err != nil {
		return nil, ErrWitnessCheckpoint
	}
	threshold := (2*len(allowed))/3 + 1
	if len(envelope.Signatures) < threshold || len(envelope.Signatures) > len(allowed) {
		return nil, ErrWitnessCheckpoint
	}
	seen := make(map[string]struct{}, len(envelope.Signatures))
	previous := ""
	for _, signature := range envelope.Signatures {
		pub, exists := allowed[signature.PublicKey]
		if !exists || len(signature.PublicKey) != ed25519.PublicKeySize*2 ||
			(signature.PublicKey <= previous && previous != "") {
			return nil, ErrWitnessCheckpoint
		}
		if _, duplicate := seen[signature.PublicKey]; duplicate {
			return nil, ErrWitnessCheckpoint
		}
		seen[signature.PublicKey] = struct{}{}
		previous = signature.PublicKey
		raw, err := hex.DecodeString(signature.Signature)
		if err != nil || len(raw) != ed25519.SignatureSize ||
			signature.Signature != hex.EncodeToString(raw) ||
			!ed25519.Verify(pub, digest[:], raw) {
			return nil, ErrWitnessCheckpoint
		}
	}
	return &checkpoint, nil
}
