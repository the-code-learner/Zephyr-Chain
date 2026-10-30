# Protocol v2 — No trusted oracle rule

Consensus-critical state transitions must not depend on external claims that Zephyr validators cannot independently verify.

Writing a value on-chain does not make the underlying claim true. A price feed, API response, vendor attestation, administrator signature or multisig assertion is still trusted external input unless the protocol can verify the evidence itself.

## Consequences

Zephyr must not use trusted oracle feeds for:

- liquidation thresholds based on external fiat prices;
- collateral valuation based on unprovable market prices;
- monetary activation;
- contribution/economic credit;
- device or hardware claims;
- governance conditions;
- cross-chain asset existence.

## Permitted evidence

Consensus may use evidence that validators can verify deterministically, including:

- finalized Zephyr state and Merkle proofs;
- Zephyr quorum certificates;
- canonical cross-shard receipts with finalized commitment proofs;
- deterministic cryptographic signatures;
- registered, finalized compute settlement evidence;
- future foreign-chain light-client proofs where Zephyr verifies the relevant header/finality/state proof itself.

A foreign-chain light client can prove that an asset or event exists on that chain. It does **not** prove an external fiat price. External price discovery therefore remains outside consensus unless a future construction makes the claimed fact independently verifiable.

## Economic telemetry

Shadow indices may consume only finalized/verifiable protocol evidence. Untrusted advertised capacity, provider claims, API summaries, market-price feeds and theoretical hardware performance are not canonical observations.

This rule applies across lending, DeviceCredential, ZECI, economics, governance and future cross-chain collateral.
