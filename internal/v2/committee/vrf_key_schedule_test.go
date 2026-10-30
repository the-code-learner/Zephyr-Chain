package committee

import (
	"crypto/elliptic"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/vrf"
)

func testScheduledVRFEpoch(t *testing.T) VRFKeyEpoch {
	t.Helper()
	key := mustHex(t, "0360fed4ba255a9d31c961eb74c6356d68c049b8923b61fa6ce669622e60f29fb6")
	var publicKey [vrf.P256PublicKeySize]byte
	copy(publicKey[:], key)
	return VRFKeyEpoch{
		Network:            types.NetworkID{1},
		Producer:           types.AccountID{7},
		PublicKey:          publicKey,
		Sequence:           1,
		RegistrationHeight: 50,
		ActiveFromHeight:   80,
	}
}

func testScheduledVRFInput(t *testing.T, e VRFKeyEpoch) Input {
	t.Helper()
	root, err := EligibilityRoot([]CandidateID{candidateWithPrefix(1), candidateWithPrefix(2)})
	if err != nil {
		t.Fatal(err)
	}
	keyHash, err := VRFPublicKeyHash(e.PublicKey[:])
	if err != nil {
		t.Fatal(err)
	}
	out := testCommitteeInput(root, keyHash)
	out.Network = e.Network
	return out
}

func TestVRFKeyScheduleCommitmentAndRotation(t *testing.T) {
	first := testScheduledVRFEpoch(t)
	first.InactiveFromHeight = 120
	second := first
	second.Sequence = 2
	second.RegistrationHeight = 90
	second.ActiveFromHeight = 120
	second.InactiveFromHeight = 170
	gx, gy := elliptic.P256().Params().Gx, elliptic.P256().Params().Gy
	copy(second.PublicKey[:], elliptic.MarshalCompressed(elliptic.P256(), gx, gy))
	previous, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	second.PreviousEpochHash = previous

	schedule := []VRFKeyEpoch{first, second}
	root, err := VRFKeyScheduleCommitment(schedule, 20)
	if err != nil || types.IsZero32([32]byte(root)) {
		t.Fatalf("schedule root %x, %v", root, err)
	}
	swapped, err := VRFKeyScheduleCommitment([]VRFKeyEpoch{second, first}, 20)
	if err != nil || swapped != root {
		t.Fatalf("ordering affected schedule root: %x vs %x, err=%v", root, swapped, err)
	}
	otherLead, err := VRFKeyScheduleCommitment(schedule, 21)
	if err != nil || otherLead == root {
		t.Fatal("minimum lead was not committed")
	}

	input := testScheduledVRFInput(t, first)
	input.FinalizedHeight = 119
	got, err := CheckVRFKeyScheduleAtInput(input, first.Producer, schedule, 20)
	if err != nil || got != first {
		t.Fatalf("first key not active before rotation: %+v, %v", got, err)
	}
	input.FinalizedHeight = 120
	if _, err := CheckVRFKeyScheduleAtInput(input, first.Producer, schedule, 20); err == nil {
		t.Fatal("retired key remained valid at retirement height")
	}
	secondHash, err := VRFPublicKeyHash(second.PublicKey[:])
	if err != nil {
		t.Fatal(err)
	}
	input.VRFKeyHash = secondHash
	got, err = CheckVRFKeyScheduleAtInput(input, first.Producer, schedule, 20)
	if err != nil || got != second {
		t.Fatalf("rotated key not selected: %+v, %v", got, err)
	}
	input.FinalizedHeight = 170
	if _, err := CheckVRFKeyScheduleAtInput(input, first.Producer, schedule, 20); err == nil {
		t.Fatal("expired key remained active")
	}
}

