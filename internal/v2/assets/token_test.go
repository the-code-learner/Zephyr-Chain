package assets

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestTokenDefinitionSupplyPoliciesAndRoundTrip(t *testing.T) {
	var token types.TokenID
	var authority types.AccountID
	token[0] = 1
	authority[0] = 2
	definition := Definition{
		TokenID: token, Name: "Capped", Symbol: "CAP", Decimals: 6,
		SupplyPolicy: SupplyCapped, MaxSupply: 1_000, CurrentSupply: 400,
		MintAuthority: authority, Burnable: true, Transferable: true,
	}
	raw, err := definition.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDefinition(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != definition {
		t.Fatalf("definition round trip mismatch: %#v != %#v", parsed, definition)
	}
	minted, err := parsed.Mint(600)
	if err != nil || minted.CurrentSupply != 1_000 {
		t.Fatalf("unexpected capped mint result: %#v %v", minted, err)
	}
	if _, err := minted.Mint(1); err != ErrInvalidTokenDefinition {
		t.Fatalf("expected cap rejection, got %v", err)
	}
	burned, err := minted.Burn(1_000)
	if err != nil || burned.CurrentSupply != 0 {
		t.Fatalf("full burn must leave a valid zero-supply definition: %#v %v", burned, err)
	}
}

func TestFixedAndUnlimitedMintPolicies(t *testing.T) {
	var token types.TokenID
	var authority types.AccountID
	token[0] = 3
	authority[0] = 4
	fixed := Definition{
		TokenID: token, Name: "Fixed", Symbol: "FIX", SupplyPolicy: SupplyFixed,
		MaxSupply: 100, CurrentSupply: 100, MintAuthority: authority, Transferable: true,
	}
	if _, err := fixed.Mint(1); err != ErrInvalidTokenDefinition {
		t.Fatalf("fixed token unexpectedly minted: %v", err)
	}
	unlimited := fixed
	unlimited.Name = "Mintable"
	unlimited.Symbol = "MINT"
	unlimited.SupplyPolicy = SupplyMintable
	unlimited.MaxSupply = 0
	if next, err := unlimited.Mint(50); err != nil || next.CurrentSupply != 150 {
		t.Fatalf("unlimited mint failed: %#v %v", next, err)
	}
}

func TestTokenMutationPayloadRoundTrip(t *testing.T) {
	var definition types.ObjectID
	var recipient types.AccountID
	definition[0] = 1
	recipient[0] = 2
	mint := MintToken{DefinitionObject: definition, Recipient: recipient, Amount: 77}
	raw, err := mint.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsedMint, err := ParseMintToken(raw)
	if err != nil || parsedMint != mint {
		t.Fatalf("mint payload mismatch: %#v %v", parsedMint, err)
	}
	burn := BurnToken{DefinitionObject: definition, Amount: 33}
	raw, err = burn.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsedBurn, err := ParseBurnToken(raw)
	if err != nil || parsedBurn != burn {
		t.Fatalf("burn payload mismatch: %#v %v", parsedBurn, err)
	}
}
