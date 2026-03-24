# Zephyr Chain MVP Usage Guide

## What This MVP Does

The current repository gives you five practical local development flows:

- a single-node flow where one Go node persists chain state, funds test accounts, validates transactions, and commits blocks
- a small multi-node devnet flow where one node produces blocks and other configured nodes follow through transport-backed replication and sync
- a scheduling flow where you elect a validator set, inspect the derived proposer schedule, and optionally enforce that schedule for local block production
- a manual certificate-gated consensus flow where you build a concrete next-block template, submit signed proposals and votes for that template, and commit only after a quorum certificate exists
- an automated certificate-gated devnet flow where the scheduled proposer self-proposes, active validators auto-vote, and the proposer auto-commits once quorum exists

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
- if validator private keys are configured, `GET /v1/status` exposes a signed identity proof and `GET /v1/peers` shows verification plus admission state for configured peers
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
- when strict peer admission is enabled, `GET /v1/status` also reports `peerIdentityRequired=true`

## Enforce Peer Admission

If you want the current HTTP devnet to fail closed on unsigned or mismatched peers, start the node with:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key>"
$env:ZEPHYR_PEERS="http://localhost:8081"
$env:ZEPHYR_REQUIRE_PEER_IDENTITY="true"
$env:ZEPHYR_PEER_VALIDATORS="http://localhost:8081=zph_validator_b"
go run ./cmd/node
```

What to expect:

- `GET /v1/status` reports `peerIdentityRequired=true`
- `GET /v1/peers` shows `expectedValidator`, `admitted`, and `admissionError` for each configured peer
- background sync and outgoing replication use only admitted peers under this policy
- replicated peer POST requests without a valid identity, or from validators outside the configured binding allowlist, are rejected with `403`

## Run A Manual Certificate-Gated Commit Flow

This is the lowest-level way to exercise the current certified block path.

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

5. Build a signed proposal whose `height`, `previousHash`, `producedAt`, full `transactions`, ordered `transactionIds`, and `blockHash` match that template exactly.
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
- the proposal is rejected if embedded `transactions` are missing or do not match those `transactionIds`
- votes are rejected if they do not reference a known proposal
- once the accumulated vote power crosses quorum, the node stores a quorum certificate artifact
- with `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES=true`, block commit returns `409` until the matching proposal and certificate exist
- once certified, the node can commit from the stored proposal body even if the local mempool no longer contains that candidate
- even after certification, changing `producedAt` changes the candidate and the commit is rejected
- peers configured with the same enforcement flag import only certified blocks whose concrete template matches a stored proposal

## Run An Automated Certified Devnet

This is the closest current path to a production-style validator flow, but it is still a first-pass round-0 engine.

1. Start the scheduled proposer with automation enabled:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_HTTP_ADDR=":8080"
$env:ZEPHYR_DATA_DIR="var/devnet-a"
$env:ZEPHYR_PEERS="http://localhost:8081"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key-a>"
$env:ZEPHYR_ENABLE_BLOCK_PRODUCTION="true"
$env:ZEPHYR_ENABLE_PEER_SYNC="true"
$env:ZEPHYR_ENABLE_CONSENSUS_AUTOMATION="true"
$env:ZEPHYR_CONSENSUS_INTERVAL="250ms"
$env:ZEPHYR_ENFORCE_PROPOSER_SCHEDULE="true"
$env:ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES="true"
go run ./cmd/node
```

2. Start another active validator with automation enabled:

```powershell
$env:ZEPHYR_NODE_ID="node-b"
$env:ZEPHYR_HTTP_ADDR=":8081"
$env:ZEPHYR_DATA_DIR="var/devnet-b"
$env:ZEPHYR_PEERS="http://localhost:8080"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key-b>"
$env:ZEPHYR_ENABLE_BLOCK_PRODUCTION="false"
$env:ZEPHYR_ENABLE_PEER_SYNC="true"
$env:ZEPHYR_ENABLE_CONSENSUS_AUTOMATION="true"
$env:ZEPHYR_CONSENSUS_INTERVAL="250ms"
$env:ZEPHYR_REQUIRE_PEER_IDENTITY="true"
$env:ZEPHYR_PEER_VALIDATORS="http://localhost:8080=zph_validator_a"
$env:ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES="true"
go run ./cmd/node
```

