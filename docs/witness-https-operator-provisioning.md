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

For strict provisioned HTTPS readers, a **single aggregate five-second
request budget** bounds the full sequential read across all origins, not
five seconds multiplied by the number of operators. A caller's earlier
deadline takes precedence. If any origin stalls or the aggregate deadline
expires, recovery/signing authorization fails closed; there is no partial
quorum or cache fallback. Generic fixture adapters retain their previous
context behavior. The budget is a defensive latency bound, **not** a
publication-freshness, uptime or liveness guarantee.

The legacy two-or-more-source `NewOperatorSeparatedWitnessFloor` constructor
now also applies the same **single aggregate five-second read deadline**
across its HTTPS origins, rather than allowing up to five seconds per source.
This corrects a cancellation-latency gap but does **not** upgrade legacy
operator configuration to the strict three-operator attested trust model.
For public-mainnet provisioning use independently authenticated inventories
and meet the separate physical operator and HSM/audit release gates.

This constructor has **no private key material, signing operation or
mechanism to assert who actually owns an operator, host or witness key**.
An identifier and a DNS name are merely configuration labels. A quorum of
signatures does not, by itself, prove **publication freshness**. In-process
monotonic observations disappear if their storage is rolled back or a fresh
process is started.

## Optional pinned Ed25519 operator inventory signatures

`node.NewAttestedMultiOperatorHTTPSWitnessFloor` accepts
`WitnessHTTPSOperatorAttestation` records and separately pinned
**operator inventory keys**, in addition to the quorum witness public
keys. Operators sign the canonical, domain-separated
`WitnessHTTPSOperatorAttestationDigest` over NetworkID, validator
root, **expected externally pinned roster revision**, operator label,
canonical HTTPS endpoint and operator key. All provisioned operator
keys must sign exactly one entry, remain distinct from the witness
checkpoint-signing keys and match the previously authenticated roster.

Every malformed, missing, substituted or replayed signature fails closed
**before the construction of the HTTPS floor readers**. A higher roster
revision is accepted only when the independently controlled caller
updates its expected revision; a restored/rolled-back local source must
not be trusted to supply that minimum. No key material for any signing
operation is present in the constructed floor.

This is proof of **possession of inventory signing keys**, not proof of
hardware custody, independent operator control, secure DNS, uptime,
remote monotonic signing or freshness of published checkpoints. An
independent auditor must verify who actually possesses each attestation
key and controls each witness key and deployment failure domain.

## Challenge-authenticated live HTTPS publication (opt-in P0)

`node.NewChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor` first verifies
the exact signed inventory as above, then requires a **fresh per-GET
challenge response from every pinned operator**. This adds a cryptographic
reachability/key-possession check missing from a static, quorum-signed
checkpoint bundle. The non-signing observer generates an independent 32-byte
OS-random challenge for each HTTPS GET:

- Request: `X-Zephyr-Witness-Challenge: <64 lowercase hexadecimal characters>`;
  `Cache-Control: no-store` and `Pragma: no-cache`.
- Response: canonical quorum-signed JSON checkpoint bundle,
  `Content-Type: application/json` and exactly one
  `X-Zephyr-Witness-Operator-Signature: <128 lowercase hexadecimal characters>`.
- The operator signs `WitnessHTTPSPublicationChallengeDigest` using a
  **separately pinned online publication Ed25519 key**, distinct from
  both offline inventory issuer and quorum witness signer keys.
  The domain-separated digest commits to NetworkID, genesis validator root,
  operator ID, externally pinned roster revision, request challenge, and
  SHA-256 of the **exact** JSON response bytes.
- The reader rejects missing/duplicated/malformed/foreign/replayed
  signatures, changed content, noncanonical media, absent operators and
  cancelled requests before the existing >2/3 checkpoint signature and
  strict all-origin equality checks. The aggregate 5-second budget remains.

