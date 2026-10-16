package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCitizenNodeRejectsBadAnchorAndDoesNotAdvanceOnBadBundle(t *testing.T) {
	if _, err := NewCitizenNode("00", "11"); err != ErrCitizenAnchor {
		t.Fatalf("expected bad anchor rejection, got %v", err)
	}
	network := strings.Repeat("11", 32)
	root := strings.Repeat("22", 32)
	node, err := NewCitizenNode(network, root)
	if err != nil {
		t.Fatal(err)
	}
	before := node.ValidatorRoot()
	if _, err := node.VerifyObjectBundle(`{"network":"bad"}`); err == nil {
		t.Fatal("invalid proof bundle was accepted")
	}
	if node.ValidatorRoot() != before {
		t.Fatal("trust anchor advanced after invalid bundle")
	}
}

func TestMobileCitizenModeIsAdaptive(t *testing.T) {
	var low map[string]bool
	if err := json.Unmarshal([]byte(SelectCitizenMode(10, false, true, false, true)), &low); err != nil {
		t.Fatal(err)
	}
	if !low["verifyHeaders"] || low["relay"] || low["sampleDA"] || low["executeRecent"] {
		t.Fatalf("unexpected low-power mode: %+v", low)
	}
	var charging map[string]bool
	if err := json.Unmarshal([]byte(SelectCitizenMode(80, true, true, false, true)), &charging); err != nil {
		t.Fatal(err)
	}
	if !charging["verifyHeaders"] || !charging["relay"] || !charging["sampleDA"] || !charging["executeRecent"] || !charging["serveCache"] {
		t.Fatalf("unexpected charging mode: %+v", charging)
	}
}
