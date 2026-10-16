# Zephyr Protocol v2 — Validator Trust Chain

This document specifies how validators, full nodes, cross-shard receipt importers and Citizen wallets identify the validator committee authorized to finalize a Zephyr v2 header.

## Why a quorum certificate is not enough by itself

A set of arbitrary keys can always create a self-consistent certificate for data they signed. Therefore a verifier must establish two facts independently:

1. the votes reach Zephyr's normal `2/3+` voting-power quorum for a validator set;
2. that exact validator set is itself authorized by the already-trusted Zephyr chain.

Zephyr v2 commits validator identity, P-256 public key and integer voting power into a canonical Merkle root. A header is only valid against a validator set whose calculated root equals `GlobalHeader.ValidatorRoot`.

## Genesis trust anchor

The canonical v2 genesis derives:

```text
NetworkID     = H(canonical genesis)
ValidatorRoot = Merkle(initial validator set)
```

`genesis.Config.TrustAnchor()` exposes these two values. A Citizen wallet can embed a known genesis or import an explicitly trusted checkpoint and needs no trusted RPC server to invent the committee for it.

## Header transition rule

Each canonical `GlobalHeader` contains:

```text
ValidatorRoot       current committee
NextValidatorRoot   committee authorized for the next height
```

If `NextValidatorRoot` is zero, it means "unchanged" and the effective next root is `ValidatorRoot`.

A committee transition is therefore:

```text
trusted committee N
        |
        | signs GlobalHeader H
        v
ValidatorRoot     = root(N)
NextValidatorRoot = root(N+1)
        |
        | 2/3+ QC from N
        v
committee N+1 becomes trusted for H+1
```

The next committee does not authorize the block that installs itself. The currently trusted committee must finalize the transition first.

## Validator behavior

Before voting, a validator verifies all of the following:

- proposal signature and scheduled proposer;
- proposal network and height;
- local validator-set root equals `Header.ValidatorRoot`;
- local deterministic execution produces the same `GlobalHeader` consensus hash;
- normal transaction/state/shard commitment rules.

Only then may it sign a vote.

## Runtime activation

`node.ScheduleValidatorTransition` places the next set root in the candidate before proposal signing.

`node.CommitWithValidatorTransition` first performs the normal QC-backed state commit using the current set. Only after successful finalization does the runtime advance its local `ValidatorRoot` to the header's effective next root.

The following block therefore requires the new validator set.

Governance/staking will eventually decide *which* transition is allowed to be proposed; the cryptographic transition rule described here remains the consensus trust boundary.

## Citizen wallet behavior

The strict wallet verifier in `apps/wallet/src/lib/v2CitizenTrusted.ts` starts from a `CitizenTrustAnchor` containing a trusted `NetworkID` and `ValidatorRoot`.

For every proof bundle it independently checks:

- header network equals the trusted network;
- supplied validator set hashes to the trusted/current `ValidatorRoot`;
- validator IDs match their P-256 public keys;
- every vote signature is canonical low-S P-256 and targets the same header;
- distinct signed voting power reaches `2/3+` using exact `BigInt` arithmetic;
- certificate hash matches the finalized header;
- shard commitment belongs to the finalized global commitment root;
- object inclusion/absence proof matches the shard state root.

After all checks pass, the wallet may advance to:

```text
nextTrustAnchor = {
  network: current NetworkID,
  validatorRoot: effective NextValidatorRoot
}
```

A malicious RPC can transport headers, validator sets and proofs, but it cannot choose a new trusted validator committee without a QC from the currently trusted committee.

## Cross-shard receipts

Cross-shard imports apply the same rule. A destination shard does not accept a source receipt merely because the supplied source validator set signed it. It verifies that the supplied set hashes to the source header's `ValidatorRoot`, verifies the QC, then verifies shard and receipt Merkle proofs before materializing destination state.

## Recovery and checkpoints

Future checkpoint/snapshot formats must carry enough finalized header history to bridge any validator-set transitions between the checkpoint already trusted by the verifier and the target state. Snapshot authentication never replaces this trust chain.

## Security invariant

```text
valid signatures
      !=
authorized validators

valid signatures
+ committed validator root
+ previously trusted transition
      =
authorized finality evidence
```
