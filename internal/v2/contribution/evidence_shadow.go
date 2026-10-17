package contribution

import (
	"bytes"
	"errors"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/citizen"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/devicecredential"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const ShadowEvidenceVersion uint16 = 1

var ErrShadowEvidence = errors.New("invalid contribution shadow evidence")

// ShadowEvidenceEnvelope is a manifest of independently interpretable evidence
// classes. It deliberately has no scalar score, weights, ranking or authority.
//
// EconomicEvidenceHash commits to finalized-evidence ZECI shadow telemetry.
// CitizenKeyControlRoot commits separately to device-key-control observations.
// Keeping these domains separate prevents a successful device challenge from
// being silently reclassified as economic contribution.
type ShadowEvidenceEnvelope struct {
	Version                    uint16
	Shadow                     bool
	Network                    types.NetworkID
	EconomicEvidenceAvailable  bool
	EconomicEpoch              uint64
	EconomicEvidenceHash       types.Hash
	CitizenKeyControlAvailable bool
	CitizenKeyControlCount     uint32
	CitizenKeyControlRoot      types.Hash
}

func (e ShadowEvidenceEnvelope) Validate() error {
	if e.Version != ShadowEvidenceVersion || !e.Shadow || types.IsZero32([32]byte(e.Network)) {
		return ErrShadowEvidence
	}
	if !e.EconomicEvidenceAvailable {
		if e.EconomicEpoch != 0 || !types.IsZero32([32]byte(e.EconomicEvidenceHash)) {
			return ErrShadowEvidence
		}
	} else if e.EconomicEpoch == 0 || types.IsZero32([32]byte(e.EconomicEvidenceHash)) {
		return ErrShadowEvidence
	}
	if !e.CitizenKeyControlAvailable {
		if e.CitizenKeyControlCount != 0 || !types.IsZero32([32]byte(e.CitizenKeyControlRoot)) {
			return ErrShadowEvidence
		}
	} else if e.CitizenKeyControlCount == 0 || types.IsZero32([32]byte(e.CitizenKeyControlRoot)) {
		return ErrShadowEvidence
	}
	if !e.EconomicEvidenceAvailable && !e.CitizenKeyControlAvailable {
		return ErrShadowEvidence
	}
	return nil
}

func (e ShadowEvidenceEnvelope) CanonicalBytes() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(e.Version)
	w.Bool(e.Shadow)
	w.Fixed(e.Network[:])
	w.Bool(e.EconomicEvidenceAvailable)
	w.U64(e.EconomicEpoch)
	w.Fixed(e.EconomicEvidenceHash[:])
	w.Bool(e.CitizenKeyControlAvailable)
	w.U32(e.CitizenKeyControlCount)
	w.Fixed(e.CitizenKeyControlRoot[:])
	return w.BytesCopy(), nil
}

func (e ShadowEvidenceEnvelope) Hash() (types.Hash, error) {
	raw, err := e.CanonicalBytes()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/contribution-evidence-shadow/v1", raw)), nil
}

// CitizenKeyControlEvidence is a canonical commitment to facts already
// established by Citizen challenge verification. CredentialHeight is the
// finalized credential proof height; it is NOT the time at which the response
// was produced.
type CitizenKeyControlEvidence struct {
	Network              types.NetworkID
	Owner                types.AccountID
	CredentialObject     types.ObjectID
	CredentialSequence   uint64
	CredentialHeight     uint64
	CredentialStateRoot  types.Hash
	RequiredCapabilities uint32
	ChallengeHash        types.Hash
	ContextHash          types.Hash
}

func (e CitizenKeyControlEvidence) Validate() error {
	if types.IsZero32([32]byte(e.Network)) ||
		types.IsZero32([32]byte(e.Owner)) ||
		types.IsZero32([32]byte(e.CredentialObject)) ||
		e.CredentialSequence == 0 ||
		e.CredentialHeight == 0 ||
		types.IsZero32([32]byte(e.CredentialStateRoot)) ||
		e.RequiredCapabilities == 0 ||
		e.RequiredCapabilities&^devicecredential.SupportedCapabilities != 0 ||
		types.IsZero32([32]byte(e.ChallengeHash)) ||
		types.IsZero32([32]byte(e.ContextHash)) {
		return ErrShadowEvidence
	}
	return nil
}

func (e CitizenKeyControlEvidence) CanonicalBytes() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Fixed(e.Network[:])
	w.Fixed(e.Owner[:])
	w.Fixed(e.CredentialObject[:])
	w.U64(e.CredentialSequence)
	w.U64(e.CredentialHeight)
	w.Fixed(e.CredentialStateRoot[:])
	w.U32(e.RequiredCapabilities)
	w.Fixed(e.ChallengeHash[:])
	w.Fixed(e.ContextHash[:])
	return w.BytesCopy(), nil
}

