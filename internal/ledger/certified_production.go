package ledger

import (
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func produceCertifiedBlockFromState(state persistedState, producedAt time.Time, chainID string) (persistedState, Block, error) {
	state = normalizeState(state)

	proposal, err := proposalForProduction(state, producedAt, true)
	if err != nil {
		return state, Block{}, err
	}
	if proposal.ChainID != chainID {
		return state, Block{}, ErrInvalidBlock
	}

	block := blockFromProposal(*proposal)
	if err := validateBlockConsensus(state, block, true); err != nil {
		return state, Block{}, err
	}

	nextState, err := importBlockIntoState(state, block, chainID)
	if err != nil {
		return state, Block{}, err
	}
	return nextState, block, nil
}

func proposalForProduction(state persistedState, producedAt time.Time, requireCertificate bool) (*consensus.Proposal, error) {
	state = normalizeState(state)
	nextHeight := uint64(len(state.Blocks) + 1)
	matchedHeight := false
	matchedProducedAt := false

	for index := len(state.Proposals) - 1; index >= 0; index-- {
		proposal := state.Proposals[index]
		if proposal.Height != nextHeight {
			continue
		}
		matchedHeight = true
		if !producedAt.IsZero() && !proposal.ProducedAt.Equal(producedAt) {
			continue
		}
		matchedProducedAt = true
		if len(proposal.Transactions) == 0 || len(proposal.Transactions) != len(proposal.TransactionIDs) {
			continue
		}

		if !requireCertificate || findCertificate(state.CommitCertificates, proposal.Height, proposal.Round, proposal.BlockHash) != nil {
			cloned := cloneProposal(proposal)
			return &cloned, nil
		}
	}

	switch {
	case matchedProducedAt && requireCertificate:
		return nil, ErrConsensusCertificateRequired
	case matchedHeight && !producedAt.IsZero():
		return nil, ErrConsensusTemplateMismatch
	case matchedHeight && requireCertificate:
		return nil, ErrConsensusCertificateRequired
	default:
		return nil, ErrConsensusProposalRequired
	}
}

func blockFromProposal(proposal consensus.Proposal) Block {
	block := Block{
		ChainID:          proposal.ChainID,
		Height:           proposal.Height,
		PreviousHash:     proposal.PreviousHash,
		StateRoot:        proposal.StateRoot,
		ProducedAt:       proposal.ProducedAt,
		TransactionCount: len(proposal.Transactions),
		TransactionIDs:   append([]string(nil), proposal.TransactionIDs...),
		Transactions:     append([]tx.Envelope(nil), proposal.Transactions...),
	}
	block.Hash = blockHash(block)
	return block
}
