package consensus

import (
	"errors"
	"strings"
	"testing"
)

func TestSignedVoteWireSizeRejectsJSONEscapingAndExcessivePayload(t *testing.T) {
	for _, tt := range []struct{ name, payload string }{
		{"raw bytes above cap", strings.Repeat("a", int(MaxSignedVoteJSONBytes)+1)},
		{"JSON escaping above cap", strings.Repeat("\x00", int(MaxSignedVoteJSONBytes/2))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := (Vote{Payload: tt.payload}).ValidateStatic(); !errors.Is(err, ErrSignedVoteTooLarge) {
				t.Fatalf("oversized vote should fail before crypto verification, got %v", err)
			}
		})
	}
}
