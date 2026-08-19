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

	bundles := make(map[string]ledger.CertifiedBlockEvidence)
	var lastErr error = ledger.ErrCertifiedEvidenceQuorum
	for _, peerURL := range peerURLs {
		fragments, err := transport.FetchBlockEvidence(peerURL, height)
		if err != nil {
			lastErr = err
			continue
		}
		for _, fragment := range fragments {
			proposal := fragment.Proposal
			if proposal.Height != block.Height || proposal.BlockHash != block.Hash || proposal.StateRoot != block.StateRoot {
				continue
			}
			key := certifiedProposalKey(proposal)
			bundle := bundles[key]
			if err := mergeCertifiedBlockEvidence(&bundle, fragment, block, s.config.ChainID); err != nil {
				// Malformed/conflicting evidence from one peer must not poison a
				// compatible bundle collected from other peers.
				lastErr = err
				continue
			}
			bundles[key] = bundle

			if err := s.ledger.ImportBlockWithEvidence(block, bundle); err != nil {
				if errors.Is(err, ledger.ErrCertifiedEvidenceQuorum) {
					lastErr = err
					continue
				}
				// Another valid round/proposal fragment may still provide the
				// evidence for the committed block, so keep collecting unless the
				// error is a block/state invariant failure.
				if errors.Is(err, ledger.ErrCertifiedEvidenceInvalid) || errors.Is(err, ledger.ErrConflictingProposal) || errors.Is(err, ledger.ErrConflictingVote) {
					lastErr = err
					continue
				}
				return err
			}
			return nil
		}
	}
	return lastErr
}

func certifiedProposalKey(proposal consensus.Proposal) string {
	// Canonical payload already binds chain/domain/height/round/block/state and
	// proposer identity. Public key is included explicitly; the ECDSA signature
	// bytes are not part of the grouping key because two valid signatures over
	// the same canonical proposal are semantically equivalent.
	return proposal.Payload + "\x00" + proposal.Proposer + "\x00" + proposal.PublicKey
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
		left.Payload == right.Payload
}

func sameCertifiedVote(left consensus.Vote, right consensus.Vote) bool {
	return left.ChainID == right.ChainID &&
		left.Domain == right.Domain &&
		left.Height == right.Height &&
		left.Round == right.Round &&
		left.BlockHash == right.BlockHash &&
		left.Voter == right.Voter &&
		left.PublicKey == right.PublicKey &&
		left.Payload == right.Payload
}
