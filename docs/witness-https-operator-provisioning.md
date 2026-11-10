# Zephyr witness HTTPS publication: strict operator provisioning (P0)

**Status: local validation building block only. Public mainnet remains NO-GO.**

The code-level constructor `node.NewProvisionedMultiOperatorHTTPSWitnessFloor`
is a stricter opt-in alternative to the generic two-source witness verifier.
It builds a non-signing, independently signature-checked read path using:

- **3–8 distinct operator inventory identifiers**, checked for exact duplicates;
- **3–8 unique canonical DNS hostnames**, even when paths or ports differ;
- **4–16 pinned, unique Ed25519 public keys**, provided out-of-band for the
  same network and finalized validator root;
- strict HTTPS with no userinfo, credentials, redirects, proxy-from-environment,
  IP literals, loopback, local aliases, noncanonical paths or URL query;
- a single pinned-signature quorum policy applied independently to every
  origin, with **exact checkpoint agreement** across all reachable origins;
- context-aware HTTPS requests and conservative failure on any unavailable,
  stale, conflicting or invalid signed publication.

This constructor has **no private key material, signing operation or
mechanism to assert who actually owns an operator, host or witness key**.
An identifier and a DNS name are merely configuration labels. A quorum of
signatures does not, by itself, prove **publication freshness**. In-process
monotonic observations disappear if their storage is rolled back or a fresh
process is started.

## Operator evidence to collect outside CI

Before a production release, an independently reviewed operator roster must
associate each inventory ID and DNS host with a genuinely separate legal or
operational entity, infrastructure provider and key custody boundary. The
operator must sign its own provisioned identity/fingerprint, trusted genesis
and validator-root anchors and witness key IDs through an independently
authenticated process. Disclose shared control, DNS zones, hosting, networks,
administration and incident-response access, rather than claiming that
different subdomains imply separate failure domains.

A release exercise must record full signed witness bundles from all origins
during normal finality, stale-origin rejection, one-origin outage,
conflicting signed checkpoint injection, multi-host partitions, cold restore,
replay after crash and concurrent conflicting signing attempts. Check the
signatures and QC-backed finalized ancestry independently at each step.
Store evidence and signing-key rotation logs on separately controlled,
nonrollbackable media. Reconcile with a **globally serialized remote
reserve-and-sign HSM authority**, never local file locks.

The construction-time tests use synthetic TLS servers and local fixture
keys; **they are not operator attestations, HSM deployment evidence, external
audits or authorization to expose real assets**. Keep GitHub issues
[#42](https://github.com/the-code-learner/Zephyr-Chain/issues/42),
[#54](https://github.com/the-code-learner/Zephyr-Chain/issues/54) and
[#55](https://github.com/the-code-learner/Zephyr-Chain/issues/55)
OPEN until evidence is independently reviewed.
