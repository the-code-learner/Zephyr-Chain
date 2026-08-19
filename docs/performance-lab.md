# Zephyr Consensus & Performance Lab

## Purpose

The Consensus & Performance Lab turns Zephyr's scalability target into a repeatable engineering program. It has two independent responsibilities:

1. **protocol conformance**: prove safety and liveness under deterministic multi-validator faults;
2. **performance measurement**: measure finalized throughput and finality using the real transaction, consensus, state and persistence paths.

Performance changes are not considered successful if they weaken the conformance matrix.

## Canonical meaning of TPS

For Zephyr, the headline TPS number means:

> **transactions finalized by validator consensus per second**.

The benchmark must not count HTTP requests accepted, mempool insertions, unsigned synthetic operations, or batches that were not individually executed as transactions.

A transaction counts only when it is contained in a committed block protected by the configured quorum-certificate rules.

## Canonical transfer workload

The first reference workload is a simple native ZPH transfer with:

- a real P-256 signature;
- the normal transaction domain and chain ID;
- normal static and stateful validation;
- a real mempool entry;
- real state execution;
- a real deterministic state commitment;
- proposal and vote dissemination;
- quorum-certificate formation;
- committed block persistence.

Client-side key generation and signing are prepared outside the timed consensus benchmark. Signature **verification** remains inside the node path and is also benchmarked independently so its cost can be profiled directly.

## Validator matrix

The target matrix is:

- 1 validator: local execution/control baseline;
- 4 validators: smallest useful multi-validator BFT-style lab;
- 7 validators: primary development baseline;
- 16 validators: scaling and dissemination-pressure baseline.

The first checked-in gate focuses on 4 and 7 validators. The harness is intentionally parameterized so 1 and 16 validator scenarios can use the same machinery as the suite grows.

The 7-validator reference configuration currently uses equal voting power: `10,000` per validator, `70,000` total, with Zephyr's normal quorum calculation producing a `46,667` voting-power threshold.

## Required metrics

Every publishable Zephyr performance result should report at least:

- sustained finalized transactions per second;
- time to finality p50, p95 and p99;
- validator count;
- transactions per finalized block;
- finalized block payload size;
- protocol payload bytes per finalized transaction;
- persisted state size per node;
- CPU profile;
- heap/allocation profile;
- machine CPU, RAM, operating system and Go version;
- network topology and latency assumptions.

The in-repository benchmark currently emits:

- `finalized-tx/s`;
- `finality-p50-ms`;
- `finality-p95-ms`;
- `finality-p99-ms`;
- `protocol-payload-B/finalized-tx`;
- `finalized-block-B`;
- `state-B/node`.

CPU, heap, mutex and block profiles come from the standard Go benchmark profiler so we can inspect flame graphs before choosing an optimization.

## First CI baseline

The first successful 7-validator measurement on a GitHub-hosted Ubuntu runner is a **development baseline only**, not a Zephyr performance claim and not a controlled hardware result.

With 32 finalized transfers per block, one observed run on an AMD EPYC 7763 hosted runner reported approximately:

- `42.92 finalized-tx/s`;
- `628 ms` p50 finality;
- `1.128 s` p95/p99 finality;
- `23,045 B` finalized block size;
- `17,468 B` of measured protocol payload per finalized transaction;
- `162,138 B` persisted state per node after the sample;
- approximately `88.8 us/op` for the separate P-256 transaction-validation baseline.

A separate verification run was in the same rough range at about `44.3 finalized-tx/s`. Shared-runner variance is expected. These numbers exist to establish a measurable starting point and to identify bottlenecks; they must not be presented as production capacity.

## Running the lab

Run the protocol conformance gate:

```bash
go test ./internal/api -run '^TestLab' -count=1 -timeout=90s
```

Run the 7-validator finalized-throughput benchmark with several consecutive finalized blocks:

```bash
go test ./internal/api \
  -run '^$' \
  -bench '^BenchmarkLabConsensusFinality7Validators$' \
  -benchtime=5x \
  -count=1 \
  -timeout=120s
```