func CitizenKeyControlFromVerified(e citizen.VerifiedDeviceChallengeEvidence) (CitizenKeyControlEvidence, error) {
	out := CitizenKeyControlEvidence{
		Network:              e.Network,
		Owner:                e.Owner,
		CredentialObject:     e.CredentialObject,
		CredentialSequence:   e.CredentialSequence,
		CredentialHeight:     e.CredentialHeight,
		CredentialStateRoot:  e.CredentialStateRoot,
		RequiredCapabilities: e.RequiredCapabilities,
		ChallengeHash:        e.ChallengeHash,
		ContextHash:          e.ContextHash,
	}
	if err := out.Validate(); err != nil {
		return CitizenKeyControlEvidence{}, err
	}
	return out, nil
}

// BuildShadowEvidenceEnvelope builds a canonical evidence manifest without
// aggregating evidence classes into a score.
//
// zeci may be nil to represent unavailable economic evidence. Citizen evidence
// may be empty to represent unavailable key-control evidence. At least one
// evidence class must be available. Any Citizen entry from another network is
// rejected rather than silently mixed into this network-scoped manifest.
func BuildShadowEvidenceEnvelope(
	network types.NetworkID,
	zeci *economics.ZECIShadowTelemetry,
	citizenEvidence []citizen.VerifiedDeviceChallengeEvidence,
) (ShadowEvidenceEnvelope, error) {
	if types.IsZero32([32]byte(network)) {
		return ShadowEvidenceEnvelope{}, ErrShadowEvidence
	}

	out := ShadowEvidenceEnvelope{
		Version: ShadowEvidenceVersion,
		Shadow:  true,
		Network: network,
	}

	if zeci != nil {
		if err := zeci.Validate(); err != nil || !zeci.Shadow {
			return ShadowEvidenceEnvelope{}, ErrShadowEvidence
		}
		hash, err := zeci.Hash()
		if err != nil || types.IsZero32([32]byte(hash)) {
			return ShadowEvidenceEnvelope{}, ErrShadowEvidence
		}
		out.EconomicEvidenceAvailable = true
		out.EconomicEpoch = zeci.Epoch
		out.EconomicEvidenceHash = hash
	}

	if len(citizenEvidence) > 0 {
		if uint64(len(citizenEvidence)) > uint64(^uint32(0)) {
			return ShadowEvidenceEnvelope{}, ErrShadowEvidence
		}
		root, err := citizenKeyControlRoot(network, citizenEvidence)
		if err != nil {
			return ShadowEvidenceEnvelope{}, err
		}
		out.CitizenKeyControlAvailable = true
		out.CitizenKeyControlCount = uint32(len(citizenEvidence))
		out.CitizenKeyControlRoot = root
	}

	if err := out.Validate(); err != nil {
		return ShadowEvidenceEnvelope{}, err
	}
	return out, nil
}

func citizenKeyControlRoot(
	network types.NetworkID,
	evidence []citizen.VerifiedDeviceChallengeEvidence,
) (types.Hash, error) {
	type dedupeKey struct {
		CredentialObject types.ObjectID
		ChallengeHash    types.Hash
	}

	records := make([][]byte, 0, len(evidence))
	seen := make(map[dedupeKey]struct{}, len(evidence))
	for _, verified := range evidence {
		record, err := CitizenKeyControlFromVerified(verified)
		if err != nil || record.Network != network {
			return types.Hash{}, ErrShadowEvidence
		}
		key := dedupeKey{
			CredentialObject: record.CredentialObject,
			ChallengeHash:    record.ChallengeHash,
		}
		if _, duplicate := seen[key]; duplicate {
			return types.Hash{}, ErrShadowEvidence
		}
		seen[key] = struct{}{}
		raw, err := record.CanonicalBytes()
		if err != nil {
			return types.Hash{}, err
		}
		records = append(records, raw)
	}

	sort.Slice(records, func(i, j int) bool {
		return bytes.Compare(records[i], records[j]) < 0
	})
	var w codec.Writer
	w.U32(uint32(len(records)))
	for _, record := range records {
		w.Bytes(record)
	}
	root := types.Hash(codec.DomainHash("zephyr/citizen-key-control-evidence/v1", w.BytesCopy()))
	if types.IsZero32([32]byte(root)) {
		return types.Hash{}, ErrShadowEvidence
	}
	return root, nil
}
