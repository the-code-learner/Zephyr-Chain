# Protocol v2 — Contribution evidence shadow manifest

P3.5 adds a canonical **shadow evidence manifest** for future contribution work.

It does not define a ContributionScore.

The current design sources do not define a canonical scalar weighting formula
that can safely combine the available evidence classes. This tranche therefore
preserves the evidence domains separately instead of inventing weights.

## Evidence classes

The manifest currently separates two classes.

### Economic evidence

Economic evidence is committed through the exact hash of existing
`ZECIShadowTelemetry`.

ZECI remains the source envelope for:

- finalized shard metrics;
- finalized verified compute work derived from settlement plus WorkRegistry;
- optional finalized productive-capital coverage from IdleCapitalTracker.

P3.5 does not reinterpret or rescale those components. It records the exact
ZECI hash and epoch.

### Citizen key-control evidence

Citizen challenge evidence is committed separately through a canonical root
over verified key-control facts:

- network;
- owner account;
- DeviceCredential ObjectID;
- credential sequence;
- finalized credential proof height and state root;
- required capability bitset;
- challenge hash;
- context hash.

The root is canonical across input ordering and rejects duplicate
credential/challenge pairs.

A Citizen challenge establishes key control for the exact finalized credential
version and signed challenge. It is **not economic contribution**.

The manifest therefore cannot turn Citizen evidence into economic availability.

## Explicit unavailability

Each evidence class has an explicit availability bit.

If economic evidence is unavailable:

- `EconomicEvidenceAvailable=false`;
- epoch is zero;
- economic evidence hash is zero.

If Citizen key-control evidence is unavailable:

- `CitizenKeyControlAvailable=false`;
- count is zero;
- Citizen root is zero.

At least one evidence class must be available. Missing evidence is never
synthesized.

## Network boundary

The manifest is scoped to one Zephyr network.

All Citizen evidence must match the manifest network. Cross-network evidence is
rejected instead of being silently mixed.

The current ZECI structure itself is not network-tagged, so this shadow
manifest does not claim that its network field independently authenticates
ZECI provenance. A future consensus integration must obtain the economic source
from the finalized collector for the same protocol instance.

## No per-account compute attribution shortcut

The current aggregate `VerifiedWork` record does not preserve provider or
owner attribution. ZECI is also an aggregate epoch envelope.

P3.5 does not infer a per-account compute contribution from:

- job existence;
- DeviceCredential presence;
- a successful Citizen challenge;
- payment totals;
- RPC summaries;
- advertised capacity;
- generic transfers.

If future contribution accounting requires per-provider verified work, that
attribution must be derived explicitly from finalized settlement evidence and
specified before aggregation.

## No scalar score

`ShadowEvidenceEnvelope` contains no:

- scalar ContributionScore;
- weights;
- normalization formula;
- ranking;
- threshold;
- reward multiplier;
- governance weight;
- validator weight;
- committee weight.

A future score design must first define the exact evidence set, units,
normalization, missing-data behavior, adversarial properties and simulations.
It must not infer those choices from the mere existence of the evidence
manifest.

## Timing boundary

Citizen `CredentialHeight` is the finalized proof height of the credential
state. It is not proof that the challenge response was produced at that height.

P3.5 therefore does not use credential height as work timing, uptime or
participation duration.

## Non-activation

P3.5 adds no consumer that changes:

- rewards;
- validator power;
- governance power;
- VRF committee selection;
- lending terms;
- mint targets;
- levies;
- fee splits;
- circulating supply.

The project discipline remains:

`measure -> simulate -> activate`

The purpose of P3.5 is to make evidence availability and provenance boundaries
canonical before any scalar contribution policy is proposed.
