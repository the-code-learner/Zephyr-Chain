package ledger

import "github.com/zephyr-chain/zephyr-chain/internal/consensus"

func (s *Store) ProposalAt(height uint64, round uint64) (*consensus.Proposal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	proposal := findProposal(s.proposals, height, round)
	if proposal == nil {
		return nil, false
	}
	return proposal, true
}

func (s *Store) HasVote(height uint64, round uint64, voter string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return hasVoteFromValidator(s.votes, height, round, voter)
}

func (s *Store) Certificate(height uint64, round uint64, blockHash string) (*CommitCertificate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	certificate := findCertificate(s.commitCertificates, height, round, blockHash)
	if certificate == nil {
		return nil, false
	}
	return certificate, true
}
