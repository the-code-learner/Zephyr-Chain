# Protocol v2 finalized-through-consensus benchmarking

This document defines the current Protocol v2 benchmark harness and the limits on what its output means.

The harness is instrumentation for correctness and comparative engineering. It is **not** evidence of production capacity, and it does not establish that Zephyr Chain has reached the 1,000,000 TPS north-star.

## Measurement boundary

A transaction is counted only after the V2 consensus path has completed:

1. each validator runtime independently builds the candidate;
2. the scheduled proposer signs the candidate header;
3. a quorum of validators verifies the proposal against its independently derived candidate and signs votes;
4. the validator set builds a certificate/QC;
5. every runtime commits the certified candidate;
6. only then is the transaction recorded as finalized.

Execution-only timings are not reported as chain TPS.

Warmup blocks are finalized through the same path but are kept outside the measured sample window. The report rejects a sample whose submission time is before the measured start.

## Reusable harness

`internal/v2/lab/finality_harness.go` contains the reusable single-process harness used by CI and the manual matrix command.

The harness derives deterministic validator and transaction identities before measurement, creates independent in-memory world states for every validator, and requires every validator to derive the same consensus header before voting.

Current topology label:

`single-process-in-memory`

That topology is intentionally explicit in every generated report. It does not model real network latency, distributed host contention, storage devices, kernel/network-stack behavior, or production deployment topology.

## Cross-shard workload semantics

For a configured cross-shard ratio, the exact requested ratio must be representable by the chosen transactions-per-block value. For example, the default manual matrix uses 40 transactions per block, so 10%, 25% and 50% are represented exactly as 4, 10 and 20 cross-shard transfers.

A cross-shard transfer follows the real receipt path:

- the source transaction is finalized and produces a canonical `CrossShardReceipt`;
- the receipt is proven against the finalized source shard commitment and receipt root;
- the proof-carrying import is included on the destination shard in the following consensus block;
- the destination object and durable receipt marker must exist after import;
- a final import-only drain block is used when the last measured traffic block leaves receipts pending.

The drain block is outside the measured TPS window. During measured steady state, imports from the previous block are processed while the current block carries new transactions. This means receipt-import work is present in measured blocks without inflating finalized transaction counts with the final drain.

`finalized_tps` therefore counts source transactions finalized through consensus. The harness additionally records generated/imported cross-shard receipt counts so a run cannot silently omit destination materialization.

## CI coverage

The normal V2 Lab keeps the benchmark small and deterministic enough for pull-request gating:

- 1 shard, 7 validators, native transfers;
- 2 shards with 50% cross-shard traffic and canonical receipt import;
- a 4-shard cross-shard smoke scenario;
- warmup/measured-window separation;
- p50/p95/p99 finality reporting;
- safety/liveness invalidation in the report core;
- existing partition-recovery and finalized-batch gates.

These CI samples are correctness/instrumentation tests, not publishable production benchmarks.

## Manual matrix

The command below runs the default single-process matrix:

```sh
go run ./cmd/v2-finality-matrix \
  -shards 1,2,4,8 \
  -cross-shard-bps 0,1000,2500,5000 \
  -transactions-per-block 40 \
  -warmup-blocks 1 \
  -measured-blocks 3 \
  -block-cadence 1ms \
  -validators 7 \
  -commit "$(git rev-parse HEAD)" \
  -topology single-process-in-memory \
  -workload native-transfer-receipt-import-matrix \
  > finality-matrix.json
```

Cross-shard configurations are skipped for one-shard runs because they are undefined. Other requested ratios that cannot be represented exactly per traffic block fail closed instead of being rounded and mislabeled.

The repository also contains the manually triggered GitHub Actions workflow `.github/workflows/v2-finality-matrix.yml`. It runs the same 1/2/4/8-shard by 0/10/25/50%-cross-shard matrix and uploads `finality-matrix.json` as an artifact.

## Report metadata

Each report includes:

- commit SHA;
- shard count;
- validator count;
- transactions per traffic block;
- configured block cadence;
- exact cross-shard ratio;
- workload label;
- topology label;
- CPU count and `GOMAXPROCS`;
- warmup finalized count;
- measured finalized count and duration;
- finalized TPS;
- p50/p95/p99 finality latency;
- rejected/error and safety/liveness counters.

The current single-process runner does not claim to measure real network bytes, DA bytes, RAM pressure, WAN behavior or distributed storage performance unless those fields are populated by a future representative runner.

## Publication gate

Do not publish a production-capacity or 1M-TPS claim from the CI or single-process manual matrix.

A publishable capacity result requires, at minimum:

- a documented distributed topology;
- documented CPU, RAM, storage, OS/runtime and network conditions;
- explicit shard and validator placement;
- exact workload and cross-shard composition;
- warmup separated from measurement;
- transactions counted only after consensus finality;
- no safety failure;
- no liveness failure;
- raw machine-readable result artifacts retained for review.

The project rule remains:

`measure first -> simulate second -> activate last`
