# Protocol v2 — Finalized provider evidence set commitment (P3.7)

This slice builds a deterministic, domain-separated **commitment** over the
complete P3.6 evidence records for one accepted finalized compute settlement.

It does **not** define a ContributionScore, reward multiplier, ranking,
eligibility threshold, validator weight or governance weight.

## Source and API

Call `DeriveFinalizedProviderComputeEvidence(transaction, result, registry)`
against a finalized transaction/result pair whose finality and WorkRegistry
context have already been independently established. Only then pass the
returned records to
`HashFinalizedProviderComputeEvidenceSet(records)`.

The set hash is **not** a finality proof. It does not authenticate an
untrusted RPC response merely because the response includes this hash.

## Canonical commitment

- Version: `1`; domain:
  `zephyr/finalized-provider-compute-evidence-set/v1`.
- Encode the version, provider-record count and each length-prefixed
  `FinalizedProviderComputeEvidence.CanonicalBytes()`.
- Sort records lexicographically by provider `AccountID`; input order
  cannot change the commitment and the input slice is not modified.
- Reject missing provider records: count must equal the declared
  `PaidProviderCount`.
- Reject duplicate providers and records with invalid fields.
- Require every record to commit to the exact same job, owner, workload,
  approved benchmark/class/units/vector, verification mode, accepted result
  root, pre-settlement job-state hash, settlement-receipt hash, WorkRegistry
  hash and assigned/paid provider counts.
- Preserve individual provider identities and exact payments in the
  per-record commitments.

The set is scoped to **one settlement**, not an epoch or a whole network.
Neither finalized height nor network ID can be inferred from a hash alone;
finality and network isolation must be established by the caller before the
derivation step.

## Replicas, payments and missing evidence

For replicated-majority settlement the accepted paid providers may be a
strict subset of the assigned providers; the minority/slashed providers are
not inserted into this set. `PaidProviderCount` is the set cardinality,
not the assignment count.

This API intentionally returns no sum of payments, work units or
cross-provider credit. Multiple accepted replicas can carry the same
WorkSpec. Deducing how much *unique* contribution that represents requires
a separately specified policy and adversarial simulations.

An empty set is rejected, not interpreted as zero contribution. Missing
or unverifiable evidence must stay explicitly unavailable in any future
epoch-level aggregation.

## Non-activation

The helper is pure and has no consumer in consensus, monetary state,
staking, governance, committee selection, rewards or lending.

The implementation remains **measure → simulate → activate**.
