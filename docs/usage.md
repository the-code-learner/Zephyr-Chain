# Zephyr Chain MVP Usage Guide

## What This MVP Does

The current repository gives you four practical local development flows:

- a single-node flow where one Go node persists chain state, funds test accounts, validates transactions, and commits blocks
- a small multi-node devnet flow where one node produces blocks and other configured nodes follow through transport-backed replication and sync
- a consensus-preparation flow where you elect a validator set, inspect the derived proposer schedule, and optionally enforce that schedule for local block production
- a proposal-and-vote flow where you submit signed consensus messages and inspect the resulting quorum certificate artifacts

The browser wallet can create a local account, export and import it, inspect node-side account state, sign a transaction, and send it to the node.

Deterministic WASM smart contracts and a confidential compute marketplace are planned next phases, not part of the current runnable workflow.

## Run A Single Node

From the repository root:

```powershell
go run ./cmd/node
```

Sanity-check the node with:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/consensus
```

## Run A Two-Node Devnet

Use separate terminals and separate data directories.

Node A, producer:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_HTTP_ADDR=":8080"
$env:ZEPHYR_DATA_DIR="var/devnet-a"
$env:ZEPHYR_PEERS="http://localhost:8081"
$env:ZEPHYR_ENABLE_BLOCK_PRODUCTION="true"
$env:ZEPHYR_ENABLE_PEER_SYNC="true"
go run ./cmd/node
```

Node B, replica:

```powershell
$env:ZEPHYR_NODE_ID="node-b"
$env:ZEPHYR_HTTP_ADDR=":8081"
$env:ZEPHYR_DATA_DIR="var/devnet-b"
$env:ZEPHYR_PEERS="http://localhost:8080"
$env:ZEPHYR_ENABLE_BLOCK_PRODUCTION="false"
$env:ZEPHYR_ENABLE_PEER_SYNC="true"
go run ./cmd/node
```

What to expect:

- Node A accepts wallet transactions and can produce blocks
- Node B polls peer status on `ZEPHYR_SYNC_INTERVAL`
- transactions, faucet credits, proposals, votes, and blocks are replicated over the current transport implementation
- if Node B starts late or misses a block import, it can recover from Node A's snapshot

## Inspect Validator Scheduling

1. Start a node with a validator address:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_ADDRESS="zph_validator_a"
go run ./cmd/node
```

2. Submit an election, then inspect:

```powershell
Invoke-RestMethod http://localhost:8080/v1/validators
Invoke-RestMethod http://localhost:8080/v1/consensus
```

You should see:

- a validator snapshot version that increases when the election result is replaced
- the persisted validator list and normalized election config
- `totalVotingPower`, `quorumVotingPower`, and `nextProposer`

## Submit A Proposal And Votes

The current MVP can persist signed proposals and votes and derive a quorum certificate, even though it does not yet use that certificate to gate block commit.

1. Make sure a validator set already exists.
2. Build a signed proposal payload using a validator key whose address matches the scheduled proposer for the next height.
3. POST it to `/v1/consensus/proposals`.
4. Submit validator votes to `/v1/consensus/votes`.
5. Inspect `/v1/consensus` to see the latest proposal, vote tallies, and latest certificate.

Expected behavior:

- the proposal is rejected if the proposer is not scheduled for that height
- votes are rejected if they do not reference a known proposal
- once the accumulated vote power crosses quorum, the node stores a quorum certificate artifact
- peers replicate those artifacts through the current transport implementation

## Enforce Proposer Scheduling Locally

This is a local safety guard, not finality.

Run the node with:

```powershell
$env:ZEPHYR_NODE_ID="node-b"
$env:ZEPHYR_VALIDATOR_ADDRESS="zph_validator_b"
$env:ZEPHYR_ENFORCE_PROPOSER_SCHEDULE="true"
go run ./cmd/node
```

After an election is stored, `POST /v1/dev/produce-block` will return `409` if the local validator is not the scheduled proposer for the next height.

## Troubleshooting

### Proposal Or Vote Submission Returns An Error

- confirm a validator set already exists through `GET /v1/validators`
- confirm the signer address is part of that validator set
- confirm the proposal height matches the node's `nextHeight` in `GET /v1/consensus`
- confirm the proposal signer matches `nextProposer`
- confirm votes reference the same `blockHash`, `height`, and `round` as a known proposal
- confirm the signed payload still matches the visible request fields exactly

### Block Production Is Rejected By Proposer Scheduling

- confirm `GET /v1/consensus` shows a non-empty validator set
- confirm `ZEPHYR_VALIDATOR_ADDRESS` matches the local validator you expect this node to represent
- confirm `GET /v1/consensus` reports the same address in `nextProposer`
- disable `ZEPHYR_ENFORCE_PROPOSER_SCHEDULE` if you intentionally want local dev-only block production without schedule checks

## Recommended Local Demo Flow

1. Start Node A.
2. Start Node B as a replica.
3. Start the Vue wallet against Node A.
4. Create a wallet.
5. Fund the wallet with the local dev faucet.
6. Sign and broadcast a sample transaction.
7. Produce a block on Node A.
8. Inspect `GET /v1/status`, `GET /v1/peers`, `GET /v1/blocks/latest`, and `GET /v1/accounts/{address}` on both nodes.
9. Submit a validator election and inspect `GET /v1/validators` plus `GET /v1/consensus`.
10. Submit a signed proposal and validator votes.
11. Inspect the resulting vote tallies and certificate on both nodes.
12. Optionally restart a node and confirm the validator snapshot and consensus artifacts survived.
