package ledger

import (
	"errors"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
)

var ErrConsensusRoundMismatch = errors.New("consensus message round does not match the active round")

type ConsensusRoundState struct {
	Height    uint64    `json:"height"`
	Round     uint64    `json:"round"`
	StartedAt time.Time `json:"startedAt,omitempty"`
}

func normalizeConsensusRoundState(roundState ConsensusRoundState, blocks []Block) ConsensusRoundState {
	nextHeight := uint64(len(blocks) + 1)
	if roundState.Height != nextHeight {
		return ConsensusRoundState{Height: nextHeight}
	}
	return roundState
}

func cloneConsensusRoundState(roundState ConsensusRoundState) ConsensusRoundState {
	return roundState
}

func proposerForHeightRound(validators []dpos.Validator, height uint64, round uint64) string {
	if len(validators) == 0 || height == 0 {
		return ""
	}
	index := int((height - 1 + round) % uint64(len(validators)))
	return validators[index].Address
}
