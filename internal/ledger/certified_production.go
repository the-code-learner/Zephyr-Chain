package ledger

import (
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func produceCertifiedBlockFromState(state persistedState, producedAt time.Time) (persistedState, Block, error) {
	state = normalizeState(state)

	proposal, err := proposalForProduction(state, producedAt, true)
	if err != nil {
		return state, Block{}, err
	}

	block := blockFromProposal(*proposal)
	if err := validateBlockConsensus(state, block, true); err != nil {
		return state, Block{}, err
	}

	nextState, err := importBlockIntoState(state, block)
	if err != nil {
		return state, Block{}, err
	}
	return nextState, block, nil
}

func proposalForProduction(state persistedState, producedAt time.Time, requireCertificate bool) (*consensus.Proposal, error) {
	state = normalizeState(state)
	nextHeight := uint64(len(state.Blocks) + 1)
	matchedProposal := false

	for index := len(state.Proposals) - 1; index >= 0; index-- {
		proposal := state.Proposals[index]
		if proposal.Height != nextHeight {
			continue
		}
		if !producedAt.IsZero() && !proposal.ProducedAt.Equal(producedAt) {
			continue
		}
		if len(proposal.Transactions) == 0 || len(proposal.Transactions) != len(proposal.TransactionIDs) {
			continue
		}

		matchedProposal = true
		if !requireCertificate || findCertificate(state.CommitCertificates, proposal.Height, proposal.Round, proposal.BlockHash) != nil {
			cloned := cloneProposal(proposal)
			return &cloned, nil
		}
	}

	if matchedProposal && requireCertificate {
		return nil, ErrConsensusCertificateRequired
	}
	return nil, ErrConsensusProposalRequired
}

func blockFromProposal(proposal consensus.Proposal) Block {
	block := Block{
		Height:           proposal.Height,
		PreviousHash:     proposal.PreviousHash,
		ProducedAt:       proposal.ProducedAt,
		TransactionCount: len(proposal.Transactions),
		TransactionIDs:   append([]string(nil), proposal.TransactionIDs...),
		Transactions:     append([]tx.Envelope(nil), proposal.Transactions...),
	}
	block.Hash = blockHash(block)
	return block
}
