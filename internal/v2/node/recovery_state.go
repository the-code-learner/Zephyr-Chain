package node

// RecoveryRequired reports whether a state backend returned an uncertain
// result during a consensus-finalized apply. Once set, the runtime refuses to
// build or commit further candidates until the normal recovery path reconstructs
// a safe state anchor.
func (r *Runtime) RecoveryRequired() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recoveryRequired
}
