package committee

import (
	"bytes"
	"crypto/elliptic"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/vrf"
)

const VRFKeyEpochVersion uint16 = 1

// VRFKeyEpoch is a historical, per-producer key-schedule claim. It is NOT a
// finalized registration, owner signature, validator grant or consensus object.
// Callers must authenticate every epoch against the same finalized Zephyr state
// and its QC-backed trust chain before using any result as authorization.
type VRFKeyEpoch struct {
	Network            types.NetworkID
	Producer           types.AccountID
	PublicKey          [vrf.P256PublicKeySize]byte
	Sequence           uint64
	RegistrationHeight uint64
	ActiveFromHeight   uint64
	InactiveFromHeight uint64 // zero means open-ended
	PreviousEpochHash  types.Hash
}

func (e VRFKeyEpoch) Validate() error {
	if types.IsZero32([32]byte(e.Network)) ||
		types.IsZero32([32]byte(e.Producer)) ||
		e.Sequence == 0 ||
		e.RegistrationHeight == 0 ||
		e.ActiveFromHeight <= e.RegistrationHeight ||
		(e.InactiveFromHeight != 0 && e.InactiveFromHeight <= e.ActiveFromHeight) {
		return ErrCommittee
	}
	if (e.Sequence == 1) != types.IsZero32([32]byte(e.PreviousEpochHash)) {
		return ErrCommittee
	}
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), e.PublicKey[:])
	if x == nil || y == nil {
		return ErrCommittee
	}
	return nil
}

func (e VRFKeyEpoch) CanonicalBytes() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(VRFKeyEpochVersion)
	w.Fixed(e.Network[:])
	w.Fixed(e.Producer[:])
	w.Fixed(e.PublicKey[:])
	w.U64(e.Sequence)
	w.U64(e.RegistrationHeight)
	w.U64(e.ActiveFromHeight)
	w.U64(e.InactiveFromHeight)
	w.Fixed(e.PreviousEpochHash[:])
	return w.BytesCopy(), nil
}

func (e VRFKeyEpoch) Hash() (types.Hash, error) {
	raw, err := e.CanonicalBytes()
	if err != nil {
		return types.Hash{}, ErrCommittee
	}
	return types.Hash(codec.DomainHash("zephyr/committee/vrf-key-epoch/v1", raw)), nil
}

// VRFKeyScheduleCommitment validates a complete sequence of per-producer key
// epochs and commits to it independently of caller ordering. A caller-supplied
// nonzero minLeadBlocks is included in the commitment; it is NOT a protocol
// policy unless independently defined and authenticated by consensus.
//
// The schedule is historical evidence, not a proof that the producer was
// authorized. The complete epoch lineage is required; pruned histories need a
// separately designed, finalized checkpoint-proof protocol.
func VRFKeyScheduleCommitment(epochs []VRFKeyEpoch, minLeadBlocks uint64) (types.Hash, error) {
	ordered, err := checkedVRFKeyEpochs(epochs, minLeadBlocks)
	if err != nil {
		return types.Hash{}, ErrCommittee
	}
	var w codec.Writer
	w.U16(VRFKeyEpochVersion)
	w.U64(minLeadBlocks)
	w.U32(uint32(len(ordered)))
	for _, epoch := range ordered {
		raw, err := epoch.CanonicalBytes()
		if err != nil {
			return types.Hash{}, ErrCommittee
		}
		w.Bytes(raw)
	}
	return types.Hash(codec.DomainHash("zephyr/committee/vrf-key-schedule/v1", w.BytesCopy())), nil
}

