# Runtime-certified witness reservation integration (P0 #55)

**Status: experimental non-signing adapter. Mainnet NO-GO.**

The `RuntimeWitnessFinalityVerifier` is constructed only if a runtime has a nonzero QC-certified history tip, no recovery-required state, a matching open certified history, and a separately maintained finality high-water ledger at the **exact same** (height, hash). It captures an immutable checkpoint, rejecting later, older, foreign or conflicting values.

The `ReserveRuntimeVerifiedWitnessTip` helper only permits the exact verifier instance configured on the journal and reserves the sealed tip. No arbitrary RPC checkpoint is accepted through this integration. This helper does not emit signatures or manage private keys.

**Security limitations:** The adapter trusts that the runtime's genesis and validator root were independently pinned and its recovered QC history was validated by the existing production recovery path. It does not independently rerun that history; a same-machine local high-water file is not remote anti-rollback evidence. A runtime that has no authenticated external history provenance cannot satisfy the full independent witness threat model even when these local checks pass. Production requires a separately deployed QC verifier with authentic operator-pinned genesis, globally fenced HSM/remote signing custody, snapshot rollback protection and independent reviewer sign-off.

## Required follow-up

1. Create an independently operated process which replays QC-certified history with validator transitions and checks authenticated remote publication, not merely local runtime state.
2. Add cross-host fenced signing key custody. Never replace it with a directory flock.
3. Exercise authenticated genesis mismatch, history tampering, disconnections, snapshot restore, key rotation and ambiguous HSM sign responses.
4. Publish multi-host evidence and an independent security audit linked to #55 / #42. Only those verified deliverables can affect mainnet GO.
