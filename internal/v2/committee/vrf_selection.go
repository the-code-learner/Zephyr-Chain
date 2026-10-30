package committee

import (
	"bytes"
	"errors"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/vrf"
)

const Version uint16 = 1

var ErrCommittee = errors.New("invalid VRF committee selection")

type CandidateID [32]byte

// Input commits one VRF evaluation to one public finalized selection context.
//
// FinalizedHeaderHash, EligibilityRoot and VRFKeyHash are commitments. This
// package does not establish that they came from authorized finalized state;
// a future consensus integration must verify that before accepting selection
// evidence.
type Input struct {
	Network             types.NetworkID
	FinalizedHeight     uint64
	FinalizedHeaderHash types.Hash
	EligibilityRoot     types.Hash
	VRFKeyHash          types.Hash
	Purpose             types.Hash
	Round               uint64
	CommitteeSize       uint32
}

func (i Input) Validate() error {
	if types.IsZero32([32]byte(i.Network)) ||
		i.FinalizedHeight == 0 ||
		types.IsZero32([32]byte(i.FinalizedHeaderHash)) ||
		types.IsZero32([32]byte(i.EligibilityRoot)) ||
		types.IsZero32([32]byte(i.VRFKeyHash)) ||
		types.IsZero32([32]byte(i.Purpose)) ||
		i.CommitteeSize == 0 {
		return ErrCommittee
	}
	return nil
}

func (i Input) MarshalBinary() ([]byte, error) {
	if err := i.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(Version)
	w.Fixed(i.Network[:])
	w.U64(i.FinalizedHeight)
	w.Fixed(i.FinalizedHeaderHash[:])
	w.Fixed(i.EligibilityRoot[:])
	w.Fixed(i.VRFKeyHash[:])
	w.Fixed(i.Purpose[:])
	w.U64(i.Round)
	w.U32(i.CommitteeSize)
	return w.BytesCopy(), nil
}

func ParseInput(data []byte) (Input, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Input{}, ErrCommittee
	}
	networkRaw, err := r.Fixed(32)
	if err != nil {
		return Input{}, ErrCommittee
	}
	height, err := r.U64()
	if err != nil {
		return Input{}, ErrCommittee
	}
	headerRaw, err := r.Fixed(32)
	if err != nil {
		return Input{}, ErrCommittee
	}
	eligibilityRaw, err := r.Fixed(32)
	if err != nil {
		return Input{}, ErrCommittee
	}
	keyRaw, err := r.Fixed(32)
	if err != nil {
		return Input{}, ErrCommittee
	}
	purposeRaw, err := r.Fixed(32)
	if err != nil {
		return Input{}, ErrCommittee
	}
	round, err := r.U64()
	if err != nil {
		return Input{}, ErrCommittee
	}
	size, err := r.U32()
	if err != nil || r.Done() != nil {
		return Input{}, ErrCommittee
	}

	var network types.NetworkID
	var header types.Hash
	var eligibility types.Hash
	var keyHash types.Hash
	var purpose types.Hash
	copy(network[:], networkRaw)
	copy(header[:], headerRaw)
	copy(eligibility[:], eligibilityRaw)
	copy(keyHash[:], keyRaw)
	copy(purpose[:], purposeRaw)

	out := Input{
		Network:             network,
		FinalizedHeight:     height,
		FinalizedHeaderHash: header,
		EligibilityRoot:     eligibility,
		VRFKeyHash:          keyHash,
		Purpose:             purpose,
		Round:               round,
		CommitteeSize:       size,
	}
	if err := out.Validate(); err != nil {
		return Input{}, err
	}
	return out, nil
}

func (i Input) Hash() (types.Hash, error) {
	raw, err := i.MarshalBinary()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/committee/vrf-input/v1", raw)), nil
}

func VRFPublicKeyHash(publicKey []byte) (types.Hash, error) {
	if len(publicKey) != vrf.P256PublicKeySize {
		return types.Hash{}, ErrCommittee
	}
	return types.Hash(codec.DomainHash("zephyr/committee/vrf-key/v1", publicKey)), nil
}

