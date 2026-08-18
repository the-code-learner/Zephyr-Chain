package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var (
	ErrInvalidSnapshot        = errors.New("invalid peer snapshot")
	ErrSnapshotChainMismatch  = errors.New("snapshot chain ID does not match local chain")
	ErrSnapshotProofInvalid   = errors.New("invalid snapshot proof")
	ErrSnapshotQuorumRequired = errors.New("snapshot does not have trusted validator quorum")
)

type SnapshotProof struct {
	BlockHash           string `json:"blockHash"`
	ChainID             string `json:"chainId"`
	Domain              string `json:"domain"`
	Height              uint64 `json:"height"`
	Payload             string `json:"payload"`
	PublicKey           string `json:"publicKey"`
	Signature           string `json:"signature"`
	Signer              string `json:"signer"`
	StateCommitment     string `json:"stateCommitment"`
	ValidatorSetVersion uint64 `json:"validatorSetVersion"`
}

type canonicalSnapshotProof struct {
	BlockHash           string `json:"blockHash"`
	ChainID             string `json:"chainId"`
	Domain              string `json:"domain"`
	Height              uint64 `json:"height"`
	Signer              string `json:"signer"`
	StateCommitment     string `json:"stateCommitment"`
	ValidatorSetVersion uint64 `json:"validatorSetVersion"`
}

func (p SnapshotProof) CanonicalPayload() string {
	payload, _ := json.Marshal(canonicalSnapshotProof{
		BlockHash:           p.BlockHash,
		ChainID:             strings.TrimSpace(p.ChainID),
		Domain:              strings.TrimSpace(p.Domain),
		Height:              p.Height,
		Signer:              p.Signer,
		StateCommitment:     p.StateCommitment,
		ValidatorSetVersion: p.ValidatorSetVersion,
	})
	return string(payload)
}

func BuildSnapshotProofTemplate(snapshot Snapshot, chainID string, signer string) (SnapshotProof, error) {
	if err := ValidateSnapshotCommittedState(snapshot, chainID); err != nil {
		return SnapshotProof{}, err
	}
	latest := snapshot.Blocks[len(snapshot.Blocks)-1]
	return SnapshotProof{
		BlockHash:           latest.Hash,
		ChainID:             strings.TrimSpace(chainID),
		Domain:              protocol.SnapshotDomain,
		Height:              latest.Height,
		Signer:              signer,
		StateCommitment:     latest.StateRoot,
		ValidatorSetVersion: snapshot.ValidatorSnapshot.Version,
	}, nil
}

func ValidateSnapshotCommittedState(snapshot Snapshot, chainID string) error {
	chainID = strings.TrimSpace(chainID)
	if err := protocol.ValidateChainID(chainID); err != nil {
		return ErrSnapshotChainMismatch
	}
	if len(snapshot.Blocks) == 0 {
		return fmt.Errorf("%w: no committed blocks", ErrInvalidSnapshot)
	}

	seenTransactions := make(map[string]struct{})
	previousHash := ""
	for index, block := range snapshot.Blocks {
		expectedHeight := uint64(index + 1)
		if block.ChainID != chainID {
			return ErrSnapshotChainMismatch
		}
		if block.Height != expectedHeight || block.PreviousHash != previousHash || block.StateRoot == "" {
			return ErrInvalidSnapshot
		}
		if block.TransactionCount != len(block.Transactions) || len(block.TransactionIDs) != len(block.Transactions) {
			return ErrInvalidSnapshot
		}
		for txIndex, envelope := range block.Transactions {
			if err := envelope.ValidateForChain(chainID); err != nil {
				return ErrInvalidSnapshot
			}
			id := tx.ID(envelope)
			if block.TransactionIDs[txIndex] != id {
				return ErrInvalidSnapshot
			}
			if _, exists := seenTransactions[id]; exists {
				return ErrInvalidSnapshot
			}
			seenTransactions[id] = struct{}{}
		}
		if block.Hash != blockHash(block) {
			return ErrInvalidSnapshot
		}
		previousHash = block.Hash
	}

	latest := snapshot.Blocks[len(snapshot.Blocks)-1]
	root, err := StateRoot(chainID, snapshot.Accounts, snapshot.ValidatorSnapshot)
	if err != nil || root != latest.StateRoot {
		return ErrInvalidStateRoot
	}
	if _, ok := sumValidatorVotingPower(snapshot.ValidatorSnapshot.Validators); !ok {
		return ErrVotingPowerOverflow
	}
	return nil
}

