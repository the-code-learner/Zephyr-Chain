package da

import (
	"bytes"
	"testing"
)

func TestErasureCodingReconstructsMissingAndCorruptChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("zephyr-data-availability/"), 1000)
	commitment, chunks, samples, err := EncodeBlob(payload, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if commitment.ChunkCount != 12 || commitment.OriginalSize != uint64(len(payload)) {
		t.Fatalf("unexpected commitment: %+v", commitment)
	}
	working := cloneChunks(chunks)
	working[1] = nil
	working[5] = nil
	working[10][0] ^= 0xff
	recovered, err := ReconstructBlob(commitment, working, samples)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered, payload) {
		t.Fatal("reconstructed payload differs")
	}
}

func TestErasureCodingRejectsInsufficientAuthenticatedChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("z"), 8192)
	commitment, chunks, samples, err := EncodeBlob(payload, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	working := cloneChunks(chunks)
	working[0] = nil
	working[1] = nil
	working[2] = nil
	if _, err := ReconstructBlob(commitment, working, samples); err != ErrReconstruction {
		t.Fatalf("expected reconstruction failure, got %v", err)
	}
}

func TestCitizenSampleDetectsWithholdingAndTamper(t *testing.T) {
	payload := []byte("citizen bounded sample")
	commitment, chunks, samples, err := EncodeBlob(payload, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifySample(commitment, samples[3], chunks[3]) {
		t.Fatal("valid citizen sample rejected")
	}
	tampered := append([]byte(nil), chunks[3]...)
	tampered[0] ^= 1
	if VerifySample(commitment, samples[3], tampered) {
		t.Fatal("tampered citizen sample accepted")
	}
}
