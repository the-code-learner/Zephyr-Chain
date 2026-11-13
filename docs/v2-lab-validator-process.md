# V2 lab validator: protected bootstrap tranche

Issue #211 remains P0 and **OPEN**. This tranche creates a real standalone `cmd/v2-lab-validator` process and a `bootstrap.OpenLabValidator` constructor. The constructor reuses existing pinned-network/pinned-validator-root observer startup, disk world-state and QC history, adds checked P-256 membership and a durable OS-locked signing journal, then installs a protected V2 consensus handler. A second holder of the same signer journal is refused before additional state initialization. User state and signer journal paths must be absolute, disjoint, and not reached through symlinked parents.

The command requires explicit `--acknowledge-lab-only`, a genesis named lab/devnet, separate owner-only DER key file, manually pinned IDs and optional pinned libp2p peers. It does not place signing secrets in flags or logs.

**Not yet demonstrated:** multi-process connected proposer/vote/finality QC, remote signer/HSM, worker/operator diversity, or safe validator restart at later finalized heights. This is a starting primitive for the Docker validator orchestration; it must never be described as mainnet ready. #42/#54/#55 remain NO-GO.

Next: add GitHub-only 4/7-process Docker fixture with per-process keys, origin-pinned peers, signed-QC proof, crash/restart and error artifacts.

## Four-process GitHub lab — second tranche

The `V2 Four-Process Lab Validator Smoke` workflow builds the lab-only V2 validator command and a short-lived fixture generator. Four random P-256 keys are generated with owner-only DER files **outside** the checkout and uploaded artifacts, all validators derive identical canonical genesis commitments, and each starts as a **separate OS process** with a unique durable signing journal and QUIC listener. The harness asserts unique process IDs and libp2p peer identities, verifies that a second process using a held signer journal fails *before opening state*, and checks three processes survive termination of a fourth. On failure, logs are preserved; ephemeral private keys are deleted.

**Evidence boundary:** Four processes share **one** GitHub runner VM and network namespace. This proves startup, key identity, journal exclusivity and process crash isolation only. It has **not** connected four mutually pinned peers to finalize a V2 block, has no QC/observer verifier, and does not establish cross-runner or independent-operator custody. The mainnet #42 and V2 #211 acceptance gates remain open.


## Real signed V2 QC, four independent processes on one runner (third tranche)

The `V2 Four-Process Signed QC Lab` workflow runs four separate libp2p QUIC validator processes and uses an explicit `-lab-control-socket` under each owner's private state directory. The optional control endpoint is a **0600 Unix-domain socket only**, never a TCP/public RPC. The harness supplies authenticated full `/p2p/<PeerID>` pinned peer addresses, rejects an anonymous address, and asks each node to propose until the scheduled proposer finalizes a height-one **empty test block**. Every replica must independently commit the same consensus header with >=3 signed votes. The complete signed snapshot is uploaded and a separate Go verifier calls `lightapi.Snapshot.Validate()` against pinned genesis commitments and the expected header hash. All keys stay in ephemeral, non-artifact directories.

This is a real cross-process protocol/signed QC test, **not** a TPS benchmark or a multi-host/independently administered network. It does not yet test a non-voting observer, cross-runner authenticated private overlay, signer rollback, network partitions, long-run liveness, or recovery after nonempty finalized state. These remain hard P0 #211 acceptance items. Do not use this test control API in production.


## Adversarial 4/7-process signed QC coverage

The signed QC GitHub workflow now runs a strict two-cell matrix of **4 and 7 real validator OS processes** on one runner each. Signed proof acceptance is checked against the correct greater-than-two-thirds threshold (3/4 or 5/7) and the pinned genesis. A separate verifier must also **reject** a syntactically valid snapshot whose first P-256 vote signature was mutated. The workflows fail on insufficient quorum, replica divergence or an accepted corrupt certificate; artifacts are per-topology and tied to the exact SHA. This is single-host transport/finality assurance, not independent operators, a cross-runner network, production TPS or rollback-resistant key custody. Issue #211 remains P0 OPEN.


## Live nonvoting observer QC (next GitHub tranche)

The 4/7 V2 process lab additionally boots a **separate observer-only OS process**, with no validator P-256 signing key or signing journal. The observer requires the same independent genesis/network/validator-root pins, joins through an authenticated `/p2p/<PeerID>` QUIC address and receives the QC-certified commit from the proposer. Its optional 0600 local Unix socket exposes **GET-only**, cryptographically validated finalized height/hash/vote count with `isValidator=false`. The lab asserts it starts at genesis, is never counted in voting quorum, and converges to exactly the same signed height-one header as the validators. This is not independently hosted or independently administered; state rollback, multiheight restart, partitions and external custody remain unproven.


## Certified observer restart and corrupted-history rejection

After a successful 4- or 7-validator height-one QC plus separate nonvoting observer confirmation, the same GitHub fixture now **stops all processes**, restarts only the observer from its persistent state directory, and requires it to reconstruct the exact previously finalized height and consensus header hash via verified genesis-to-tip signed history replay. It then appends one deliberately invalid byte to the observer's private *test* `certified-history.log` file and demands that a subsequent observer process refuse startup without emitting a ready event. Metadata and sanitized logs are stored as artifacts; ephemeral private keys stay outside the artifact tree. This is a same-runner simulated corruption test, not evidence of externally anchored nonrollbackable storage or independent hosts.
