package lab

import (
	"bytes"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/da"
)

func TestV2LabDataAvailabilityWithholdingGate(t *testing.T) {
	payload := bytes.Repeat([]byte("zephyr-v2-da-fault/"), 4096)
	commitment, chunks, samples, err := da.EncodeBlob(payload, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Losing up to parity capacity must not lose the block payload.
	withinTolerance := cloneDAChunks(chunks)
	withinTolerance[0] = nil
	withinTolerance[5] = nil
	withinTolerance[11] = nil
	recovered, err := da.ReconstructBlob(commitment, withinTolerance, samples)
	if err != nil || !bytes.Equal(recovered, payload) {
		t.Fatalf("DA recovery within parity budget failed: %v", err)
	}

	// Withholding more than parity capacity must fail closed.
	withheld := cloneDAChunks(chunks)
	for _, index := range []int{0, 1, 2, 3, 4} {
		withheld[index] = nil
	}
	if _, err := da.ReconstructBlob(commitment, withheld, samples); err != da.ErrReconstruction {
		t.Fatalf("expected withholding failure, got %v", err)
	}
}

func TestV2LabDataAvailabilityCorruptionIsTreatedAsMissing(t *testing.T) {
	payload := bytes.Repeat([]byte("authenticated-da"), 2048)
	commitment, chunks, samples, err := da.EncodeBlob(payload, 6, 3)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := cloneDAChunks(chunks)
	corrupt[2][0] ^= 0xff
	corrupt[8][len(corrupt[8])-1] ^= 0x01
	recovered, err := da.ReconstructBlob(commitment, corrupt, samples)
	if err != nil || !bytes.Equal(recovered, payload) {
		t.Fatalf("authenticated corruption recovery failed: %v", err)
	}
}

func cloneDAChunks(in [][]byte) [][]byte {
	out := make([][]byte, len(in))
	for i, chunk := range in {
		if chunk != nil {
			out[i] = append([]byte(nil), chunk...)
		}
	}
	return out
}
