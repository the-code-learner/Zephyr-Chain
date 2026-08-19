package economics

// Clone returns an independent deep copy of the finalized economics collector.
// Runtime owners use this to prevent external callers from mutating economic
// telemetry or its protocol configuration outside the node synchronization
// boundary.
func (c *EpochCollector) Clone() *EpochCollector {
	out := c.clone()
	if out == nil {
		return nil
	}
	out.config.InitialCirculatingSupply = copyShardMap(c.config.InitialCirculatingSupply)
	out.config.OpeningComputeBacklog = copyShardMap(c.config.OpeningComputeBacklog)
	out.config.ResourceCapacityPerBlock = copyShardMap(c.config.ResourceCapacityPerBlock)
	registry, err := cloneCollectorRegistry(c.config.WorkRegistry)
	if err != nil {
		return nil
	}
	out.config.WorkRegistry = registry
	return out
}

func (c *EpochCollector) Epoch() uint64 {
	if c == nil {
		return 0
	}
	return c.config.Epoch
}