func ValidateSnapshotProof(snapshot Snapshot, chainID string, proof SnapshotProof, trusted ValidatorSnapshot) (uint64, error) {
	if err := ValidateSnapshotCommittedState(snapshot, chainID); err != nil {
		return 0, err
	}
	latest := snapshot.Blocks[len(snapshot.Blocks)-1]
	if strings.TrimSpace(proof.ChainID) != strings.TrimSpace(chainID) {
		return 0, ErrSnapshotChainMismatch
	}
	if proof.Domain != protocol.SnapshotDomain || proof.Height != latest.Height || proof.BlockHash != latest.Hash || proof.StateCommitment != latest.StateRoot || proof.ValidatorSetVersion != snapshot.ValidatorSnapshot.Version {
		return 0, ErrSnapshotProofInvalid
	}
	if proof.Signer == "" || proof.Payload == "" || proof.PublicKey == "" || proof.Signature == "" || proof.Payload != proof.CanonicalPayload() {
		return 0, ErrSnapshotProofInvalid
	}
	address, err := tx.DeriveAddressFromPublicKey(proof.PublicKey)
	if err != nil || address != proof.Signer {
		return 0, ErrSnapshotProofInvalid
	}
	if err := tx.VerifySignature(proof.PublicKey, proof.Payload, proof.Signature); err != nil {
		return 0, ErrSnapshotProofInvalid
	}

	trusted = normalizeValidatorSnapshot(trusted)
	for _, validator := range trusted.Validators {
		if validator.Address == proof.Signer {
			return validator.VotingPower, nil
		}
	}
	return 0, ErrSnapshotProofInvalid
}

func ValidateSnapshotQuorum(snapshot Snapshot, chainID string, proofs []SnapshotProof, trusted ValidatorSnapshot) error {
	trusted = normalizeValidatorSnapshot(trusted)
	total := totalVotingPower(trusted)
	quorum := quorumVotingPower(total)
	if total == 0 || quorum == 0 {
		return ErrSnapshotQuorumRequired
	}

	seen := make(map[string]struct{})
	var signedPower uint64
	for _, proof := range proofs {
		if _, exists := seen[proof.Signer]; exists {
			continue
		}
		power, err := ValidateSnapshotProof(snapshot, chainID, proof, trusted)
		if err != nil {
			continue
		}
		next, ok := addUint64(signedPower, power)
		if !ok {
			return ErrVotingPowerOverflow
		}
		signedPower = next
		seen[proof.Signer] = struct{}{}
	}
	if signedPower < quorum {
		return ErrSnapshotQuorumRequired
	}
	return nil
}

func (s *Store) RestoreQuorumSnapshot(snapshot Snapshot, chainID string, proofs []SnapshotProof, trusted ValidatorSnapshot, now time.Time) error {
	if err := ValidateSnapshotQuorum(snapshot, chainID, proofs, trusted); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	localState := s.snapshotLocked()

	incoming := persistedFromSnapshot(snapshot)
	incoming.Mempool = make([]MempoolEntry, 0)
	incoming.AppliedFundingIDs = make([]string, 0)
	incoming.RoundState = ConsensusRoundState{Height: uint64(len(incoming.Blocks) + 1)}
	incoming.Proposals = make([]consensus.Proposal, 0)
	incoming.Votes = make([]VoteRecord, 0)
	incoming.CommitCertificates = make([]CommitCertificate, 0)
	incoming.ConsensusActions = normalizeConsensusActions(localState.ConsensusActions)
	incoming.ConsensusDiagnostics = normalizeConsensusDiagnostics(localState.ConsensusDiagnostics)
	incoming.PeerSyncIncidents = normalizePeerSyncIncidents(localState.PeerSyncIncidents)
	incoming.CommittedTransactionIDs = committedIDsFromBlocks(incoming.Blocks)
	incoming = completeConsensusActionsForHeightInState(incoming, uint64(len(incoming.Blocks)), now.UTC(), "quorum-validated state restored from peer snapshot")
	if err := s.writeState(incoming); err != nil {
		return err
	}
	s.applyStateLocked(incoming)
	return nil
}

func committedIDsFromBlocks(blocks []Block) []string {
	ids := make([]string, 0)
	for _, block := range blocks {
		ids = append(ids, block.TransactionIDs...)
	}
	return uniqueSortedStrings(ids)
}
