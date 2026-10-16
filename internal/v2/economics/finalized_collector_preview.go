package economics

import "github.com/zephyr-chain/zephyr-chain/internal/v2/types"

// PreviewFinalizedBlock evaluates a finalized block observation without
// advancing the receiver. A node can prepare this preview before durable state
// Apply and promote it only after the block transition succeeds.
func (c *EpochCollector) PreviewFinalizedBlock(height uint64, observations map[uint32]FinalizedShardObservation) (*EpochCollector, error) {
	if c == nil {
		return nil, ErrFinalizedEconomics
	}
	preview := c.clone()
	if preview == nil {
		return nil, ErrFinalizedEconomics
	}
	if err := preview.observeFinalizedBlock(height, observations); err != nil {
		return nil, err
	}
	return preview, nil
}

func (c *EpochCollector) ShardCount() uint32 {
	if c == nil {
		return 0
	}
	return c.config.ShardCount
}

func (c *EpochCollector) NativeTokenID() types.TokenID {
	if c == nil {
		return types.TokenID{}
	}
	return c.config.NativeToken
}
