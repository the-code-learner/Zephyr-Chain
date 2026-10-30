# Protocol v2 — Finalized provider compute evidence

P3.6 derives provider-attributed compute evidence from deterministic finalized
settlement state.

This tranche does not define a ContributionScore.

## Evidence source

Provider evidence is derived only when a finalized compute settlement already
passes the same narrow checks used by productive-capital evidence:

- exact transaction/result identity;
- one supported compute settlement operation;
- exact consumed pre-settlement compute job witness;
- exact canonical settlement receipt;
- deterministic settlement reconstruction;
- protocol WorkRegistry resolution.

No RPC settlement summary, provider self-report, advertised capacity or
DeviceCredential claim is used for attribution.

## Paid accepted providers only

The settlement receipt is the canonical payment source.

A provider receives one `FinalizedProviderComputeEvidence` record only when:

- the provider has exactly one assignment in the finalized job;
- the provider has exactly one submitted result;
- the receipt pays that provider the assignment price;
- the provider result root equals the settlement's accepted result root.

For replicated-majority resolution, minority/slashed providers therefore do
not receive accepted paid-work evidence.

This does not erase the fact that a minority provider submitted a result; it
only means that P3.6 does not label that result accepted paid compute evidence.

## Protocol-normalized work context

Each record includes the WorkRegistry-approved:

- workload hash;
- benchmark hash;
- work class;
- normalized units;
- work vector.

It also commits to the exact WorkRegistry hash supplied to the verifier.

These are protocol-defined workload descriptors, not provider-advertised
theoretical FLOPS or capacity.

## Settlement provenance

Each record commits to:

- compute job ObjectID and JobID;
- job owner;
- provider account;
- pre-settlement job-state hash;
- canonical settlement-receipt hash;
- WorkRegistry hash;
- accepted result root;
- verification mode;
- assigned-provider count;
- paid-provider count;
- exact ZPH payment to that provider.

The record itself has a canonical domain-separated hash.

## Replication boundary

For a replicated workload, more than one paid provider can legitimately have
the same workload specification because each replica executed the workload.

P3.6 does **not** specify whether replicated work should be:

- summed;
- de-duplicated;
- discounted;
- weighted by verification role;
- normalized into any scalar score.

Future contribution policy must make that choice explicitly and simulate its
adversarial incentives before activation.

## Payment is not contribution weight

`PaidZPH` records finalized contract settlement value.

It is not:

- a ContributionScore;
- a reward multiplier;
- governance weight;
- validator weight;
- committee weight;
- a fiat-value proxy.

A high payment does not automatically imply proportionally higher protocol
contribution.

## Timing boundary

Compute `Result.CompletedHeight` is not exposed as contribution timing in
P3.6.

The field is part of the finalized provider result, but it originates from the
submitted result and is not independently sufficient to prove when useful work
was physically performed. P3.6 therefore does not convert it into uptime,
latency or duration credit.

## Identity boundary

Provider attribution is to the provider `AccountID` present in finalized
compute assignments/results.

P3.6 does not infer that this account:

- owns a particular DeviceCredential;
- represents one human;
- represents one physical machine;
- has a particular location or hardware identity.

Any future linkage must be separately verified from finalized Zephyr state.

## Non-activation

P3.6 adds no consumer that changes:

- ContributionScore;
- rewards;
- validator power;
- governance power;
- VRF committee eligibility or weight;
- lending;
- monetary policy;
- minting, levies, fee splits or supply.

The records are evidence only.