Measure P-256 transaction verification separately:

```bash
go test ./internal/api \
  -run '^$' \
  -bench '^BenchmarkLabP256TransactionVerification$' \
  -benchtime=2s \
  -count=1
```

Generate profiles for the end-to-end benchmark:

```bash
go test ./internal/api \
  -run '^$' \
  -bench '^BenchmarkLabConsensusFinality7Validators$' \
  -benchtime=5x \
  -cpuprofile=cpu.out \
  -memprofile=mem.out \
  -mutexprofile=mutex.out \
  -blockprofile=block.out \
  -count=1 \
  -timeout=120s
```

Then inspect, for example:

```bash
go tool pprof -http=:8081 cpu.out
```

## Fault-injection contract

The lab transport wraps Zephyr's existing `peerTransport`; it does not replace consensus or ledger logic. Faults are injected at the transport boundary while transactions, proposals, votes, certificates, blocks, state roots and persistence remain the production implementations.

The initial gate covers:

- 7-validator certified happy-path finality;
- a 4/3 partition where neither side has quorum: no block may commit while partitioned; after heal, quorum finality must resume and lagging validators must catch up;
- a 5/2 partition where the quorum side commits and the minority later catches up through the normal peer-recovery path;
- delayed, duplicated and deliberately vote-before-proposal delivery: all nodes must still converge on one committed tip.

The first 4/3 recovery test exposed a real integration gap: after heal, enough validators could vote to finalize the block while some voters still had not received the committed block. Snapshot recovery alone could not solve that state because fewer than 2/3 of validators had materialized the new snapshot.

Zephyr therefore now has a certified block catch-up path:

1. peers expose retained signed proposal/vote evidence for an already committed block through an authenticated internal endpoint;
2. the receiving node validates the block against its local committed state;
3. it validates every proposal/vote signature and validator identity against the local validator set;
4. it independently recomputes voting power and requires the normal quorum before state mutation;
5. only then does it import the block atomically and derive its local commit certificate;
6. quorum-validated snapshot recovery remains the fallback for deeper repair.

The transport capability is optional, so the future libp2p/QUIC implementation can implement the same evidence contract while HTTP remains the reference transport.

The matrix will expand to cover:

- validator offline/restart before and after vote;
- proposer crash during a round;
- conflicting proposals;
- conflicting votes;
- explicit Byzantine peer payloads;
- corrupted snapshots;
- wrong-chain validators;
- longer partitions and repeated heal/fail cycles;
- the same conformance suite over HTTP and the future libp2p/QUIC transport.

## Performance-gate policy

Correctness is a hard gate now. Numerical performance thresholds are deliberately not hard-coded against GitHub-hosted runners yet because shared-runner variance would turn a useful measurement into a flaky gate.

The next step is to establish a controlled reference machine and retain benchmark history. Once variance is understood, Zephyr can add regression budgets such as:

- no more than N% sustained-TPS regression;
- no more than N% p95 finality regression;
- no unbounded growth in protocol bytes per finalized transaction;
- no unexpected state-size or allocation regression.

## Optimization decision rule

No major performance architecture is selected before profiling the canonical benchmark.

The first profile should attribute time and resource pressure across:

`signature verification -> transaction validation -> mempool -> state execution -> state root -> block serialization -> proposal dissemination -> votes -> persistence`

Likely candidates include parallel signature verification, serialization changes, storage replacement, lock reduction, incremental state commitments and transport/dissemination changes. These are hypotheses, not roadmap commitments, until the profile identifies the actual bottleneck.

## Path toward 1M TPS

The engineering sequence is:

1. consensus and performance lab;
2. profiling and evidence-backed performance architecture;
3. scalable storage/state execution;
4. production libp2p/QUIC transport, while keeping HTTP as the reference transport;
5. deterministic Rust-first WASM execution and fee metering;
6. staking/governance;
7. public devnet;
8. confidential compute marketplace.

At every stage the canonical finalized-TPS/finality benchmark and the consensus conformance matrix remain the comparison point.