func checkedVRFKeyEpochs(epochs []VRFKeyEpoch, minLeadBlocks uint64) ([]VRFKeyEpoch, error) {
	if minLeadBlocks == 0 || len(epochs) == 0 || uint64(len(epochs)) > uint64(^uint32(0)) {
		return nil, ErrCommittee
	}
	ordered := append([]VRFKeyEpoch(nil), epochs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })

	first := ordered[0]
	seen := make(map[[vrf.P256PublicKeySize]byte]struct{}, len(ordered))
	for i, epoch := range ordered {
		if err := epoch.Validate(); err != nil ||
			epoch.Network != first.Network || epoch.Producer != first.Producer ||
			epoch.ActiveFromHeight-epoch.RegistrationHeight < minLeadBlocks ||
			epoch.Sequence != uint64(i)+1 {
			return nil, ErrCommittee
		}
		if _, duplicate := seen[epoch.PublicKey]; duplicate {
			return nil, ErrCommittee
		}
		seen[epoch.PublicKey] = struct{}{}
		if i == 0 {
			continue
		}
		prior := ordered[i-1]
		priorHash, err := prior.Hash()
		if err != nil || epoch.PreviousEpochHash != priorHash ||
			epoch.RegistrationHeight <= prior.RegistrationHeight ||
			epoch.ActiveFromHeight <= prior.ActiveFromHeight ||
			prior.InactiveFromHeight == 0 ||
			prior.InactiveFromHeight > epoch.ActiveFromHeight {
			return nil, ErrCommittee
		}
	}
	return ordered, nil
}

// CheckVRFKeyScheduleAtInput matches the key committed by a selection input to
// exactly one active epoch. It does not authenticate the epoch records, the
// selection's finalized anchor, or the origin of minLeadBlocks. It MUST NOT be
// used as a consensus authorization verdict on untrusted caller data.
func CheckVRFKeyScheduleAtInput(input Input, producer types.AccountID, epochs []VRFKeyEpoch, minLeadBlocks uint64) (VRFKeyEpoch, error) {
	if err := input.Validate(); err != nil || types.IsZero32([32]byte(producer)) {
		return VRFKeyEpoch{}, ErrCommittee
	}
	ordered, err := checkedVRFKeyEpochs(epochs, minLeadBlocks)
	if err != nil || ordered[0].Network != input.Network || ordered[0].Producer != producer {
		return VRFKeyEpoch{}, ErrCommittee
	}
	for _, epoch := range ordered {
		if epoch.ActiveFromHeight > input.FinalizedHeight ||
			(epoch.InactiveFromHeight != 0 && input.FinalizedHeight >= epoch.InactiveFromHeight) {
			continue
		}
		keyHash, err := VRFPublicKeyHash(epoch.PublicKey[:])
		if err != nil || keyHash != input.VRFKeyHash {
			return VRFKeyEpoch{}, ErrCommittee
		}
		return epoch, nil
	}
	return VRFKeyEpoch{}, ErrCommittee
}

// ScheduledVRFObservation commits to a successfully verified proof plus its
// key schedule. It does not solve withholding, producer authorization, beacon
// combination, unpredictability or live committee authority.
type ScheduledVRFObservation struct {
	ScheduleHash types.Hash
	KeyEpochHash types.Hash
	InputHash    types.Hash
	ProofHash    types.Hash
	Output       vrf.Output
}

// ObserveScheduledVRFProof fails closed for a missing, stale, substituted or
// invalid VRF proof. Missing evidence remains unavailable: this method never
// fabricates randomness or a fallback on proof absence.
func ObserveScheduledVRFProof(input Input, producer types.AccountID, epochs []VRFKeyEpoch, minLeadBlocks uint64, publicKey, proof []byte) (ScheduledVRFObservation, error) {
	epoch, err := CheckVRFKeyScheduleAtInput(input, producer, epochs, minLeadBlocks)
	if err != nil || !bytes.Equal(epoch.PublicKey[:], publicKey) {
		return ScheduledVRFObservation{}, ErrCommittee
	}
	scheduleHash, err := VRFKeyScheduleCommitment(epochs, minLeadBlocks)
	if err != nil {
		return ScheduledVRFObservation{}, ErrCommittee
	}
	epochHash, err := epoch.Hash()
	if err != nil {
		return ScheduledVRFObservation{}, ErrCommittee
	}
	verified, err := VerifyProof(input, publicKey, proof)
	if err != nil {
		return ScheduledVRFObservation{}, err
	}
	return ScheduledVRFObservation{
		ScheduleHash: scheduleHash,
		KeyEpochHash: epochHash,
		InputHash:    verified.InputHash(),
		ProofHash:    verified.ProofHash(),
		Output:       verified.Output(),
	}, nil
}
