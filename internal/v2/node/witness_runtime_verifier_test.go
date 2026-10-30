//go:build linux || darwin

package node

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestRuntimeWitnessVerifierFailsClosedWithoutCertifiedAnchor(t *testing.T) {
	if _, err := NewRuntimeWitnessFinalityVerifier(nil); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("nil runtime accepted: %v", err)
	}
	if _, err := NewRuntimeWitnessFinalityVerifier(&Runtime{}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("empty runtime accepted: %v", err)
	}
	var absent *RuntimeWitnessFinalityVerifier
	if err := absent.VerifyFinalizedCheckpoint(FinalityCheckpoint{}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("nil verifier accepted: %v", err)
	}
	if err := ReserveRuntimeVerifiedWitnessTip(nil, absent); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("nil journal accepted: %v", err)
	}
}

func TestRuntimeWitnessVerifierOnlyAuthorizesExactSealedTip(t *testing.T) {
	network := types.NetworkID(types.HashBytes("runtime-witness", []byte("net")))
	root := types.HashBytes("runtime-witness", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 12, HeaderHash: types.Hash{2}}
	v := &RuntimeWitnessFinalityVerifier{certified: cp}
	if err := v.VerifyFinalizedCheckpoint(cp); err != nil {
		t.Fatalf("exact sealed tip rejected: %v", err)
	}
	changed := cp
	changed.Height++
	if err := v.VerifyFinalizedCheckpoint(changed); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("future height accepted: %v", err)
	}
	changed = cp
	changed.HeaderHash[0]++
	if err := v.VerifyFinalizedCheckpoint(changed); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("same-height hash conflict accepted: %v", err)
	}
	changed = cp
	changed.Network = types.NetworkID{3}
	if err := v.VerifyFinalizedCheckpoint(changed); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("foreign network accepted: %v", err)
	}
	changed = cp
	changed.ValidatorRoot = types.Hash{4}
	if err := v.VerifyFinalizedCheckpoint(changed); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("foreign root accepted: %v", err)
	}
	other := &RuntimeWitnessFinalityVerifier{certified: cp}
	j := &WitnessSigningIntentJournal{verifier: other}
	if err := ReserveRuntimeVerifiedWitnessTip(j, v); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("mismatched verifier accepted: %v", err)
	}
}
