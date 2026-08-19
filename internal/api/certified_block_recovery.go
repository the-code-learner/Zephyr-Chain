package api

import (
	"errors"
	"fmt"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

func (s *Server) recoverCertifiedBlockFromPeers(primaryPeerURL string, height uint64, block ledger.Block) error {
	transport, ok := s.transport.(certifiedBlockEvidenceTransport)
	if !ok {
		return fmt.Errorf("peer transport does not support certified block evidence")
	}

	peerURLs := make([]string, 0, len(s.config.PeerURLs)+1)
	seenPeers := make(map[string]struct{}, len(s.config.PeerURLs)+1)
	appendPeer := func(peerURL string) {
		if peerURL == "" {
			return
		}
		if _, exists := seenPeers[peerURL]; exists {
			return
		}
		seenPeers[peerURL] = struct{}{}
		peerURLs = append(peerURLs, peerURL)
	}
	appendPeer(primaryPeerURL)
	for _, peerURL := range s.config.PeerURLs {
		appendPeer(peerURL)
	}

	var merged ledger.CertifiedBlockEvidence
	var lastErr error = ledger.ErrCertifiedEvidenceQuorum
	for _, peerURL := range peerURLs {
		fragment, err := transport.FetchBlockEvidence(peerURL, height)
		if err != nil {
			lastErr = err
			continue
		}
		if err := mergeCertifiedBlockEvidence(&merged, fragment, block, s.config.ChainID); err != nil {
			// One malformed or conflicting peer fragment must not prevent recovery
			// from honest peers carrying compatible signed evidence.
			lastErr = err
			continue
		}
		if err := s.ledger.ImportBlockWithEvidence(block, merged); err != nil {
			if errors.Is(err, ledger.ErrCertifiedEvidenceQuorum) {
				lastErr = err
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
}

func mergeCertifiedBlockEvidence(dst *ledger.CertifiedBlockEvidence, fragment ledger.CertifiedBlockEvidence, block ledger.Block, chainID string) error {
	proposal := fragment.Proposal
	if err := proposal.ValidateForChain(chainID); err != nil {
		return fmt.Errorf("invalid certified block proposal: %w", err)
	}
	if proposal.Height != block.Height || proposal.BlockHash != block.Hash || proposal.StateRoot != block.StateRoot {
		return ledger.ErrCertifiedEvidenceInvalid
	}

	if dst.Proposal.Height == 0 {
		dst.Proposal = proposal
	} else if !sameCertifiedProposal(dst.Proposal, proposal) {
		return ledger.ErrCertifiedEvidenceInvalid
	}

	votesByValidator := make(map[string]consensus.Vote, len(dst.Votes)+len(fragment.Votes))
	for _, vote := range dst.Votes {
		votesByValidator[vote.Voter] = vote
	}
	for _, vote := range fragment.Votes {
		if err := vote.ValidateForChain(chainID); err != nil {
			return fmt.Errorf("invalid certified block vote: %w", err)
		}
		if vote.Height != proposal.Height || vote.Round != proposal.Round || vote.BlockHash != proposal.BlockHash {
			return ledger.ErrCertifiedEvidenceInvalid
		}
		if existing, ok := votesByValidator[vote.Voter]; ok {
			if !sameCertifiedVote(existing, vote) {
				return ledger.ErrCertifiedEvidenceInvalid
			}
			continue
		}
		votesByValidator[vote.Voter] = vote
	}

	dst.Votes = dst.Votes[:0]
	for _, vote := range votesByValidator {
		dst.Votes = append(dst.Votes, vote)
	}
	sort.Slice(dst.Votes, func(i, j int) bool { return dst.Votes[i].Voter < dst.Votes[j].Voter })
	return nil
}

func sameCertifiedProposal(left consensus.Proposal, right consensus.Proposal) bool {
	return left.ChainID == right.ChainID &&
		left.Domain == right.Domain &&
		left.Height == right.Height &&
		left.Round == right.Round &&
		left.BlockHash == right.BlockHash &&
		left.StateRoot == right.StateRoot &&
		left.Proposer == right.Proposer &&
		left.PublicKey == right.PublicKey &&
		left.Payload == right.Payload &&
		left.Signature == right.Signature
}

func sameCertifiedVote(left consensus.Vote, right consensus.Vote) bool {
	return left.ChainID == right.ChainID &&
		left.Domain == right.Domain &&
		left.Height == right.Height &&
		left.Round == right.Round &&
		left.BlockHash == right.BlockHash &&
		left.Voter == right.Voter &&
		left.PublicKey == right.PublicKey &&
		left.Payload == right.Payload &&
		left.Signature == right.Signature
}
