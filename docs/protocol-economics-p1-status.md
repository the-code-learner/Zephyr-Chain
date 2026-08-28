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
- adversarial/edge coverage for repeated self-cycling, payment/change outputs, mixed-age merge, fragmentation, duplicate materialization and malformed/invalid transition handling;
- finalized-evidence lineage derivation from transaction/result/witness data without heuristic transfer identifiers;
- canonical cross-shard tracker identity using `CrossShardReceipt.Hash()` and protocol `DestinationObject()` identity;
- opt-in `IdleCapitalTracker` integration inside the finalized `EpochCollector` preview/apply path;
- canonical export receipts passed from the runtime into finalized economic observations after the source state root is known;
- collector checkpoint version 2 carrying idle-capital state while retaining restore support for version 1 checkpoints;
- restart/canonical checkpoint and local/cross-shard collector integration tests;
- finalized compute productive-evidence derivation that verifies the referenced consumed job object and canonical settlement receipt, reconstructs exact/strict-majority settlement, isolates paid compute escrow from collateral/refunds, and computes overflow-safe `paid / escrow` coverage in basis points;
- productive-evidence tests for exact replicated settlement, strict-majority settlement, tampered receipts, generic-transfer exclusion and maximum-`uint64` capital values;
- finalized-through-consensus benchmark report core with warm-up separation, measured finalized TPS, p50/p95/p99 finality, environment/config metadata, JSON output and safety/liveness invalidation.

The pre-existing supply-growth controller remains a legacy **shadow simulator** until the ZPPI path is integrated into the monetary epoch path. ZCSI remains primarily a compute incentive/capacity-routing signal rather than the monetary anchor.

## Productive-capital boundary

The finalized compute evidence layer is intentionally read-only for now. A compute job object can contain both the owner's escrow and provider collateral, while settlement outputs can aggregate provider payment and collateral return into the same native coin. Marking the whole job object productive would therefore over-credit collateral.

The next lineage step must preserve those economic subcomponents explicitly before any call to `MarkObjectProductive`. Generic transfers and arbitrary contract calls remain ineligible for productive-use credit.

## Activation discipline

```text
measure first -> simulate second -> activate last
```

No live mint, burn policy, levy or reward-routing change is implied by the presence of telemetry or simulation code.

## Remaining P1 work

- map finalized compute escrow lineage through job creation, assignment/ingest and settlement without folding provider collateral into productive credit, then connect only verified paid escrow to productive-coverage updates;
- expand restart/fault scenarios around collector/runtime checkpoints and cross-shard lineage as the shadow tracker accumulates larger state;
- connect the finalized benchmark report to real V2 Lab finality/QC samples;
- add a small deterministic CI benchmark scenario and a heavier manual shard/cross-shard matrix;
- run long-horizon public/adversarial economic simulations before proposing any monetary or idle-capital activation;
- publish no new chain-TPS claim until the finalized-through-consensus harness has actually run under a documented configuration.
