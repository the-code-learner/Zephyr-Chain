package node

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const minWitnessOperators = 3

// WitnessOperatorKeyBinding is independently provisioned operator ownership
// metadata for a trusted witness public key. Ownership labels are NOT
// inferred from the untrusted signed checkpoint or its publisher. A binding
// is only meaningful if operator identity/key custody has been independently
// verified out of band during an audited genesis/key-rotation ceremony.
type WitnessOperatorKeyBinding struct {
	OperatorID string
	PublicKey  []byte
}

// OperatorDiverseWitnessFloor additionally requires a quorum of DISTINCT
// provisioned operator identities in every accepted signed checkpoint.
// Standard strict >2/3 Ed25519 key verification remains mandatory.
//
// This is an optional stricter recovery policy, NOT evidence of separate
// physical hosts, key-holding institutions, geographic failure domains,
// HSM antirollback, remote publication freshness, or an actual ceremony.
type OperatorDiverseWitnessFloor struct {
	source        SignedWitnessFloorSource
	trustedKeys   [][]byte
	operators     map[string]string
	network       types.NetworkID
	validatorRoot types.Hash
}

func validWitnessOperatorID(id string) bool {
	if len(id) == 0 || len(id) > 64 || id[0] == '-' || id[len(id)-1] == '-' {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// NewOperatorDiverseWitnessFloor pins the ENTIRE witness committee and each
// key's operator ID. At least four distinct keys with >=3 operator identities
// are required, with exactly one owner per key. The caller MUST obtain this
// mapping through a separately authenticated operator ceremony; accepting a
// mapping asserted by the witness HTTP response defeats the security model.
func NewOperatorDiverseWitnessFloor(source SignedWitnessFloorSource, bindings []WitnessOperatorKeyBinding, network types.NetworkID, root types.Hash) (*OperatorDiverseWitnessFloor, error) {
	if source == nil || len(bindings) < 4 || len(bindings) > maxWitnessKeys {
		return nil, ErrWitnessSigningIntent
	}
	keys := make([][]byte, 0, len(bindings))
	operators := make(map[string]string, len(bindings))
	distinctOperators := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if !validWitnessOperatorID(binding.OperatorID) || len(binding.PublicKey) != ed25519.PublicKeySize {
			return nil, ErrWitnessSigningIntent
		}
		id := hex.EncodeToString(binding.PublicKey)
		if _, duplicate := operators[id]; duplicate {
			return nil, ErrWitnessSigningIntent
		}
		operators[id] = binding.OperatorID
		distinctOperators[binding.OperatorID] = struct{}{}
		keys = append(keys, append([]byte(nil), binding.PublicKey...))
	}
	if len(distinctOperators) < minWitnessOperators {
		return nil, ErrWitnessSigningIntent
	}
	base, err := NewQuorumAuthenticatedWitnessFloor(source, keys, network, root)
	if err != nil {
		return nil, err
	}
	return &OperatorDiverseWitnessFloor{
		source:        source,
		trustedKeys:   base.trustedKeys,
		operators:     operators,
		network:       network,
		validatorRoot: root,
	}, nil
}

// AuthenticatedWitnessFloor verifies canonical signed bytes against the
// independently pinned >2/3 KEY quorum, then additionally checks at least
// three DISTINCT operator owners actually contributed signatures. Two keys
// from one owner can never substitute for a third independent operator.
func (f *OperatorDiverseWitnessFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	if f == nil || f.source == nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	raw, err := f.source.SignedWitnessFloor()
	if err != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	checkpoint, err := ParseSignedWitnessCheckpoint(raw, f.trustedKeys, f.network, f.validatorRoot)
	if err != nil {
		return FinalityCheckpoint{}, errors.Join(ErrWitnessSigningIntent, ErrWitnessCheckpoint)
	}
	// The strict parser above has already proven this exact JSON byte
	// envelope canonical and all its Ed25519 signatures individually valid.
	var envelope SignedWitnessCheckpoint
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	present := make(map[string]struct{}, len(envelope.Signatures))
	for _, signature := range envelope.Signatures {
		owner, ok := f.operators[signature.PublicKey]
		if !ok {
			return FinalityCheckpoint{}, ErrWitnessSigningIntent
		}
		present[owner] = struct{}{}
	}
	if len(present) < minWitnessOperators {
		return FinalityCheckpoint{}, ErrWitnessSigningIntent
	}
	return *checkpoint, nil
}
