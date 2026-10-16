# Protocol v2 P2.1 — Bilateral native lending foundations

Status: implementation tranche. This is protocol functionality, not monetary-policy activation.

## Scope

P2.1 introduces deterministic bilateral lending of the native ZPH token with explicit lender and borrower identities.

The first tranche supports:

- lender-funded native principal escrow;
- borrower acceptance with optional native collateral;
- fixed whole-contract term rates in basis points;
- positive, zero and negative term rates;
- deterministic repayment amount;
- repayment with collateral return;
- lender cancellation before acceptance;
- collateral claim after maturity when a loan defaults.

The implementation intentionally limits lender and borrower to the same execution shard. Cross-shard lending will require an explicit message/receipt protocol and is not emulated by hidden trust or heuristic state transfer.

## Rate semantics

`TermRateBps` is the rate for the entire bilateral contract. It is **not an APR**.

The exact repayment amount is:

`principal + trunc_toward_zero(principal * term_rate_bps / 10_000)`

Negative rates are valid. For example, a principal of 300 ZPH at -1000 bps settles at 270 ZPH.

The current canonical bounds are:

- minimum: -9999 bps;
- maximum: +100000 bps.

The lower bound guarantees a strictly positive repayment. These are representation/safety bounds for the implementation tranche, not recommended market rates or monetary-policy parameters.

## State machine

### Offer

The lender locks the full native principal in a `KindLoanOffer` object. The offer names exactly one borrower and includes principal, optional collateral, term rate and maturity height.

### Accept

Only the named borrower may consume the offer. Acceptance:

- releases the principal to the borrower;
- locks the agreed collateral in a `KindLoan` object;
- records the accepted height;
- preserves the original bilateral terms.

Acceptance at or after maturity is rejected.

### Repay

The borrower consumes the live loan object and pays the deterministic repayment amount to the lender. Any locked collateral is returned to the borrower.

Because the loan object is consumed, a finalized repayment cannot be repeated against the same loan state.

### Default

Only the lender may default a live loan, and only strictly after the maturity height. Default consumes the loan object and transfers the locked collateral to the lender.

A default at the maturity height itself is rejected.

### Cancel

Before acceptance, only the lender may consume the offer and recover its principal.

## Conservation and fees

Principal and collateral are accounted as internally locked native value by the execution engine. Every transition must balance exact native inputs, generated payouts, change and transaction fee.

The protocol does not mint ZPH to fund positive or negative rates. A negative-rate loan is an explicit bilateral transfer of value from lender to borrower over the contract lifecycle.

## Current boundary

This tranche does not yet include:

- cross-shard lending;
- floating/index-linked rates;
- partial repayment;
- refinancing;
- liquidation auctions;
- oracle-priced collateral;
- external-chain collateral;
- protocol credit creation or monetary issuance;
- DeviceCredential or ZECI integration.

Those remain later P2 work.

The project discipline remains: correctness and finalized evidence first; economic activation only after explicit later gates.
