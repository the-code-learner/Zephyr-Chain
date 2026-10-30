package main

import (
	"crypto/elliptic"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/genesis"
)

func validGenesisAuditFixture(t *testing.T) ([]byte, string, string) {
	t.Helper()
	key := elliptic.Marshal(elliptic.P256(), elliptic.P256().Params().Gx, elliptic.P256().Params().Gy)
	owner := make([]byte, 32)
	owner[0] = 7
	raw := []byte(fmt.Sprintf(`{"version":2,"chainName":"zephyr-test","genesisUnix":1,"initialShardCount":1,"maxShardCount":1,"nativeSymbol":"ZPH","validators":[{"consensusPublicKey":"%s","votingPower":"10"}],"allocations":[{"owner":"%s","amount":"1000"}]}`,hex.EncodeToString(key),hex.EncodeToString(owner)))
	cfg, err := genesis.ParseJSON(raw)
	if err != nil { t.Fatal(err) }
	anchor, err := cfg.TrustAnchor()
	if err != nil { t.Fatal(err) }
	return raw, hex.EncodeToString(anchor.Network[:]), anchor.ValidatorRoot.String()
}

func TestGenesisAuditRequiresPinnedTrustAnchors(t *testing.T) {
	raw, network, root := validGenesisAuditFixture(t)
	report, err := auditGenesis(raw, network, root, false)
	if err != nil || !report.AnchorsMatched || report.InitialSupply != 1000 ||
		report.ValidatorCount != 1 || report.InitialShards != 1 {
		t.Fatalf("valid genesis rejected: %+v, %v", report, err)
	}
	if _, err := auditGenesis(raw, "", "", false); err == nil {
		t.Fatal("untrusted genesis accepted without pinned hashes")
	}
	if _, err := auditGenesis(raw, network, "", false); err == nil {
		t.Fatal("incomplete anchor accepted")
	}
	if _, err := auditGenesis(raw, root, network, false); err == nil {
		t.Fatal("swapped anchors accepted")
	}
	if _, err := auditGenesis(raw, network, network, false); err == nil {
		t.Fatal("validator-root substitution accepted")
	}
	if _, err := auditGenesis(raw, "", "", true); err != nil {
		t.Fatalf("inspection should not require pinned hashes: %v", err)
	}
	if _, err := auditGenesis(raw, network, root, true); err == nil {
		t.Fatal("misleading inspect+verify configuration accepted")
	}
}

func TestGenesisAuditRejectsInvalidOrOverflowedInputs(t *testing.T) {
	raw, network, root := validGenesisAuditFixture(t)
	for _, b := range [][]byte{nil, []byte("{}"), make([]byte, maxGenesisAuditBytes+1)} {
		if _, err := auditGenesis(b, network, root, false); err == nil {
			t.Fatal("malformed/big genesis accepted")
		}
	}
	if _, err := auditGenesis(raw, strconv.FormatUint(0, 16), root, false); err == nil {
		t.Fatal("invalid short network accepted")
	}
	if _, err := auditGenesis(raw, network+" ", root, false); err == nil {
		t.Fatal("trailing whitespace on expected network accepted")
	}
	// No extra fields may be silently accepted in the canonical genesis JSON.
	extra := append([]byte(nil), raw[:len(raw)-1]...)
	extra = append(extra, []byte(`,"unexpected":true}`)...)
	if _, err := auditGenesis(extra, network, root, false); err == nil {
		t.Fatal("unknown genesis field accepted")
	}
}