func TestVRFKeyScheduleRejectsAmbiguousOrForgedHistory(t *testing.T) {
	first := testScheduledVRFEpoch(t)
	check := func(label string, epochs []VRFKeyEpoch, minLead uint64) {
		t.Helper()
		t.Run(label, func(t *testing.T) {
			if _, err := VRFKeyScheduleCommitment(epochs, minLead); err == nil {
				t.Fatal("invalid key schedule accepted")
			}
		})
	}
	check("missing", nil, 10)
	check("zero lead", []VRFKeyEpoch{first}, 0)
	check("grinding lead too short", []VRFKeyEpoch{first}, 31)
	check("duplicate sequence", []VRFKeyEpoch{first, first}, 10)
	check("missing initial epoch", []VRFKeyEpoch{func() VRFKeyEpoch { e := first; e.Sequence = 2; e.PreviousEpochHash = types.Hash{3}; return e }()}, 10)
	check("invalid public key", []VRFKeyEpoch{func() VRFKeyEpoch { e := first; e.PublicKey = [vrf.P256PublicKeySize]byte{}; return e }()}, 10)

	first.InactiveFromHeight = 90
	firstHash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Sequence = 2
	second.RegistrationHeight = 60
	second.ActiveFromHeight = 100
	second.InactiveFromHeight = 0
	second.PreviousEpochHash = firstHash
	// Independent key is necessary; reuse is not a rotation.
	check("reused key", []VRFKeyEpoch{first, second}, 10)
	gx, gy := elliptic.P256().Params().Gx, elliptic.P256().Params().Gy
	copy(second.PublicKey[:], elliptic.MarshalCompressed(elliptic.P256(), gx, gy))
	foreign := second
	foreign.Network = types.NetworkID{9}
	check("different network", []VRFKeyEpoch{first, foreign}, 10)
	wrongOwner := second
	wrongOwner.Producer = types.AccountID{4}
	check("different producer", []VRFKeyEpoch{first, wrongOwner}, 10)
	wrongChain := second
	wrongChain.PreviousEpochHash = types.Hash{99}
	check("wrong predecessor", []VRFKeyEpoch{first, wrongChain}, 10)
	overlap := second
	overlap.ActiveFromHeight = 80
	check("overlapping active intervals", []VRFKeyEpoch{first, overlap}, 10)
	openEnded := first
	openEnded.InactiveFromHeight = 0
	check("unretired predecessor", []VRFKeyEpoch{openEnded, second}, 10)
	sequenceGap := second
	sequenceGap.Sequence = 3
	check("missing sequence", []VRFKeyEpoch{first, sequenceGap}, 10)
}

func TestScheduledVRFObservationVerifiesBoundProof(t *testing.T) {
	epoch := testScheduledVRFEpoch(t)
	input := testScheduledVRFInput(t, epoch)
	input.FinalizedHeight = 100
	// Existing verified RFC test fixture is bound to the exact testCommitteeInput.
	// The helper's input hash changes if its eligibility root changes; this
	// test uses the existing 3-candidate fixture for exact proof compatibility.
	root, err := EligibilityRoot([]CandidateID{candidateWithPrefix(0x30), candidateWithPrefix(0x10), candidateWithPrefix(0x20)})
	if err != nil {
		t.Fatal(err)
	}
	input = testCommitteeInput(root, input.VRFKeyHash)
	input.Network = epoch.Network
	publicKey := epoch.PublicKey[:]
	proof := mustHex(t, "022e513fd5cb343298aebd62726a5d543e21f500b5315bfac2673110874e233e74b1440c13cc3580eaf08e9ef3f810e9e7663d1aad7eb93bb49d147e594a27567e94a6082934a602dd42a73e48392e3ed2")
	observed, err := ObserveScheduledVRFProof(input, epoch.Producer, []VRFKeyEpoch{epoch}, 20, publicKey, proof)
	if err != nil || types.IsZero32([32]byte(observed.ProofHash)) ||
		types.IsZero32([32]byte(observed.ScheduleHash)) ||
		types.IsZero32([32]byte(observed.KeyEpochHash)) ||
		types.IsZero32([32]byte(observed.InputHash)) {
		t.Fatalf("observation %v, %v", observed, err)
	}
	verified, err := VerifyProof(input, publicKey, proof)
	if err != nil || observed.Output != verified.Output() {
		t.Fatal("observation did not preserve verified output")
	}
	if _, err := ObserveScheduledVRFProof(input, epoch.Producer, []VRFKeyEpoch{epoch}, 20, publicKey, nil); err == nil {
		t.Fatal("missing proof was accepted")
	}
	changed := append([]byte(nil), proof...)
	changed[len(changed)-1] ^= 1
	if _, err := ObserveScheduledVRFProof(input, epoch.Producer, []VRFKeyEpoch{epoch}, 20, publicKey, changed); err == nil {
		t.Fatal("invalid proof was accepted")
	}
	modifiedInput := input
	modifiedInput.Round++
	if _, err := ObserveScheduledVRFProof(modifiedInput, epoch.Producer, []VRFKeyEpoch{epoch}, 20, publicKey, proof); err == nil {
		t.Fatal("proof replayed for another round")
	}
	modifiedInput = input
	modifiedInput.FinalizedHeight = 79
	if _, err := ObserveScheduledVRFProof(modifiedInput, epoch.Producer, []VRFKeyEpoch{epoch}, 20, publicKey, proof); err == nil {
		t.Fatal("pre-activation proof accepted")
	}
	modifiedInput = input
	modifiedInput.Network = types.NetworkID{7}
	if _, err := ObserveScheduledVRFProof(modifiedInput, epoch.Producer, []VRFKeyEpoch{epoch}, 20, publicKey, proof); err == nil {
		t.Fatal("cross-network schedule accepted")
	}
	if _, err := ObserveScheduledVRFProof(input, types.AccountID{2}, []VRFKeyEpoch{epoch}, 20, publicKey, proof); err == nil {
		t.Fatal("wrong producer accepted")
	}
}
