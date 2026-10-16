package economics

// Clone returns an independent copy of the shadow epoch engine, including the
// accepted monetary-state history anchor and prior ZCPI snapshot.
func (e *ShadowEpochEngine) Clone() *ShadowEpochEngine {
	if e == nil {
		return nil
	}
	out := *e
	if e.previous != nil {
		previous := *e.previous
		out.previous = &previous
	}
	return &out
}
