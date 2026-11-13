package tx

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestEnvelopeWireSizeIncludesActualJSONEscapingAndSignedPayload(t *testing.T) {
	base := Envelope{ChainID: "zephyr", Domain: "tx", From: "from", To: "to"}
	if err := base.ValidateWireSize(); err != nil {
		t.Fatalf("ordinary envelope exceeds limit: %v", err)
	}
	// An oversized raw memo is rejected BEFORE producing a massive JSON
	// object or attempting an expensive cryptographic verification.
	oversized := base
	oversized.Memo = strings.Repeat("x", MaxEnvelopeJSONBytes+1)
	if err := oversized.ValidateWireSize(); !errors.Is(err, ErrEnvelopeTooLarge) {
		t.Fatalf("oversized memo: got %v, want ErrEnvelopeTooLarge", err)
	}
	if err := oversized.ValidateStatic(); !errors.Is(err, ErrEnvelopeTooLarge) {
		t.Fatalf("signature validation did not reject oversize first: %v", err)
	}
	// JSON escaping can make a short raw memo exceed the signed wire cap.
	escaped := base
	escaped.Memo = strings.Repeat("\x00", MaxEnvelopeJSONBytes/3)
	if err := escaped.ValidateWireSize(); !errors.Is(err, ErrEnvelopeTooLarge) {
		t.Fatalf("escaped JSON amplification bypassed size bound: %v", err)
	}
	// The exact serialized envelope boundary is permitted, but +1 byte
	// fails closed. A forged huge signed Payload is bounded the same way.
	encoded := base
	b, err := json.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	encoded.Payload = strings.Repeat("P", MaxEnvelopeJSONBytes-len(b))
	if err := encoded.ValidateWireSize(); err != nil {
		t.Fatalf("exact byte boundary rejected: %v", err)
	}
	encoded.Payload += "P"
	if err := encoded.ValidateWireSize(); !errors.Is(err, ErrEnvelopeTooLarge) {
		t.Fatalf("over-bound payload accepted: %v", err)
	}
}
