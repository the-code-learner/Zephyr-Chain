# Protocol v2 P2.2 — Explicit cross-shard bilateral lending

P2.2 extends bilateral native ZPH lending across Zephyr shards without trusted oracles and without hidden cross-shard state access.

All cross-shard state movement uses the existing finalized receipt protocol:

1. source transaction finalizes;
2. a canonical `CrossShardReceipt` is committed in the source shard receipt root;
3. destination verifies global header/QC, shard commitment proof and receipt proof;
4. destination materializes `DestinationObject()` plus the durable receipt marker;
5. replay is rejected by consensus state.

## Successful lifecycle

### 1. Cross offer

The lender submits `OpCrossLoanOffer` on the lender shard.

The principal is removed from ordinary coin spendability and carried by a finalized `KindCrossLoanOffer` receipt to the borrower shard. No local shadow copy of the principal remains spendable.

### 2. Acceptance

After the offer receipt is imported, the named borrower consumes it with `OpCrossLoanAccept`.

Acceptance atomically:

- releases the principal as a local borrower coin;
- creates a borrower-side `KindCrossLoanActivation` mirror containing no locked value;
- locks the agreed collateral;
- sends that collateral back to the lender shard inside a finalized `KindCrossLoanAcceptance` receipt.

The acceptance receipt itself is the lender-shard authoritative live collateral object.

### 3. Repayment

Before or at `MaturityHeight`, the borrower consumes the activation mirror and pays the deterministic repayment amount.

`OpCrossLoanRepay` sends the repayment value to the lender shard in a finalized `KindCrossLoanRepayment` receipt. The message commits to the acceptance object, activation object, terms hash, amount and finalized paid height.

Negative whole-contract rates remain supported exactly as in P2.1. No ZPH is minted.

### 4. Settlement

After both acceptance and repayment receipts are imported on the lender shard, `OpCrossLoanSettle` can be executed by a fee payer.

Settlement verifies the message linkage and terms, then:

- pays the exact due amount to the lender locally;
- consumes acceptance collateral and repayment value;
- returns collateral to the borrower through a normal native-coin cross-shard receipt.

The borrower imports that coin receipt normally.

## Default and expiry

A borrower repayment operation is rejected after `MaturityHeight`.

Cross-shard default is deliberately delayed by `CrossShardSettlementGraceBlocks` after maturity. This gives a finalized repayment receipt a deterministic ordinary import window before collateral can be claimed by the lender.

After the grace window:

- the lender may consume an unsettled acceptance and claim its locked collateral;
- an unaccepted offer imported on the borrower shard may be expired permissionlessly, returning the locked principal to the lender through a native-coin receipt.

The grace window reduces normal receipt-latency races. It is **not** claimed to provide censorship-proof fair exchange under an adversarial indefinitely censoring proposer set. Stronger fair-exchange/challenge semantics would be a separate protocol extension.

## Value accounting

Three non-coin message kinds carry explicit native ZPH value:

- cross offer: principal;
- cross acceptance: collateral;
- cross repayment: repayment amount.

The economics collector recognizes these finalized receipts so per-shard circulating-supply telemetry follows value movement. The IdleCapitalTracker follows the same movements using the exact canonical `CrossShardReceipt.Hash()` as TransferID.

Metadata-only activation objects carry no native value.

## Oracle boundary

No step reads an external asset price, API, oracle feed or off-chain assertion.

All conditions are functions of:

- signed bilateral terms;
- finalized Zephyr height;
- deterministic repayment arithmetic;
- finalized receipt/QC/Merkle evidence;
- object consumption/replay state.

P2.2 therefore preserves the project-wide rule: consensus-critical decisions use evidence validators can verify themselves.
