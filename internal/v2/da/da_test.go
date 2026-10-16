package da

import "testing"

func TestSampleVerification(t *testing.T) {
	chunks := [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")}
	commitment, samples, err := CommitChunks(chunks, 2, 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifySample(commitment, samples[2], chunks[2]) {
		t.Fatal("sample failed")
	}
	if VerifySample(commitment, samples[2], []byte("tampered")) {
		t.Fatal("tampered sample verified")
	}
}
