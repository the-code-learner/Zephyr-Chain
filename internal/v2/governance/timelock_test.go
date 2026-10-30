package governance

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestProposalRoundTripAndActionBinding(t *testing.T) {
	proposal := Proposal{
		Proposer:      types.AccountID(types.HashBytes("governance/proposer", []byte("alice"))),
		ActionKind:    ActionProtocolParameter,
		ActionHash:    types.HashBytes("governance/action", []byte("parameter-a")),
		CreatedHeight: 100,
		Nonce:         types.HashBytes("governance/nonce", []byte("proposal-1")),
	}
	raw, err := proposal.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseProposal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != proposal {
		t.Fatalf("proposal round-trip mismatch: %+v", parsed)
	}
	first, err := proposal.Hash()
	if err != nil {
		t.Fatal(err)
	}
	changed := proposal
	changed.ActionHash = types.HashBytes("governance/action", []byte("parameter-b"))
	second, err := changed.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("proposal hash did not bind action commitment")
	}
}

func TestTimelockRoundTripAndHeightSemantics(t *testing.T) {
	lock := Timelock{
		ProposalHash:       types.HashBytes("governance/proposal", []byte("1")),
		AuthorizationHash:  types.HashBytes("governance/authorization", []byte("1")),
		AuthorizedHeight:   100,
		ExecuteAfterHeight: 120,
		ExpiresHeight:      150,
	}
	if err := lock.ValidateMinDelay(20); err != nil {
		t.Fatal(err)
	}
	if err := lock.ValidateMinDelay(21); err == nil {
		t.Fatal("timelock accepted a delay shorter than policy minimum")
	}
	if lock.ReadyAt(119) {
		t.Fatal("timelock became ready before execute-after height")
	}
	if !lock.ReadyAt(120) || !lock.ReadyAt(149) {
		t.Fatal("timelock was not ready inside execution window")
	}
	if lock.ReadyAt(150) || !lock.ExpiredAt(150) {
		t.Fatal("timelock expiry boundary was not enforced")
	}
	raw, err := lock.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseTimelock(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != lock {
		t.Fatalf("timelock round-trip mismatch: %+v", parsed)
	}
}

func TestTimelockWithoutExpiryRemainsReadyAfterActivation(t *testing.T) {
	lock := Timelock{
		ProposalHash:       types.HashBytes("governance/proposal", []byte("2")),
		AuthorizationHash:  types.HashBytes("governance/authorization", []byte("2")),
		AuthorizedHeight:   10,
		ExecuteAfterHeight: 11,
	}
	if !lock.ReadyAt(11) || !lock.ReadyAt(1_000_000) || lock.ExpiredAt(1_000_000) {
		t.Fatal("non-expiring timelock has incorrect height semantics")
	}
}

func TestTimelockRejectsInvalidWindowsAndCommitments(t *testing.T) {
	base := Timelock{
		ProposalHash:       types.HashBytes("governance/proposal", []byte("3")),
		AuthorizationHash:  types.HashBytes("governance/authorization", []byte("3")),
		AuthorizedHeight:   50,
		ExecuteAfterHeight: 60,
		ExpiresHeight:      70,
	}
	bad := base
	bad.ProposalHash = types.Hash{}
	if bad.Validate() == nil {
		t.Fatal("zero proposal hash was accepted")
	}
	bad = base
	bad.AuthorizationHash = types.Hash{}
	if bad.Validate() == nil {
		t.Fatal("zero authorization hash was accepted")
	}
	bad = base
	bad.ExecuteAfterHeight = bad.AuthorizedHeight
	if bad.Validate() == nil {
		t.Fatal("zero-delay timelock was accepted")
	}
	bad = base
	bad.ExpiresHeight = bad.ExecuteAfterHeight
	if bad.Validate() == nil {
		t.Fatal("empty execution window was accepted")
	}
}

func TestTimelockHashBindsAuthorizationAndProposal(t *testing.T) {
	base := Timelock{
		ProposalHash:       types.HashBytes("governance/proposal", []byte("4")),
		AuthorizationHash:  types.HashBytes("governance/authorization", []byte("4")),
		AuthorizedHeight:   75,
		ExecuteAfterHeight: 100,
		ExpiresHeight:      200,
	}
	first, err := base.Hash()
	if err != nil {
		t.Fatal(err)
	}
	changedAuthorization := base
	changedAuthorization.AuthorizationHash = types.HashBytes("governance/authorization", []byte("different"))
	second, err := changedAuthorization.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("timelock hash did not bind authorization commitment")
	}
	changedProposal := base
	changedProposal.ProposalHash = types.HashBytes("governance/proposal", []byte("different"))
	third, err := changedProposal.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("timelock hash did not bind proposal commitment")
	}
}

func TestProposalRejectsUnknownActionAndZeroFields(t *testing.T) {
	base := Proposal{
		Proposer:      types.AccountID(types.HashBytes("governance/proposer", []byte("bob"))),
		ActionKind:    ActionValidatorTransition,
		ActionHash:    types.HashBytes("governance/action", []byte("validator-transition")),
		CreatedHeight: 1,
		Nonce:         types.HashBytes("governance/nonce", []byte("proposal-2")),
	}
	bad := base
	bad.ActionKind = ActionUnknown
	if bad.Validate() == nil {
		t.Fatal("unknown governance action was accepted")
	}
	bad = base
	bad.ActionHash = types.Hash{}
	if bad.Validate() == nil {
		t.Fatal("zero action commitment was accepted")
	}
	bad = base
	bad.CreatedHeight = 0
	if bad.Validate() == nil {
		t.Fatal("zero proposal height was accepted")
	}
}
