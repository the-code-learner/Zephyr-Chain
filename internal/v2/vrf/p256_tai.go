package vrf

import (
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math/big"
)

const (
	// SuiteP256SHA256TAI is the RFC 9381 ECVRF-P256-SHA256-TAI suite byte.
	SuiteP256SHA256TAI byte = 0x01

	P256PublicKeySize = 33
	P256ProofSize     = 81
	OutputSize        = 32

	// Protocol callers use a fixed-size canonical committee input. Keep the
	// low-level verifier bounded as well so an untrusted alpha cannot cause
	// 256 repeated hashes over an arbitrarily large byte string.
	MaxInputBytes = 4 << 10
)

var (
	ErrInvalidPublicKey = errors.New("invalid RFC 9381 P-256 VRF public key")
	ErrInvalidProof     = errors.New("invalid RFC 9381 P-256 VRF proof")
	ErrInvalidInput     = errors.New("invalid RFC 9381 P-256 VRF input")
	ErrVerifyFailed     = errors.New("RFC 9381 P-256 VRF verification failed")
)

type Output [OutputSize]byte

type ecPoint struct {
	x *big.Int
	y *big.Int
}

// VerifyP256SHA256TAI verifies RFC 9381 ECVRF-P256-SHA256-TAI with
// validate_key=true and returns beta only after the proof challenge verifies.
//
// This package intentionally implements verification only. Protocol v2 does
// not yet authorize a VRF producer, aggregate a randomness beacon, select a
// live committee, or expose a consensus transition that consumes this output.
func VerifyP256SHA256TAI(publicKey, alpha, proof []byte) (Output, error) {
	var output Output
	if len(publicKey) != P256PublicKeySize {
		return output, ErrInvalidPublicKey
	}
	if len(alpha) > MaxInputBytes {
		return output, ErrInvalidInput
	}
	if len(proof) != P256ProofSize {
		return output, ErrInvalidProof
	}

	curve := elliptic.P256()
	publicX, publicY := elliptic.UnmarshalCompressed(curve, publicKey)
	if publicX == nil || isIdentity(publicX, publicY) {
		return output, ErrInvalidPublicKey
	}
	gammaX, gammaY := elliptic.UnmarshalCompressed(curve, proof[:33])
	if gammaX == nil || isIdentity(gammaX, gammaY) {
		return output, ErrInvalidProof
	}

	challenge := new(big.Int).SetBytes(proof[33:49])
	response := new(big.Int).SetBytes(proof[49:81])
	if response.Cmp(curve.Params().N) >= 0 {
		return output, ErrInvalidProof
	}

	hashedInput, err := encodeToCurveP256TAI(publicKey, alpha)
	if err != nil {
		return output, err
	}

	u := subPoints(
		scalarBaseMult(curve, response),
		scalarMult(curve, ecPoint{x: publicX, y: publicY}, challenge),
	)
	v := subPoints(
		scalarMult(curve, hashedInput, response),
		scalarMult(curve, ecPoint{x: gammaX, y: gammaY}, challenge),
	)
	if isIdentity(u.x, u.y) || isIdentity(v.x, v.y) {
		return output, ErrVerifyFailed
	}

	expectedChallenge := challengeP256(curve, []ecPoint{
		{x: publicX, y: publicY},
		hashedInput,
		{x: gammaX, y: gammaY},
		u,
		v,
	})
	if subtle.ConstantTimeCompare(expectedChallenge[:16], proof[33:49]) != 1 {
		return output, ErrVerifyFailed
	}

	// RFC 9381 ECVRF_proof_to_hash for the P-256 ciphersuite. P-256 has
	// cofactor 1, so cofactor*Gamma is Gamma.
	hash := sha256.New()
	_, _ = hash.Write([]byte{SuiteP256SHA256TAI, 0x03})
	_, _ = hash.Write(elliptic.MarshalCompressed(curve, gammaX, gammaY))
	_, _ = hash.Write([]byte{0x00})
	copy(output[:], hash.Sum(nil))
	return output, nil
}

func encodeToCurveP256TAI(publicKey, alpha []byte) (ecPoint, error) {
	curve := elliptic.P256()
	for counter := 0; counter < 256; counter++ {
		hash := sha256.New()
		_, _ = hash.Write([]byte{SuiteP256SHA256TAI, 0x01})
		_, _ = hash.Write(publicKey)
		_, _ = hash.Write(alpha)
		_, _ = hash.Write([]byte{byte(counter), 0x00})

		digest := hash.Sum(nil)
		compressed := make([]byte, 33)
		compressed[0] = 0x02
		copy(compressed[1:], digest)

		x, y := elliptic.UnmarshalCompressed(curve, compressed)
		if x != nil && !isIdentity(x, y) {
			return ecPoint{x: x, y: y}, nil
		}
	}
	return ecPoint{}, ErrVerifyFailed
}

func challengeP256(curve elliptic.Curve, points []ecPoint) [32]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte{SuiteP256SHA256TAI, 0x02})
	for _, point := range points {
		_, _ = hash.Write(elliptic.MarshalCompressed(curve, point.x, point.y))
	}
	_, _ = hash.Write([]byte{0x00})

	var output [32]byte
	copy(output[:], hash.Sum(nil))
	return output
}

func scalarBaseMult(curve elliptic.Curve, scalar *big.Int) ecPoint {
	if scalar.Sign() == 0 {
		return identityPoint()
	}
	x, y := curve.ScalarBaseMult(scalarBytes(scalar))
	return ecPoint{x: x, y: y}
}

func scalarMult(curve elliptic.Curve, point ecPoint, scalar *big.Int) ecPoint {
	if scalar.Sign() == 0 {
		return identityPoint()
	}
	x, y := curve.ScalarMult(point.x, point.y, scalarBytes(scalar))
	return ecPoint{x: x, y: y}
}

func subPoints(left, right ecPoint) ecPoint {
	if isIdentity(right.x, right.y) {
		return left
	}
	if isIdentity(left.x, left.y) {
		return ecPoint{x: new(big.Int).Set(right.x), y: negateY(right.y)}
	}
	x, y := elliptic.P256().Add(left.x, left.y, right.x, negateY(right.y))
	return ecPoint{x: x, y: y}
}

func scalarBytes(value *big.Int) []byte {
	raw := value.Bytes()
	output := make([]byte, 32)
	copy(output[32-len(raw):], raw)
	return output
}

func negateY(y *big.Int) *big.Int {
	if y == nil || y.Sign() == 0 {
		return new(big.Int)
	}
	return new(big.Int).Sub(elliptic.P256().Params().P, y)
}

func identityPoint() ecPoint {
	return ecPoint{x: new(big.Int), y: new(big.Int)}
}

func isIdentity(x, y *big.Int) bool {
	return x == nil || y == nil || (x.Sign() == 0 && y.Sign() == 0)
}
