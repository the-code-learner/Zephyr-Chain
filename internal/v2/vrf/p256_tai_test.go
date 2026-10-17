package vrf

import (
	"crypto/elliptic"
	"encoding/hex"
	"errors"
	"testing"
)

func TestP256SHA256TAIRFC9381Vectors(t *testing.T) {
	publicKey := mustHex(t, "0360fed4ba255a9d31c961eb74c6356d68c049b8923b61fa6ce669622e60f29fb6")
	vectors := []struct {
		name   string
		alpha  string
		proof  string
		output string
	}{
		{
			name:   "RFC9381-example-10-sample",
			alpha:  "73616d706c65",
			proof:  "035b5c726e8c0e2c488a107c600578ee75cb702343c153cb1eb8dec77f4b5071b4a53f0a46f018bc2c56e58d383f2305e0975972c26feea0eb122fe7893c15af376b33edf7de17c6ea056d4d82de6bc02f",
			output: "a3ad7b0ef73d8fc6655053ea22f9bede8c743f08bbed3d38821f0e16474b505e",
		},
		{
			name:   "RFC9381-example-11-test",
			alpha:  "74657374",
			proof:  "034dac60aba508ba0c01aa9be80377ebd7562c4a52d74722e0abae7dc3080ddb56c19e067b15a8a8174905b13617804534214f935b94c2287f797e393eb0816969d864f37625b443f30f1a5a33f2b3c854",
			output: "a284f94ceec2ff4b3794629da7cbafa49121972671b466cab4ce170aa365f26d",
		},
	}
	for _, vector := range vectors {
		t.Run(vector.name, func(t *testing.T) {
			output, err := VerifyP256SHA256TAI(publicKey, mustHex(t, vector.alpha), mustHex(t, vector.proof))
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(output[:]); got != vector.output {
				t.Fatalf("output = %s, want %s", got, vector.output)
			}
		})
	}
}

func TestP256SHA256TAIEncodeToCurveMatchesRFC9381Example10(t *testing.T) {
	publicKey := mustHex(t, "0360fed4ba255a9d31c961eb74c6356d68c049b8923b61fa6ce669622e60f29fb6")
	point, err := encodeToCurveP256TAI(publicKey, []byte("sample"))
	if err != nil {
		t.Fatal(err)
	}
	compressed := ellipticMarshalCompressed(point)
	if got, want := hex.EncodeToString(compressed), "0272a877532e9ac193aff4401234266f59900a4a9e3fc3cfc6a4b7e467a15d06d4"; got != want {
		t.Fatalf("H = %s, want %s", got, want)
	}
}

func TestP256SHA256TAIRejectsSubstitution(t *testing.T) {
	publicKey := mustHex(t, "0360fed4ba255a9d31c961eb74c6356d68c049b8923b61fa6ce669622e60f29fb6")
	proof := mustHex(t, "035b5c726e8c0e2c488a107c600578ee75cb702343c153cb1eb8dec77f4b5071b4a53f0a46f018bc2c56e58d383f2305e0975972c26feea0eb122fe7893c15af376b33edf7de17c6ea056d4d82de6bc02f")

	if _, err := VerifyP256SHA256TAI(publicKey, []byte("different"), proof); !errors.Is(err, ErrVerifyFailed) {
		t.Fatalf("alpha substitution error = %v, want %v", err, ErrVerifyFailed)
	}

	tamperedProof := append([]byte(nil), proof...)
	tamperedProof[len(tamperedProof)-1] ^= 1
	if _, err := VerifyP256SHA256TAI(publicKey, []byte("sample"), tamperedProof); err == nil {
		t.Fatal("tampered proof was accepted")
	}

	tamperedKey := append([]byte(nil), publicKey...)
	tamperedKey[len(tamperedKey)-1] ^= 1
	if _, err := VerifyP256SHA256TAI(tamperedKey, []byte("sample"), proof); err == nil {
		t.Fatal("public-key substitution was accepted")
	}
}

func TestP256SHA256TAIRejectsMalformedLengthsAndResponse(t *testing.T) {
	publicKey := mustHex(t, "0360fed4ba255a9d31c961eb74c6356d68c049b8923b61fa6ce669622e60f29fb6")
	proof := mustHex(t, "035b5c726e8c0e2c488a107c600578ee75cb702343c153cb1eb8dec77f4b5071b4a53f0a46f018bc2c56e58d383f2305e0975972c26feea0eb122fe7893c15af376b33edf7de17c6ea056d4d82de6bc02f")

	if _, err := VerifyP256SHA256TAI(publicKey[:32], []byte("sample"), proof); !errors.Is(err, ErrInvalidPublicKey) {
		t.Fatalf("short key error = %v", err)
	}
	if _, err := VerifyP256SHA256TAI(publicKey, []byte("sample"), proof[:80]); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("short proof error = %v", err)
	}
	if _, err := VerifyP256SHA256TAI(publicKey, make([]byte, MaxInputBytes+1), proof); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversize alpha error = %v", err)
	}

	badResponse := append([]byte(nil), proof...)
	for i := 49; i < len(badResponse); i++ {
		badResponse[i] = 0xff
	}
	if _, err := VerifyP256SHA256TAI(publicKey, []byte("sample"), badResponse); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("out-of-range response error = %v, want %v", err, ErrInvalidProof)
	}
}

func mustHex(t *testing.T, value string) []byte {
	t.Helper()
	output, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func ellipticMarshalCompressed(point ecPoint) []byte {
	return elliptic.MarshalCompressed(elliptic.P256(), point.x, point.y)
}