// EligibilityRoot commits to the exact unique candidate set independently of
// caller ordering. Candidate authorization/eligibility is deliberately outside
// this package and must come from finalized verifiable state.
func EligibilityRoot(candidates []CandidateID) (types.Hash, error) {
	if len(candidates) == 0 || uint64(len(candidates)) > uint64(^uint32(0)) {
		return types.Hash{}, ErrCommittee
	}
	ordered := append([]CandidateID(nil), candidates...)
	sort.Slice(ordered, func(a, b int) bool {
		return bytes.Compare(ordered[a][:], ordered[b][:]) < 0
	})
	for index, candidate := range ordered {
		if types.IsZero32([32]byte(candidate)) {
			return types.Hash{}, ErrCommittee
		}
		if index > 0 && candidate == ordered[index-1] {
			return types.Hash{}, ErrCommittee
		}
	}

	var w codec.Writer
	w.U32(uint32(len(ordered)))
	for _, candidate := range ordered {
		w.Fixed(candidate[:])
	}
	return types.Hash(codec.DomainHash("zephyr/committee/eligibility/v1", w.BytesCopy())), nil
}

// VerifiedProof can only be obtained through VerifyProof outside this package.
// It proves a VRF relation for the supplied key and Input hash; it does not
// prove that the key or the committed eligibility set is protocol-authorized.
type VerifiedProof struct {
	inputHash     types.Hash
	publicKeyHash types.Hash
	proofHash     types.Hash
	output        vrf.Output
	valid         bool
}

func VerifyProof(input Input, publicKey, proof []byte) (VerifiedProof, error) {
	if err := input.Validate(); err != nil {
		return VerifiedProof{}, err
	}
	keyHash, err := VRFPublicKeyHash(publicKey)
	if err != nil || keyHash != input.VRFKeyHash {
		return VerifiedProof{}, ErrCommittee
	}
	inputHash, err := input.Hash()
	if err != nil {
		return VerifiedProof{}, err
	}
	output, err := vrf.VerifyP256SHA256TAI(publicKey, inputHash[:], proof)
	if err != nil {
		return VerifiedProof{}, err
	}
	return VerifiedProof{
		inputHash:     inputHash,
		publicKeyHash: keyHash,
		proofHash:     types.Hash(codec.DomainHash("zephyr/committee/vrf-proof/v1", proof)),
		output:        output,
		valid:         true,
	}, nil
}

func (v VerifiedProof) InputHash() types.Hash     { return v.inputHash }
func (v VerifiedProof) PublicKeyHash() types.Hash { return v.publicKeyHash }
func (v VerifiedProof) ProofHash() types.Hash     { return v.proofHash }
func (v VerifiedProof) Output() vrf.Output        { return v.output }

// SelectUnweighted deterministically ranks the exact committed candidate set
// using a verified VRF output and returns the first CommitteeSize candidates.
//
// This is intentionally unweighted: ContributionScore, stake weighting and
// governance power are not defined by this tranche. A future live protocol
// must separately verify the finalized anchor, VRF-key authorization and
// eligibility state before this result can carry authority.
func SelectUnweighted(input Input, verified VerifiedProof, candidates []CandidateID) ([]CandidateID, error) {
	if !verified.valid {
		return nil, ErrCommittee
	}
	inputHash, err := input.Hash()
	if err != nil || inputHash != verified.inputHash || input.VRFKeyHash != verified.publicKeyHash {
		return nil, ErrCommittee
	}
	root, err := EligibilityRoot(candidates)
	if err != nil || root != input.EligibilityRoot || uint64(input.CommitteeSize) > uint64(len(candidates)) {
		return nil, ErrCommittee
	}

	type rankedCandidate struct {
		id   CandidateID
		rank types.Hash
	}
	ranked := make([]rankedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		var w codec.Writer
		w.Fixed(inputHash[:])
		w.Fixed(verified.output[:])
		w.Fixed(candidate[:])
		ranked = append(ranked, rankedCandidate{
			id:   candidate,
			rank: types.Hash(codec.DomainHash("zephyr/committee/unweighted-rank/v1", w.BytesCopy())),
		})
	}
	sort.Slice(ranked, func(a, b int) bool {
		if cmp := bytes.Compare(ranked[a].rank[:], ranked[b].rank[:]); cmp != 0 {
			return cmp < 0
		}
		return bytes.Compare(ranked[a].id[:], ranked[b].id[:]) < 0
	})

	selected := make([]CandidateID, input.CommitteeSize)
	for index := range selected {
		selected[index] = ranked[index].id
	}
	return selected, nil
}
