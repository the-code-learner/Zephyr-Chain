# Protocol v2 adversarial economics long-horizon simulation

This document describes the P1 long-horizon shadow simulation added for stress testing Zephyr-native economic signals. It is a deterministic engineering scenario, not an economic forecast and not an activation mechanism.

## Safety boundary

The runner is shadow-only. It cannot mint ZPH, impose an idle-capital levy, change fee splits, route live rewards, mutate supply, or activate a monetary policy.

The output labels `looser`, `on-target`, and `tighter` are **directional telemetry only**. They are not executable policy decisions.

The project rule remains:

`measure first -> simulate second -> activate last`

## Default horizon

`cmd/zephyr-econ-longrun` defaults to 1,095 epochs, representing three 365-epoch years for the purpose of the annual ZPPI comparison.

The default configuration uses:

- ZPPI weights: compute 50%, data availability 30%, storage 20%;
- ZPPI minimum reliable coverage: 80%;
- ZPPI EWMA alpha: 20%;
- annual ZPPI target factor: the canonical `1_020_408_163` Q9 factor already defined by Protocol v2;
- monetary-direction tolerance band: 25 basis points around the annual target;
- initial tracked shadow capital: 1,000,000 units;
- dormancy buckets: 30, 180, 365 and 730 blocks plus the overflow bucket;
- state carrying-cost policy: 100 base units per object plus 10 units per KiB.

## Sparse-data stress

The deterministic adversarial generator deliberately introduces reliability gaps:

- compute observation unreliable every 53 epochs;
- data-availability observation unreliable every 17 epochs;
- storage observation unreliable every 29 epochs;
- every 97 epochs both data availability and storage are forced unreliable, leaving only the compute weight when compute itself is available;
- verified compute supply is marked unreliable every 41 epochs.

An unreliable component is excluded from the active ZPPI basket for that epoch. P1 now preserves its previous component index internally, so a temporary data gap does not erase EWMA history when the component returns.

The basket remains fail-closed: if active reliable weight falls below the configured coverage threshold, the epoch is marked unreliable and no annual monetary-direction signal can be produced from that endpoint.

## Oscillating compute scarcity

The workload alternates every 60 epochs inside a 120-epoch cycle.

High-scarcity phase:

- escrow-backed demand: 20,000 units;
- verified supply: 10,000 units;
- backlog: 11,000 units;
- fulfilled: 9,000 units;
- utilization: 90%;
- compute-price trend: +1,000 bps.

Low-scarcity phase:

- escrow-backed demand: 10,000 units;
- verified supply: 20,000 units;
- backlog: 1,000 units;
- fulfilled: 9,000 units;
- utilization: 45%;
- compute-price trend: -800 bps.

This exercises positive and negative ZCSI regimes as well as periods where scarcity is observable but not reliable because authenticated supply evidence is deliberately absent.

## Idle/productive regime shifts

Each 365-epoch year cycles through three productive-coverage regimes:

- epochs 1-120: 0% productive reset;
- epochs 121-240: 75% productive reset;
- epochs 241-365: 10% productive reset.

Productive resets use the existing lineage-preserving `MarkProductiveCoverage` primitive and operate oldest-first. Total tracked capital must remain invariant for every epoch.

No generic transfer is classified as productive by this simulation.

## Fragmentation and state-cost pressure

The hypothetical object count rotates every 90 epochs through:

`1 -> 10 -> 100 -> 1000`

The underlying capital lots and dormancy lineage are not fragmented for economic age purposes. The object count is used only to estimate deterministic state carrying cost, making it possible to observe state pressure without giving wallet fragmentation an idle-age reset.

## Annual ZPPI direction signal

The simulation does not derive a per-epoch inflation target. Instead, once one full configured year of history exists, a reliable current ZPPI is compared with the exact annual target derived from the reliable ZPPI one year earlier.

If either annual endpoint is unreliable, the direction is `unavailable`.

With reliable endpoints:

- current ZPPI below the lower target band -> `looser` shadow direction;
- current ZPPI inside the band -> `on-target`;
- current ZPPI above the upper band -> `tighter`.

This avoids inventing an unapproved per-epoch compounding rule for the canonical annual purchasing-power objective.

## Reproducible command

Run the default three-year scenario:

```sh
go run ./cmd/zephyr-econ-longrun > economics-longrun.json
```

Write annual checkpoints to a file explicitly:

```sh
go run ./cmd/zephyr-econ-longrun \
  -epochs 1095 \
  -checkpoint-every 365 \
  -output economics-longrun.json
```

Use `-include-epochs` only when the full per-epoch timeline is needed. By default the JSON contains the aggregate summary plus compact checkpoints so public review artifacts remain manageable.

The artifact also includes a compact seven-case sensitivity matrix covering baseline, sparse evidence, strict ZPPI coverage, faster ZPPI EWMA, a wider annual-direction band, capped compute scarcity and a prolonged-idle capital regime. Each case remains shadow-only and reports its aggregate summary without activating policy.

## Manual GitHub Actions run

The repository contains `.github/workflows/v2-economics-longrun.yml`, a `workflow_dispatch`-only workflow that runs the same command and uploads `economics-longrun.json` as a 30-day artifact.

The workflow summary repeats the shadow-only non-activation boundary.

## Reviewed P1 long-horizon artifact

P1 validation executed the 1,095-epoch three-year scenario with the seven-case sensitivity matrix. The reviewed artifact came from validation run `38956758511` with digest `sha256:e883d6dd072a689a9b1c6af2ce8cd679f810376c5aeb847146948bb4cda54899`.

Observed validation properties:

- every case remained `shadow=true`;
- initial and final tracked capital were exactly 1,000,000 units in every case;
- baseline mixed reliable and unreliable ZPPI epochs and positive/negative compute-scarcity regimes;
- sparse-evidence increased unreliable ZPPI epochs from 95 to 361;
- strict ZPPI coverage increased unreliable ZPPI epochs to 130;
- the capped-scarcity case limited maximum absolute scarcity to 5,000 bps;
- the prolonged-idle case moved final dormancy into the 31-180 block bucket while preserving capital;
- no output activates monetary policy.

These numbers describe the deterministic stress model only. They are not forecasts or parameter recommendations.

## Invariants covered by repository tests

The three-year adversarial test requires:

- total tracked capital unchanged from first epoch to last;
- both reliable and unreliable ZPPI epochs;
- annual direction unavailable during the initial lookback window and whenever the current ZPPI endpoint is unreliable;
- both positive and negative compute-scarcity regimes;
- both reliable and unreliable compute-scarcity epochs;
- carrying-cost pressure above the one-object baseline;
- productive-regime reset of the expected capital fraction;
- rejection of invalid productive coverage and zero-fragment inputs;
- ZPPI EWMA component history surviving a missing-data epoch.

## What this simulation does not prove

It does not prove monetary stability, optimal policy parameters, manipulation resistance, production state-cost pricing, real-world service-price representativeness, or safe live activation.

It also does not model endogenous agent behavior, market microstructure, validator strategy, credit markets, macroeconomic shocks, or a distributed production network.

Those require additional scenario families, public data, sensitivity analysis and explicit governance/activation gates before any live monetary or idle-capital mechanism can be proposed.
