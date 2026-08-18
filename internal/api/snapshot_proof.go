package api

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var errSnapshotSignerRequired = errors.New("snapshot serving requires an active validator signer")

func (s *Server) signedSnapshot() (ledger.Snapshot, error) {
	if s.identitySigner == nil {
		return ledger.Snapshot{}, errSnapshotSignerRequired
	}

	snapshot := s.ledger.Snapshot()
	proof, err := ledger.BuildSnapshotProofTemplate(snapshot, s.config.ChainID, s.identitySigner.validatorAddress)
	if err != nil {
		return ledger.Snapshot{}, err
	}
	proof.PublicKey = s.identitySigner.publicKey
	proof.Payload = proof.CanonicalPayload()
	proof.Signature, err = tx.SignPayload(s.identitySigner.privateKey, proof.Payload)
	if err != nil {
		return ledger.Snapshot{}, err
	}
	snapshot.Proof = proof
	return snapshot, nil
}
