# Zephyr Chain MVP Usage Guide

## What This MVP Does

The current repository gives you four practical local development flows:

- a single-node flow where one Go node persists chain state, funds test accounts, validates transactions, and commits blocks
- a small multi-node devnet flow where one node produces blocks and other configured nodes follow through transport-backed replication and sync
- a scheduling flow where you elect a validator set, inspect the derived proposer schedule, and optionally enforce that schedule for local block production
- a certificate-gated consensus flow where you build a concrete next-block template, submit signed proposals and votes for that template, and commit only after a quorum certificate exists

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
- if validator private keys are configured, `GET /v1/status` exposes a signed identity proof and `GET /v1/peers` shows whether peer identity verification succeeded
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

## Inspect Signed Validator Identity

If you want the node to prove which validator it represents over the current HTTP transport, start it with a validator private key:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key>"
go run ./cmd/node
```

Then inspect:

```powershell
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/peers
```

What to expect:

- `GET /v1/status` includes an `identity` object signed by the validator key
- the node derives `validatorAddress` from that key unless you also set `ZEPHYR_VALIDATOR_ADDRESS`, in which case startup rejects mismatches
- `GET /v1/peers` shows `identityPresent`, `identityVerified`, and `identityError` for configured peers

## Run A Certificate-Gated Commit Flow

This is the closest current path to production-style block acceptance.

1. Start a node with proposer scheduling and certificate enforcement enabled:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_ADDRESS="zph_validator_a"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key>"
$env:ZEPHYR_ENFORCE_PROPOSER_SCHEDULE="true"
$env:ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES="true"
go run ./cmd/node
```

2. Make sure a validator set already exists.
3. Queue at least one transaction in the mempool.
4. Fetch the concrete next block candidate:

```powershell
Invoke-RestMethod http://localhost:8080/v1/dev/block-template
```

5. Build a signed proposal whose `height`, `previousHash`, `producedAt`, `transactionIds`, and `blockHash` match that template exactly.
6. POST the proposal to `/v1/consensus/proposals`.
7. Submit validator votes to `/v1/consensus/votes` until a quorum certificate exists for that same `blockHash`.
8. Commit that exact block template by reusing the returned `producedAt` timestamp:

```powershell
$body = @{ producedAt = "2026-03-23T10:00:00Z" } | ConvertTo-Json
Invoke-RestMethod http://localhost:8080/v1/dev/produce-block -Method Post -ContentType 'application/json' -Body $body
```

Expected behavior:

- the proposal is rejected if the proposer is not scheduled for that height or if `previousHash` does not match the current tip
- the proposal is rejected if `blockHash` does not match the signed `producedAt` plus ordered `transactionIds`
- votes are rejected if they do not reference a known proposal
- once the accumulated vote power crosses quorum, the node stores a quorum certificate artifact
- with `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES=true`, block commit returns `409` until the matching proposal and certificate exist
- even after certification, changing `producedAt` changes the candidate and the commit is rejected
- peers configured with the same enforcement flag import only certified blocks whose concrete template matches a stored proposal

## Enforce Proposer Scheduling Locally

This is a narrower guard than certificate enforcement.

Run the node with:

```powershell
$env:ZEPHYR_NODE_ID="node-b"
$env:ZEPHYR_VALIDATOR_ADDRESS="zph_validator_b"
$env:ZEPHYR_ENFORCE_PROPOSER_SCHEDULE="true"
go run ./cmd/node
```

After an election is stored, `POST /v1/dev/produce-block` returns `409` if the local validator is not the scheduled proposer for the next height.

## Troubleshooting

### Proposal Or Vote Submission Returns An Error

- confirm a validator set already exists through `GET /v1/validators`
- confirm the signer address is part of that validator set
- confirm the proposal height matches the node's `nextHeight` in `GET /v1/consensus`
- confirm the proposal signer matches `nextProposer`
- confirm the proposal `previousHash` matches the current chain tip
- confirm the proposal `producedAt` and ordered `transactionIds` come from the same `GET /v1/dev/block-template` response as `blockHash`
- confirm votes reference the same `blockHash`, `height`, and `round` as a known proposal
- confirm the signed payload still matches the visible request fields exactly

### Peer Identity Verification Fails

- confirm the peer validator node is started with `ZEPHYR_VALIDATOR_PRIVATE_KEY`
- confirm the private key is a base64-encoded PKCS#8 P-256 key
- confirm `GET /v1/status` on the remote node includes an `identity` object
- confirm `GET /v1/peers` shows the expected `validatorAddress` and read `identityError` for the exact verification failure
- if you intentionally run unsigned legacy peers during development, expect `identityPresent=false`

### Certified Block Production Is Rejected

- confirm `GET /v1/consensus` shows a non-empty validator set
- confirm `ZEPHYR_VALIDATOR_ADDRESS` matches the local validator you expect this node to represent
- confirm `GET /v1/consensus` reports the same address in `nextProposer`
- confirm `GET /v1/dev/block-template` and your proposal use the same `blockHash`, `previousHash`, `producedAt`, and `transactionIds`
- confirm `GET /v1/consensus` shows a latest certificate for that same `blockHash`
- confirm you replay `POST /v1/dev/produce-block` with the same `producedAt` used by the certified template
- disable `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES` only if you intentionally want a looser local dev flow

## Recommended Local Demo Flow

1. Start Node A.
2. Start Node B as a replica.
3. Start the Vue wallet against Node A.
4. Create a wallet.
5. Fund the wallet with the local dev faucet.
6. Sign and broadcast a sample transaction.
7. Produce a block on Node A.
8. Inspect `GET /v1/status`, `GET /v1/peers`, `GET /v1/blocks/latest`, and `GET /v1/accounts/{address}` on both nodes.
9. If validator private keys are configured, confirm peer identity verification succeeds in `GET /v1/peers`.
10. Submit a validator election and inspect `GET /v1/validators` plus `GET /v1/consensus`.
11. Fetch a block template, submit a matching signed proposal and validator votes, and wait for the certificate to appear.
12. Produce the certified block with the same `producedAt` timestamp.
13. Inspect the resulting block, vote tallies, and certificate on both nodes.
14. Optionally restart a node and confirm the validator snapshot and consensus artifacts survived.