3. Submit an election that makes both validators active and puts Node A first in the proposer schedule.
4. Queue at least one transaction on Node A.
5. Inspect the live state:

```powershell
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/consensus
Invoke-RestMethod http://localhost:8081/v1/consensus
```

Expected behavior:

- startup fails if automation is enabled without `ZEPHYR_VALIDATOR_PRIVATE_KEY`
- the scheduled proposer builds the next block template and persists a self-contained round-0 proposal automatically
- active validators persist and replicate votes automatically for that proposal
- once quorum is observed, the proposer commits from the stored certified proposal body without requiring `POST /v1/dev/produce-block`
- admitted peers replicate the proposal, vote, certificate, and committed block over the current HTTP transport
- `GET /v1/status` and `GET /v1/consensus` expose `consensusAutomationEnabled=true`

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
- confirm the proposal `producedAt`, full `transactions`, and ordered `transactionIds` come from the same `GET /v1/dev/block-template` response as `blockHash`
- confirm votes reference the same `blockHash`, `height`, and `round` as a known proposal
- confirm the signed payload still matches the visible request fields exactly

### Consensus Automation Does Not Fire

- confirm the node started with `ZEPHYR_VALIDATOR_PRIVATE_KEY`; startup rejects automation without it
- confirm the local validator is part of the active validator set shown by `GET /v1/validators`
- confirm `GET /v1/consensus` reports the expected validator in `nextProposer`
- confirm the proposer node still has `ZEPHYR_ENABLE_BLOCK_PRODUCTION=true`
- confirm `GET /v1/status` or `GET /v1/consensus` shows `consensusAutomationEnabled=true`
- confirm there is at least one queued transaction when expecting an automatic proposal
- remember the current automation path is only round 0; there is no timeout, re-proposal, rebroadcast, or round-change recovery yet

### Peer Identity Verification Or Admission Fails

- confirm the peer validator node is started with `ZEPHYR_VALIDATOR_PRIVATE_KEY`
- confirm the private key is a base64-encoded PKCS#8 P-256 key
- confirm `GET /v1/status` on the remote node includes an `identity` object
- confirm `GET /v1/peers` shows the expected `validatorAddress`, then read `identityError` or `admissionError` for the exact failure
- if you enable `ZEPHYR_REQUIRE_PEER_IDENTITY`, peer-originated replicated POST requests without a valid identity are rejected with `403`
- if you configure `ZEPHYR_PEER_VALIDATORS`, confirm the bound `<peer-url>=<validator-address>` pair matches what the peer proves in `GET /v1/status`

### Certified Block Production Is Rejected

- confirm `GET /v1/consensus` shows a non-empty validator set
- confirm `ZEPHYR_VALIDATOR_ADDRESS` matches the local validator you expect this node to represent, or let the private key derive it automatically
- confirm `GET /v1/consensus` reports the same address in `nextProposer`
- confirm `GET /v1/dev/block-template` and your proposal use the same `blockHash`, `previousHash`, `producedAt`, full `transactions`, and `transactionIds`
- confirm `GET /v1/consensus` shows a latest certificate for that same `blockHash`
- confirm you replay `POST /v1/dev/produce-block` with the same `producedAt` used by the certified template when you are using the manual path
- disable `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES` only if you intentionally want a looser local dev flow

## Recommended Local Demo Flow

1. Start Node A.
2. Start Node B as a replica.
3. Start the Vue wallet against Node A.
4. Create a wallet.
5. Fund the wallet with the local dev faucet.
6. Sign and broadcast a sample transaction.
7. Produce a block on Node A or enable automated certified consensus if you already have validator keys.
8. Inspect `GET /v1/status`, `GET /v1/peers`, `GET /v1/blocks/latest`, and `GET /v1/accounts/{address}` on both nodes.
9. If validator private keys are configured, confirm peer identity verification succeeds in `GET /v1/peers`.
10. Submit a validator election and inspect `GET /v1/validators` plus `GET /v1/consensus`.
11. Either submit a matching proposal and validator votes manually, or let the automated proposer and validators do it for the current round-0 flow.
12. Inspect the resulting block, vote tallies, and certificate on both nodes.
13. Optionally restart a node and confirm the validator snapshot and consensus artifacts survived.