**Important security boundary:** This is proof that the independently pinned
operator-specific publication key signed the exact response to a fresh nonce, not proof that
the operator checked new QC finality, that the signed checkpoint is at the
current chain tip, or that its private key is guarded by nonrollbackable HSM.
A dishonest operator can sign an older still-valid checkpoint in response to
a new nonce. The independently authenticated monotonic high-water policy,
true freshness SLA, QC ancestry, 3+ real operators, key-role isolation,
cross-host crash/recovery drills and external audit are still mandatory.
An intermediary must not be treated as an operator merely because it can
relay the challenge; the signing endpoint and publication key custody need
independent operational attestation. Publication keys must be pinned **out of
band in the exact signed roster order**; the constructor rejects missing,
duplicate, inventory-issuer or witness-signing key reuse. The inventory
signing keys should remain offline and MUST NOT be exposed to the live HTTPS
publisher. A separate signed publication-key delegation/rotation ceremony
and independent verification are needed before production key rotation. Synthetic HTTPS regression tests are
not launch evidence. **PUBLIC MAINNET NO-GO.**

## Offline-signed online publication key delegation (P0)

`node.NewDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor`
accepts one `WitnessHTTPSPublicationDelegation` per signed operator roster
entry. The delegated **online** publication public key and exact
OperatorID/revision are signed with that operator's separately pinned
**offline inventory issuer Ed25519 key** using the canonical
`WitnessHTTPSPublicationDelegationDigest` over NetworkID, original
validator root, OperatorID, expected inventory revision and online key.
Delegations must match the signed roster **in exact order**, verify under
the issuer corresponding to that entry, and have the externally pinned
revision. Role separation still forbids using the online publication key
as a witness checkpoint key, a second operator's publication key, or an
offline inventory issuer key. Every failure denies constructing a live
reader; no cached unsigned fallback.

The offline issuer signs delegation bytes in a controlled ceremony, never
by running on a live HTTPS publisher. A delegation is an authorization
for an online **response signature**, not a capability to mint finality or
sign validator votes. A properly signed but **older** delegation can be
replayed if an operator restores the independently pinned expected roster
revision, and an offline issuer can equivocate by signing two different
keys at the same revision. This mechanism alone is **NOT** authenticated
revocation, freshness SLA, a monotonically updated key registry, real
hardware isolation, or production HSM-backed key custody. An independent
auditor must review issuance, operator key history, revocation/overlap and
recovery before mainnet authorization. **PUBLIC MAINNET NO-GO.**

## Challenge-bound HTTPS publisher adapter (P0 reference server)

The `node.NewWitnessHTTPSChallengePublicationHandler` provides the **server**
side of the opt-in client protocol above. It is a narrow, non-key-owning
`http.Handler` for `GET /v1/checkpoint` on a genuine TLS listener:

1. Require exactly one canonical 256-bit lower-hex challenge request header,
   no query/escaped target/body or plaintext request.
2. Obtain an independently configured canonical quorum-signed checkpoint
   bundle through a context-aware source (no arbitrary caller-supplied
   checkpoint or sign-height API), within a shared five-second deadline.
3. Verify every signer and the >2/3 threshold against **separately pinned
   witness public keys**, then use a distinct, genesis-anchored
   `WitnessFinalityVerifier` to validate the checkpoint's native QC ancestry.
4. Only after these checks delegate the domain-separated response digest
   to a provider implementing `WitnessHTTPSPublicationSigner`. The server
   receives a signature, checks it against its pinned **online publication
   public key** and responds with the exact original bundle bytes and the
   single canonical signature header. The offline inventory issuer key and
   witness checkpoint private keys never enter the HTTP handler.

The handler checks pinned key-role separation, refusing witness keys reused
as publication or inventory keys and refusing inventory/publication reuse.
Malformed headers, invalid QC/signature or signer errors fail closed before
any authenticated publication. The handler does **not** create or configure
the listener: production needs separately audited DNS/TLS certificates,
correct process isolation, rate limiting, operator custody, independent
on-host QC validation and genuine remote nonrollbackable key protection.
The injected `WitnessFinalityVerifier` is a *critical trust dependency*;
test stubs or unverified RPC assertions cannot be production inputs.
Signing a fresh nonce still cannot certify publication freshness if an
operator deliberately re-signs an old valid checkpoint. **Mainnet NO-GO**.

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
