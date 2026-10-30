# Protocol v2 — Governance proposal and timelock foundations

P3.2 introduces canonical governance proposal and block-height timelock primitives.

This tranche is deliberately **inert**. It does not grant governance authority, create voting power, schedule validator changes, activate economic policy or mutate protocol parameters.

## Canonical proposal commitment

A governance `Proposal` commits to:

- proposer account;
- action class;
- action-specific canonical hash;
- creation height;
- non-zero nonce.

Current action classes are:

- protocol parameter change;
- validator transition;
- economic activation.

The action hash is only a commitment. **Hashing or storing a claim does not make it valid or authorized.** A later action-specific protocol must define the exact canonical payload and validate every field from consensus-verifiable evidence.

## Timelock

A `Timelock` commits to:

- proposal hash;
- authorization-evidence hash;
- finalized authorization height;
- earliest execution height;
- optional expiry height.

The canonical invariant is:

`AuthorizedHeight < ExecuteAfterHeight < ExpiresHeight`

when an expiry exists.

A caller may additionally apply a policy-specific minimum delay through `ValidateMinDelay`. The primitive does not hard-code a policy delay because different future governance domains may require different bounded delays.

Execution is permitted by the primitive only inside the height window. There is no wall-clock dependency.

## Authorization boundary

`AuthorizationHash` is a commitment field, **not proof by itself**.

This tranche contains no transaction operation or execution hook that can create authority from an arbitrary hash. Before governance timelocks become consensus-active, a later protocol layer must derive authorization from evidence validators can verify independently.

Examples of acceptable future evidence may include finalized Zephyr governance state, quorum-backed protocol state, or another explicitly defined proof system. Administrator signatures, RPC assertions, external APIs or off-chain multisig claims do not become valid merely by being hashed on-chain.

## Validator transitions

Existing Protocol v2 validator trust remains unchanged:

1. the currently trusted validator set finalizes a header;
2. the header may commit `NextValidatorRoot`;
3. only after the current committee's normal QC finalizes that header does the next root become trusted.

A future governance rule may decide **which** validator transition is eligible to be proposed, but it must not bypass the QC-backed validator-root trust chain.

P3.2 does not call `ScheduleValidatorTransition`, modify `NextValidatorRoot`, or change voting power.

## Economic activation

The `ActionEconomicActivation` class reserves a canonical governance domain; it does not activate minting, idle levies, reward routing, fee splits, supply mutation or any other monetary mechanism.

The existing discipline remains mandatory:

`measure -> simulate -> activate`

Any future economic action needs its own bounded canonical payload, observation requirements, simulations, authorization proof and timelock policy before an execution path can exist.

## Relationship to Citizen evidence and VRF committees

P3.1 provides finalized-credential-bound Citizen challenge evidence. P3.2 provides inert governance/timelock commitments.

Neither layer assigns ContributionScore, governance weight or committee membership.

The repository does not currently contain a cryptographic VRF primitive. Deterministic weighted proposer scheduling and ECDSA/QC hashes must not be described as VRF randomness. A later committee tranche must introduce or select an explicit independently verifiable randomness construction before VRF-selected committees can become protocol state.

## Non-activation checklist

P3.2 adds no:

- governance voting algorithm;
- governance weight;
- proposal execution transaction;
- validator-set mutation path;
- monetary activation;
- trusted oracle;
- external-price dependency;
- ContributionScore;
- reward or supply mutation.

The purpose of this tranche is to make future authorization and delay semantics canonical before any live governance control plane is connected.
