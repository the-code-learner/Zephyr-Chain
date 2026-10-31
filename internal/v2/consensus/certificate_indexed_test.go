package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestV2IndexedCertificateVerificationRejectsTampering(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("indexed-certificate")))
	hash := types.HashBytes("header", []byte("indexed-certificate"))
	set := ValidatorSet{Network: network}
	votes := make([]Vote, 0, 7)
	for i := 0; i < 7; i++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		id := types.ValidatorIDFromPublicKey(pub)
		set.Validators = append(set.Validators, Validator{ID: id, PublicKey: pub, Power: 10})
		vote, err := SignVote(key, network, 1, 0, hash)
		if err != nil {
			t.Fatal(err)
		}
		votes = append(votes, vote)
	}
	good := Certificate{Network: network, Height: 1, HeaderHash: hash, Votes: append([]Vote(nil), votes[:5]...)}
	if err := set.VerifyCertificate(good); err != nil {
		t.Fatalf("valid quorum rejected: %v", err)
	}
	if err := set.VerifyCertificate(Certificate{Network: network, Height: 1, HeaderHash: hash, Votes: votes[:4]}); err != ErrInsufficientPower {
		t.Fatalf("subquorum accepted or unexpected error: %v", err)
	}
	tampered := good
	tampered.Votes = append([]Vote(nil), good.Votes...)
	tampered.Votes[0].Signature = append([]byte(nil), good.Votes[0].Signature...)
	tampered.Votes[0].Signature[0] ^= 0x80
	if err := set.VerifyCertificate(tampered); err != ErrCertificate {
		t.Fatalf("tampered P-256 signature accepted: %v", err)
	}
	tampered = good
	tampered.Votes = append([]Vote(nil), good.Votes...)
	tampered.Votes[0].PublicKey = append([]byte(nil), good.Votes[1].PublicKey...)
	if err := set.VerifyCertificate(tampered); err != ErrCertificate {
		t.Fatalf("substituted public key accepted: %v", err)
	}
}
