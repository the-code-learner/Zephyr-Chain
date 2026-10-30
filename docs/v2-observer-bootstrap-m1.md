# M1 / P0 — Pinned Protocol v2 observer process

**Status: first-session development observer, NOT production mainnet.**

This tranche introduces `cmd/v2-observer`, a deliberately restricted
process that can initialize the actual Protocol v2 genesis, persist each
shard's canonical state via its protected WAL, and join the network as
a **non-voting** QUIC/libp2p observer. It does not import a validator
consensus private key and cannot propose or sign votes.

## Operator startup

First independently obtain and verify the genesis file, the canonical
64-hex NetworkID and the 64-hex initial validator-root commitment.
Use independent trustworthy channels to agree on both commitments,
not simply the RPC endpoint or the file's author.

```sh
go run ./cmd/zephyr-genesis-audit \
  -genesis /private/genesis.json \
  -expected-network-id '<independently-pinned-64-hex>' \
  -expected-validator-root '<independently-pinned-64-hex>'

go run ./cmd/v2-observer \
  -genesis /private/genesis.json \
  -expected-network-id '<independently-pinned-64-hex>' \
  -expected-validator-root '<independently-pinned-64-hex>' \
  -data-dir /private/observer-data \
  -listen /ip4/127.0.0.1/udp/0/quic-v1
```

For a restricted devnet operator may supply
`-peers '/ip4/127.0.0.1/udp/9900/quic-v1/p2p/<pinned-peer-ID>,...'`.
The default listener binds only loopback. Do not expose the reference
observer to untrusted public Internet traffic: consensus handler
admission quotas and rate limits are not yet production hardened.

The startup JSON reports the observer peer identity, *pinned* trust
anchors, and initial height 0; it does not imply independent network
finality beyond what the local validator set and the certified-commit
protocol actually verify.

## Hard stop before unsafe recovery

The current runtime does **not** reconstruct the full historical
consensus height and parent hash from an authenticated durable
checkpoint at process startup. A state root identical to genesis is
insufficient: finalized empty blocks can advance height without
changing any object. Resetting runtime height to 0 would be unsafe.

Therefore `cmd/v2-observer` has an intentionally conservative
**genesis + single-certified-successor** lifecycle:
- it verifies independent genesis roots for every shard before binding
  a network listener;
- it synchronizes an irreversible `observer-session-started` marker
  before creating the libp2p listener;
- it **allows a restart only if** the latest durable global-commit journal
  proves a single QC-certified height-1 successor directly from genesis;
  it verifies all independent genesis predecessor roots, current shard
  post-roots, and the original validator set before resuming;
- it refuses preexisting shard roots different from canonical genesis
  on first launch, or different from the certified height-1 journal on
  a recoverable restart;
- it uses owner-only private directories, separate exclusive state WAL
  locks and a persistent network-bound transport identity;
- it supports up to 16 genesis shards for bounded experimental startup;
- it refuses all multi-height recoveries, unknown validator rotations,
  missing/corrupt journals and unverified state. The global commit
  journal currently retains only the most recent certified commit.

Never delete the marker, restore an older copy of the marker/state,
manually rewrite height or reset a signer journal to bypass the guard.
The safe next milestone is a **QC-authenticated append-only multi-height
finality history and checkpoint replay** with negative partition/crash
and rollback tests. Certified height-1 recovery does not suffice for
production mainnet, backup restore or validator restart. Existing v2 test-only in-memory services remain
separate and are not an alternative production startup procedure.

## Validation

```sh
go test ./internal/v2/bootstrap ./cmd/v2-observer -count=1
go test -race ./internal/v2/bootstrap ./cmd/v2-observer -count=1
bash scripts/verify-v2.sh full
```

The tests prove that pinned genesis, initialized multi-shard roots,
non-voting behavior, invalid peer bindings, post-genesis state changes,
duplicate startup and an untrusted trust anchor are rejected as
expected. They do not establish independent multi-host mainnet
operations, secure protocol upgrades, production throughput, mobile
verification, VRF/beacon authorization, WASM contract safety or
economic policy activation.

**Mainnet launch remains NO-GO** pending independent audit and every
outstanding gate in `docs/mainnet-security-release-gates.md`.
