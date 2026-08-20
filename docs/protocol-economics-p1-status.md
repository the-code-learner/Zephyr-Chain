# Protocol Economics P1 Status

Working paper reference: 19 August 2026, v0.1.

This tranche is shadow-only and does not activate monetary supply changes.

Implemented here:

- dynamic per-work-class ZCU references from verified slot evidence;
- deterministic weighted median, EWMA smoothing and per-epoch rate limiting;
- ZPPI price-relative basket for compute, data availability and storage;
- reliability/coverage gates for thin economic data;
- canonical annual ZPPI factor `1_020_408_163`, derived from a 2% purchasing-power decline;
- tokenomics documentation migrated away from a fixed 2% supply-growth objective.

The pre-existing supply-growth controller remains a legacy shadow simulator until ZPPI is integrated into the monetary epoch path.

Next P1 tranche:

- dormancy histogram;
- ProductiveCoverage hooks;
- capital-lineage metadata;
- fragmentation/state carrying-cost simulation;
- adversarial wallet-splitting scenarios.
