package api

import (
	"io"
	"strings"
	"testing"
)

type peerUnexpectedTailReader struct {
	reads int
}

func (r *peerUnexpectedTailReader) Read(_ []byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

// Reading an enormous attacker-controlled second JSON array into any can
// allocate memory proportional to the attack. The decoder may prefetch
// bytes from the first chunk: reject the first non-whitespace byte without
// consuming any further tail data.
func TestDecodeSinglePeerJSONRejectsTailWithoutDecodingSecondValue(t *testing.T) {
	unread := &peerUnexpectedTailReader{}
	var decoded struct{}
	err := decodeSinglePeerJSON(io.MultiReader(strings.NewReader("{}["), unread), &decoded)
	if err == nil {
		t.Fatal("second JSON value prefix was accepted")
	}
	if unread.reads != 0 {
		t.Fatalf("decoder consumed %d additional reads after malicious second JSON prefix", unread.reads)
	}
}

// Whitespace can span the Decoder's internal prefetch buffer and multiple
// scanner chunks. Only the four RFC 8259 JSON whitespace bytes are legal.
func TestDecodeSinglePeerJSONStreamingWhitespaceBoundaries(t *testing.T) {
	valid := "{}" + strings.Repeat(" \t\r\n", 8192)
	var destination struct{}
	if err := decodeSinglePeerJSON(strings.NewReader(valid), &destination); err != nil {
		t.Fatalf("valid large trailing JSON whitespace rejected: %v", err)
	}
	for _, suffix := range []string{
		strings.Repeat(" ", 8192) + "null",
		strings.Repeat("\n", 8192) + "{",
		strings.Repeat("\t", 8192) + "\v",
	} {
		if err := decodeSinglePeerJSON(strings.NewReader("{}"+suffix), &destination); err == nil {
			t.Fatalf("extra peer data accepted after %d trailing bytes", len(suffix))
		}
	}
}
