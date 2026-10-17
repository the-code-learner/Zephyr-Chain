package committee

import (
	"encoding/hex"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestVRFCommitteeProofAndSelectionEndToEnd(t *testing.T) {
	candidates := []CandidateID{
		candidateWithPrefix(0x30),
		candidateWithPrefix(0x10),
		candidateWithPrefix(0x20),
	}
	root, err := EligibilityRoot(candidates)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(root[:]), "c7ed039ef6e311ecef78b130945bbcb5a7f729e6e75fb574e091db0075e3301e"; got != want {
		t.Fatalf("eligibility root = %s, want %s", got, want)
	}

	publicKey := mustHex(t, "0360fed4ba255a9d31c961eb74c6356d68c049b8923b61fa6ce669622e60f29fb6")
	keyHash, err := VRFPublicKeyHash(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(keyHash[:]), "2145c8ce18f3fdce21bc4415ef16766f2a44fac1e858893fb63c657955a49772"; got != want {
		t.Fatalf("VRF key hash = %s, want %s", got, want)
	}

	input := testCommitteeInput(root, keyHash)
	inputHash, err := input.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(inputHash[:]), "742766307788386f6f251b07a9e9ce2c1a336c912dec9e9a1743e4e59d6cfd8d"; got != want {
		t.Fatalf("input hash = %s, want %s", got, want)
	}

	// Generated from the RFC 9381 Appendix B.1 example-10 secret key against
	// the exact canonical input hash above. The low-level verifier is
	// independently pinned to the RFC's official vectors.
	proof := mustHex(t, "022e513fd5cb343298aebd62726a5d543e21f500b5315bfac2673110874e233e74b1440c13cc3580eaf08e9ef3f810e9e7663d1aad7eb93bb49d147e594a27567e94a6082934a602dd42a73e48392e3ed2")
	verified, err := VerifyProof(input, publicKey, proof)
	if err != nil {
		t.Fatal(err)
	}
	output := verified.Output()
	if got, want := hex.EncodeToString(output[:]), "e2bcdab31497231e70e1dd1740e263ef5fd266c85ec204af70b37288dbe0d0f3"; got != want {
		t.Fatalf("verified output = %s, want %s", got, want)
	}

	selected, err := SelectUnweighted(input, verified, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0] != candidateWithPrefix(0x10) || selected[1] != candidateWithPrefix(0x20) {
		t.Fatalf("unexpected committee selection: %x", selected)
	}

	shuffled := []CandidateID{candidates[1], candidates[2], candidates[0]}
	selectedAgain, err := SelectUnweighted(input, verified, shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if len(selectedAgain) != len(selected) || selectedAgain[0] != selected[0] || selectedAgain[1] != selected[1] {
		t.Fatal("candidate caller order changed deterministic selection")
	}
}

func TestCommitteeInputRoundTripAndContextBinding(t *testing.T) {
	candidates := []CandidateID{candidateWithPrefix(1), candidateWithPrefix(2), candidateWithPrefix(3)}
	root, err := EligibilityRoot(candidates)
	if err != nil {
		t.Fatal(err)
	}
	keyHash := types.HashBytes("committee/test-key", []byte("key"))
	input := testCommitteeInput(root, keyHash)

	raw, err := input.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != input {
		t.Fatalf("input round-trip mismatch: %+v", parsed)
	}

	first, err := input.Hash()
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Input){
		"height": func(value *Input) { value.FinalizedHeight++ },
		"header": func(value *Input) { value.FinalizedHeaderHash = types.HashBytes("committee/header", []byte("other")) },
		"eligibility": func(value *Input) { value.EligibilityRoot = types.HashBytes("committee/root", []byte("other")) },
		"key": func(value *Input) { value.VRFKeyHash = types.HashBytes("committee/key", []byte("other")) },
		"purpose": func(value *Input) { value.Purpose = types.HashBytes("committee/purpose", []byte("other")) },
		"round": func(value *Input) { value.Round++ },
		"size": func(value *Input) { value.CommitteeSize++ },
	} {
		t.Run(name, func(t *testing.T) {
			changed := input
			mutate(&changed)
			hash, err := changed.Hash()
			if err != nil {
				t.Fatal(err)
			}
			if hash == first {
				t.Fatal("context mutation did not change committee input hash")
			}
		})
	}
}

func TestEligibilityRootRejectsAmbiguousSets(t *testing.T) {
	if _, err := EligibilityRoot(nil); err == nil {
		t.Fatal("empty eligibility set was accepted")
	}
	if _, err := EligibilityRoot([]CandidateID{{}}); err == nil {
		t.Fatal("zero candidate was accepted")
	}
	candidate := candidateWithPrefix(1)
	if _, err := EligibilityRoot([]CandidateID{candidate, candidate}); err == nil {
		t.Fatal("duplicate candidate was accepted")
	}

	first, err := EligibilityRoot([]CandidateID{candidateWithPrefix(1), candidateWithPrefix(2)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := EligibilityRoot([]CandidateID{candidateWithPrefix(2), candidateWithPrefix(1)})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("eligibility root depends on caller ordering")
	}
}

func TestCommitteeSelectionRejectsUnverifiedOrMismatchedEvidence(t *testing.T) {
	candidates := []CandidateID{candidateWithPrefix(1), candidateWithPrefix(2), candidateWithPrefix(3)}
	root, err := EligibilityRoot(candidates)
	if err != nil {
		t.Fatal(err)
	}
	keyHash := types.HashBytes("committee/test-key", []byte("key"))
	input := testCommitteeInput(root, keyHash)

	if _, err := SelectUnweighted(input, VerifiedProof{}, candidates); err == nil {
		t.Fatal("zero/unverified VRF evidence was accepted")
	}

	inputHash, err := input.Hash()
	if err != nil {
		t.Fatal(err)
	}
	verified := VerifiedProof{
		inputHash:     inputHash,
		publicKeyHash: keyHash,
		valid:         true,
	}
	if _, err := SelectUnweighted(input, verified, []CandidateID{candidateWithPrefix(1), candidateWithPrefix(2), candidateWithPrefix(4)}); err == nil {
		t.Fatal("candidate set different from committed eligibility root was accepted")
	}

	changed := input
	changed.Round++
	if _, err := SelectUnweighted(changed, verified, candidates); err == nil {
		t.Fatal("VRF evidence was replayed under a different selection input")
	}

	changed = input
	changed.CommitteeSize = 4
	if _, err := SelectUnweighted(changed, verified, candidates); err == nil {
		t.Fatal("oversized committee request was accepted")
	}
}

func TestVerifyProofRejectsKeyCommitmentSubstitution(t *testing.T) {
	candidates := []CandidateID{candidateWithPrefix(1)}
	root, err := EligibilityRoot(candidates)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := mustHex(t, "0360fed4ba255a9d31c961eb74c6356d68c049b8923b61fa6ce669622e60f29fb6")
	actualKeyHash, err := VRFPublicKeyHash(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	input := testCommitteeInput(root, types.HashBytes("committee/wrong-key", []byte("wrong")))
	if input.VRFKeyHash == actualKeyHash {
		t.Fatal("test key hash unexpectedly matches")
	}
	if _, err := VerifyProof(input, publicKey, make([]byte, 81)); err == nil {
		t.Fatal("public key not matching committed VRF key hash was accepted")
	}
}

func testCommitteeInput(root, keyHash types.Hash) Input {
	var network types.NetworkID
	for index := range network {
		network[index] = 0x01
	}
	var header types.Hash
	for index := range header {
		header[index] = 0x02
	}
	var purpose types.Hash
	for index := range purpose {
		purpose[index] = 0x03
	}
	return Input{
		Network:             network,
		FinalizedHeight:     100,
		FinalizedHeaderHash: header,
		EligibilityRoot:     root,
		VRFKeyHash:          keyHash,
		Purpose:             purpose,
		Round:               7,
		CommitteeSize:       2,
	}
}

func candidateWithPrefix(prefix byte) CandidateID {
	var candidate CandidateID
	candidate[0] = prefix
	return candidate
}

func mustHex(t *testing.T, value string) []byte {
	t.Helper()
	output, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return output
}
