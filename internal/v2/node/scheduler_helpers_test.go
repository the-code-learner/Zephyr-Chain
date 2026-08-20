package node

import (
	"crypto/ecdsa"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
)

func schedulerCertificate(t *testing.T, runtime *Runtime, key *ecdsa.PrivateKey, validators v2consensus.ValidatorSet, candidate Candidate) v2consensus.Certificate {
	t.Helper()
	proposal, err := v2consensus.SignProposal(key, candidate.Header, 0)
	if err != nil {
		t.Fatal(err)
	}
	vote, err := v2consensus.SignVote(key, runtime.Network, candidate.Header.Height, 0, v2consensus.HeaderConsensusHash(candidate.Header))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := validators.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func commitSchedulerCandidateExpectError(t *testing.T, runtime *Runtime, key *ecdsa.PrivateKey, validators v2consensus.ValidatorSet, candidate Candidate) {
	t.Helper()
	certificate := schedulerCertificate(t, runtime, key, validators, candidate)
	if _, err := runtime.Commit(candidate, certificate, validators); err == nil {
		t.Fatal("candidate commit unexpectedly succeeded")
	}
}
