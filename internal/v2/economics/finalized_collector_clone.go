package economics

// Clone returns an independent deep copy of the finalized economics collector.
// Runtime owners use this to prevent external callers from mutating economic
// telemetry outside the node's synchronization boundary.
func (c *EpochCollector) Clone() *EpochCollector {
	return c.clone()
}
