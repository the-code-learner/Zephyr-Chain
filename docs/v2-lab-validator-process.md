# V2 lab validator: protected bootstrap tranche

Issue #211 remains P0 and **OPEN**. This tranche creates a real standalone `cmd/v2-lab-validator` process and a `bootstrap.OpenLabValidator` constructor. The constructor reuses existing pinned-network/pinned-validator-root observer startup, disk world-state and QC history, adds checked P-256 membership and a durable OS-locked signing journal, then installs a protected V2 consensus handler. A second holder of the same signer journal is refused before additional state initialization. User state and signer journal paths must be absolute, disjoint, and not reached through symlinked parents.

The command requires explicit `--acknowledge-lab-only`, a genesis named lab/devnet, separate owner-only DER key file, manually pinned IDs and optional pinned libp2p peers. It does not place signing secrets in flags or logs.

**Not yet demonstrated:** multi-process connected proposer/vote/finality QC, remote signer/HSM, worker/operator diversity, or safe validator restart at later finalized heights. This is a starting primitive for the Docker validator orchestration; it must never be described as mainnet ready. #42/#54/#55 remain NO-GO.

Next: add GitHub-only 4/7-process Docker fixture with per-process keys, origin-pinned peers, signed-QC proof, crash/restart and error artifacts.