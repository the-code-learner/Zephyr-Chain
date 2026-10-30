# Protocol v2 — ZECI shadow telemetry

ZECI v1 is implemented as a shadow-only finalized-evidence telemetry envelope.

It intentionally does **not** define a single composite contribution score. The protocol records components that can be verified from finalized Zephyr state and leaves weighting/activation to later simulation and governance work.

## Inputs

ZECI v1 is built from:

- finalized shard epoch metrics;
- verified compute work reconstructed from finalized settlement state and the canonical WorkRegistry;
- optional productive-capital coverage from the finalized IdleCapitalTracker.

It does not consume:

- external price feeds;
- provider-advertised capacity;
- theoretical FLOPS;
- RPC summaries;
- DeviceCredential presence as economic contribution;
- generic transfers as productive activity;
- off-chain claims that validators cannot verify.

## Telemetry fields

The current envelope records:

- epoch and shard count;
- finalized operation count;
- resource used/capacity and utilization;
- supply-weighted native velocity;
- count, normalized units and paid ZPH of verified compute work;
- compute supply reliability;
- fulfilled, expired and backlog compute units;
- productive observed capital and productive coverage when that finalized evidence exists;
- a canonical evidence hash committing to the exact ordered source metrics and verified work.

## Shadow boundary

`Shadow=true` is mandatory.

No reward, validator weight, governance power, lending rate, mint target, levy, fee split or supply mutation reads ZECI v1.

The absence of productive-capital evidence is represented explicitly as unavailable; it is never synthesized.

The collector only emits ZECI for its exact latest finalized height. A caller cannot advance dormancy or other telemetry by asking for a future height without finalizing blocks.

## Future work

A future proposal may define normalization or weighting after public simulations and adversarial analysis. That would be a separate activation decision. ZECI v1 is deliberately measurement-first.
