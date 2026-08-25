# Protocol Economics P1 Status

Working paper reference: 19 August 2026, v0.1.

This tranche is **shadow-only**. It does not activate live monetary supply changes, an idle-capital levy, reward-routing changes or production benchmark claims.

## Implemented in this branch

- dynamic per-work-class ZCU references from verified slot evidence;
- deterministic weighted median, EWMA smoothing and per-epoch rate limiting;
- ZPPI price-relative basket for compute, data availability and storage;
- reliability/coverage gates for thin economic data;
- canonical annual ZPPI factor `1_020_408_163`, derived from the ~2% annual purchasing-power objective;
- tokenomics documentation migrated away from a fixed 2% supply-growth objective;
- capital-lot lineage primitives, dormancy histograms and productive-coverage measurement;
- deterministic state carrying-cost estimation and fragmentation-pressure simulation;
- prospective `IdleCapitalTracker` with amount-conserving lineage, canonical checkpoints, local/cross-shard targets and explicit productive-use hooks;
- repository tests for fragmentation stability, target-order canonicality, cross-shard materialization, productive coverage, checkpoint round trips and atomic invalid-transition rejection;
- finalized-through-consensus benchmark report core with warm-up separation, measured finalized TPS, p50/p95/p99 finality, environment/config metadata, JSON output and safety/liveness invalidation.

The pre-existing supply-growth controller remains a legacy **shadow simulator** until the ZPPI path is integrated into the monetary epoch path. ZCSI remains primarily a compute incentive/capacity-routing signal rather than the monetary anchor.

## Activation discipline

```text
measure first -> simulate second -> activate last
```

No live mint, burn policy, levy or reward-routing change is implied by the presence of telemetry or simulation code.

## Remaining P1 work

- add the extended adversarial tracker suite for repeated self-cycling, payment/change outputs, mixed-age merge, materialization replay and checkpoint malformed-state coverage;
- integrate `IdleCapitalTracker` into the finalized `EpochCollector` as an opt-in shadow component;
- derive local and cross-shard lineage only from finalized transaction/result/witness evidence;
- use canonical cross-shard receipt identity for tracker transfer IDs and protocol-defined destination object identity on import;
- integrate tracker state into collector checkpoint/restore with explicit versioning and restart tests;
- connect the finalized benchmark report to real V2 Lab finality/QC samples;
- add a small deterministic CI benchmark scenario and a heavier manual shard/cross-shard matrix;
- publish no new chain-TPS claim until the finalized-through-consensus harness has actually run under a documented configuration.
