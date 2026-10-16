# Protocol v2 ephemeral devnet gate

This repository uses an ephemeral Protocol v2 devnet as a pull-request correctness gate.

The gate is designed to catch regressions that package-level unit tests can miss: real QUIC/libp2p connectivity, transaction delivery, scheduled proposal, validator voting, quorum-certificate construction, finalized commit and non-validator state convergence.

## Pull-request topology

The default pull-request run has two layers:

- a correctness scenario with 7 Protocol v2 validator nodes, 1 non-validator full observer and 1 transaction client;
- a large validator-set smoke scenario with 20 Protocol v2 validators plus the same observer/client roles for transaction delivery and finalized consensus.

All nodes use the real Protocol v2 libp2p transport on loopback QUIC addresses allocated by the GitHub-hosted runner. Each validator and the observer own an independent runtime and independent world state.

The transaction client sends a signed native-token transaction through the Protocol v2 transaction stream to the scheduled proposer. Finality is accepted only after proposal verification, validator votes and a normal 2/3+ certificate.

The gate verifies that:

- the transaction reaches the proposer over the network transport;
- the scheduled proposer collects a 2/3+ quorum;
- every active validator commits the same finalized height;
- the non-validator observer applies the certified commit and materializes the same payment;
- the observer cannot propose or vote as a validator;
- with 2 of 7 validators offline, the remaining quorum can still finalize;
- with 3 of 7 validators offline, finality fails closed and no node commits.

## Manual scaling

The pull-request gate always runs the 7-validator correctness/fault scenario and a 20-validator transaction/finality smoke. The workflow also supports manual runs with the large validator-set count configurable from 4 through 20; the manual input defaults to 20.

The quorum-fault boundary test remains fixed at 7 validators because it explicitly checks the 5-of-7 quorum boundary.

## GitHub-hosted topology boundary

These are real Protocol v2 nodes and real QUIC/libp2p connections, but the current gate places them on one standard GitHub-hosted Linux runner.

That is intentionally different from the in-memory finality harness, and it is useful for process/network-stack correctness, but it is still not a geographically distributed production network.

A later phase can place nodes on separate GitHub jobs once an authenticated zero-cost inter-runner rendezvous/transport is available. Until then, no throughput result from this gate is a production-capacity claim.

## Pre-merge enforcement

`.github/workflows/v2-devnet-gate.yml` runs on pull requests. Repository protection for `main` should require the check named `V2 Devnet Gate` in addition to the existing CI, V2 Lab and V2 Dependency Lock checks.

The workflow is intentionally correctness-oriented. It does not activate monetary policy and it does not change the project's benchmark publication boundary.
