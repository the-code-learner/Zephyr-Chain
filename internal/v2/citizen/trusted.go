package citizen

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/state"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrTrustedBundle = errors.New("Citizen bundle is not anchored to trusted finalized state")

type TrustAnchor struct {
	Network       types.NetworkID
	ValidatorRoot types.Hash
}

type ValidatorDTO struct {
	ID        string `json:"id"`
	PublicKey []byte `json:"publicKey"`
	Power     string `json:"power"`
}

type ObjectBundle struct {
	Network         string         `json:"network"`
	Height          uint64         `json:"height"`
	ShardID         uint32         `json:"shardId"`
	Header          []byte         `json:"header"`
	Certificate     []byte         `json:"certificate"`
	Commitment      []byte         `json:"commitment"`
	CommitmentProof []byte         `json:"commitmentProof"`
	ObjectID        string         `json:"objectId"`
	ObjectPresent   bool           `json:"objectPresent"`
	Object          []byte         `json:"object,omitempty"`
	StateProof      []byte         `json:"stateProof"`
	Validators      []ValidatorDTO `json:"validators"`
}

type VerifiedObject struct {
	Network    types.NetworkID
	Height     uint64
	ShardID    uint32
	ObjectID   types.ObjectID
	Present    bool
	Object     object.Object
	StateRoot  types.Hash
	NextAnchor TrustAnchor
}

func VerifyObjectBundleJSON(data []byte, anchor TrustAnchor) (VerifiedObject, error) {
	var bundle ObjectBundle
	if len(data) == 0 || json.Unmarshal(data, &bundle) != nil {
		return VerifiedObject{}, ErrTrustedBundle
	}
	return VerifyObjectBundle(bundle, anchor)
}

func VerifyObjectBundle(bundle ObjectBundle, anchor TrustAnchor) (VerifiedObject, error) {
	if types.IsZero32([32]byte(anchor.Network)) || types.IsZero32([32]byte(anchor.ValidatorRoot)) {
		return VerifiedObject{}, ErrTrustedBundle
	}
	header, err := sharding.ParseGlobalHeader(bundle.Header)
	if err != nil || header.Network != anchor.Network || header.ValidatorRoot != anchor.ValidatorRoot || header.Height != bundle.Height || bundle.Network != header.Network.String() {
		return VerifiedObject{}, ErrTrustedBundle
	}
	validators, err := validatorSetFromDTO(header.Network, bundle.Validators)
	if err != nil {
		return VerifiedObject{}, ErrTrustedBundle
	}
	validatorRoot, err := validators.Root()
	if err != nil || validatorRoot != header.ValidatorRoot {
		return VerifiedObject{}, ErrTrustedBundle
	}
	certificate, err := v2consensus.ParseCertificate(bundle.Certificate)
	if err != nil || certificate.Network != header.Network || certificate.Height != header.Height || certificate.HeaderHash != v2consensus.HeaderConsensusHash(header) || header.CertificateHash != certificate.Hash() {
		return VerifiedObject{}, ErrTrustedBundle
	}
	if err := validators.VerifyCertificate(certificate); err != nil {
		return VerifiedObject{}, ErrTrustedBundle
	}
	commitment, err := sharding.ParseCommitment(bundle.Commitment)
	if err != nil || commitment.ShardID != bundle.ShardID {
		return VerifiedObject{}, ErrTrustedBundle
	}
	commitmentProof, err := merkle.ParseProof(bundle.CommitmentProof)
	if err != nil || !merkle.Verify(header.ShardCommitmentRoot, commitment.Hash(), commitmentProof) {
		return VerifiedObject{}, ErrTrustedBundle
	}
	objectRaw, err := hex.DecodeString(bundle.ObjectID)
	if err != nil || len(objectRaw) != 32 {
		return VerifiedObject{}, ErrTrustedBundle
	}
	var objectID types.ObjectID
	copy(objectID[:], objectRaw)
	proof, err := state.ParseProof(bundle.StateProof)
	if err != nil || proof.Exists != bundle.ObjectPresent {
		return VerifiedObject{}, ErrTrustedBundle
	}
	verified := VerifiedObject{Network: header.Network, Height: header.Height, ShardID: bundle.ShardID, ObjectID: objectID, Present: bundle.ObjectPresent, StateRoot: commitment.StateRoot, NextAnchor: TrustAnchor{Network: header.Network, ValidatorRoot: header.EffectiveNextValidatorRoot()}}
	if bundle.ObjectPresent {
		obj, err := object.ParseObject(bundle.Object)
		if err != nil || obj.ID != objectID {
			return VerifiedObject{}, ErrTrustedBundle
		}
		hash := obj.Hash()
		if !state.Verify(commitment.StateRoot, types.Hash(objectID), hash[:], proof) {
			return VerifiedObject{}, ErrTrustedBundle
		}
		verified.Object = obj
	} else if len(bundle.Object) != 0 || !state.Verify(commitment.StateRoot, types.Hash(objectID), nil, proof) {
		return VerifiedObject{}, ErrTrustedBundle
	}
	return verified, nil
}

func validatorSetFromDTO(network types.NetworkID, values []ValidatorDTO) (v2consensus.ValidatorSet, error) {
	if len(values) == 0 || len(values) > v2consensus.MaxCertificateVotes {
		return v2consensus.ValidatorSet{}, ErrTrustedBundle
	}
	set := v2consensus.ValidatorSet{Network: network, Validators: make([]v2consensus.Validator, len(values))}
	seen := make(map[types.ValidatorID]struct{}, len(values))
	for i, value := range values {
		idRaw, err := hex.DecodeString(value.ID)
		if err != nil || len(idRaw) != 32 {
			return v2consensus.ValidatorSet{}, ErrTrustedBundle
		}
		var id types.ValidatorID
		copy(id[:], idRaw)
		if types.ValidatorIDFromPublicKey(value.PublicKey) != id {
			return v2consensus.ValidatorSet{}, ErrTrustedBundle
		}
		if _, duplicate := seen[id]; duplicate {
			return v2consensus.ValidatorSet{}, ErrTrustedBundle
		}
		seen[id] = struct{}{}
		power, err := strconv.ParseUint(value.Power, 10, 64)
		if err != nil || power == 0 {
			return v2consensus.ValidatorSet{}, ErrTrustedBundle
		}
		set.Validators[i] = v2consensus.Validator{ID: id, PublicKey: append([]byte(nil), value.PublicKey...), Power: power}
	}
	if err := set.Validate(); err != nil {
		return v2consensus.ValidatorSet{}, ErrTrustedBundle
	}
	return set, nil
}
