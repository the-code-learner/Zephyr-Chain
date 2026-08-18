from pathlib import Path
import re


def replace_exact(path, old, new, expected=1):
    p = Path(path)
    s = p.read_text()
    count = s.count(old)
    if count != expected:
        raise SystemExit(f"{path}: expected {expected} occurrences of {old!r}, found {count}")
    p.write_text(s.replace(old, new))


def replace_regex(path, pattern, replacement, expected=1):
    p = Path(path)
    s = p.read_text()
    s2, count = re.subn(pattern, replacement, s, flags=re.S)
    if count != expected:
        raise SystemExit(f"{path}: expected {expected} regex replacements for {pattern!r}, found {count}")
    p.write_text(s2)


store = "internal/ledger/store.go"
replace_exact(
    store,
    '"github.com/zephyr-chain/zephyr-chain/internal/dpos"\n\t"github.com/zephyr-chain/zephyr-chain/internal/tx"',
    '"github.com/zephyr-chain/zephyr-chain/internal/dpos"\n\t"github.com/zephyr-chain/zephyr-chain/internal/protocol"\n\t"github.com/zephyr-chain/zephyr-chain/internal/tx"',
)
replace_exact(
    store,
    'type Block struct {\n\tHeight           uint64        `json:"height"`',
    'type Block struct {\n\tChainID          string        `json:"chainId"`\n\tHeight           uint64        `json:"height"`',
)
replace_exact(
    store,
    '\tPreviousHash     string        `json:"previousHash"`\n\tProducedAt',
    '\tPreviousHash     string        `json:"previousHash"`\n\tStateRoot        string        `json:"stateRoot"`\n\tProducedAt',
)
replace_regex(
    store,
    r'(type Snapshot struct \{.*?\tPeerSyncIncidents       \[\]PeerSyncIncident      `json:"peerSyncIncidents"`\n)\}',
    r'\1\tProof                   SnapshotProof            `json:"proof"`\n}',
)
replace_exact(
    store,
    'type Store struct {\n\tmu                    sync.RWMutex',
    'type Store struct {\n\tmu                    sync.RWMutex\n\tchainID               string',
)
replace_exact(
    store,
    'func NewStore(dataDir string) (*Store, error) {\n\tif dataDir == "" {',
    'func NewStore(dataDir string) (*Store, error) {\n\treturn NewStoreWithChainID(dataDir, protocol.DefaultChainID)\n}\n\nfunc NewStoreWithChainID(dataDir string, chainID string) (*Store, error) {\n\tchainID = protocol.ConfiguredChainID(chainID)\n\tif err := protocol.ValidateChainID(chainID); err != nil {\n\t\treturn nil, err\n\t}\n\tif dataDir == "" {',
)
replace_exact(
    store,
    '\tstore := &Store{\n\t\tdataDir:',
    '\tstore := &Store{\n\t\tchainID:               chainID,\n\t\tdataDir:',
)
replace_exact(
    store,
    '\t_, block, err := produceBlockFromState(state, maxTransactions, producedAt)',
    '\t_, block, err := produceBlockFromState(state, maxTransactions, producedAt, s.chainID)',
)
replace_exact(
    store,
    '\t\tnextState, block, err = produceCertifiedBlockFromState(state, producedAt)',
    '\t\tnextState, block, err = produceCertifiedBlockFromState(state, producedAt, s.chainID)',
)
replace_exact(
    store,
    '\t\tnextState, block, err = produceBlockFromState(state, maxTransactions, producedAt)',
    '\t\tnextState, block, err = produceBlockFromState(state, maxTransactions, producedAt, s.chainID)',
)
replace_exact(
    store,
    '\tnextState, err := importBlockIntoState(state, block)',
    '\tnextState, err := importBlockIntoState(state, block, s.chainID)',
)
replace_exact(
    store,
    'func produceBlockFromState(state persistedState, maxTransactions int, producedAt time.Time) (persistedState, Block, error) {',
    'func produceBlockFromState(state persistedState, maxTransactions int, producedAt time.Time, chainID string) (persistedState, Block, error) {',
)
replace_exact(
    store,
    '\tif producedAt.IsZero() {\n\t\tproducedAt = time.Now().UTC()\n\t}\n\tblock := Block{',
    '\trootState := state\n\trootState.Accounts = accounts\n\tstateRoot, err := stateRootFromState(chainID, rootState)\n\tif err != nil {\n\t\treturn state, Block{}, err\n\t}\n\n\tif producedAt.IsZero() {\n\t\tproducedAt = time.Now().UTC()\n\t}\n\tblock := Block{\n\t\tChainID:          chainID,\n\t\tStateRoot:        stateRoot,',
)
replace_exact(
    store,
    'func importBlockIntoState(state persistedState, block Block) (persistedState, error) {\n\tstate = normalizeState(state)',
    'func importBlockIntoState(state persistedState, block Block, chainID string) (persistedState, error) {\n\tstate = normalizeState(state)\n\tif block.ChainID != chainID || block.StateRoot == "" {\n\t\treturn state, ErrInvalidBlock\n\t}',
)
replace_exact(
    store,
    '\t\tif err := envelope.ValidateStatic(); err != nil {',
    '\t\tif err := envelope.ValidateForChain(chainID); err != nil {',
    1,
)
replace_exact(
    store,
    '\tsanitized := Block{\n\t\tHeight:',
    '\trootState := state\n\trootState.Accounts = accounts\n\tstateRoot, err := stateRootFromState(chainID, rootState)\n\tif err != nil || stateRoot != block.StateRoot {\n\t\treturn state, ErrBlockInvariant\n\t}\n\n\tsanitized := Block{\n\t\tChainID:          chainID,\n\t\tStateRoot:        stateRoot,\n\t\tHeight:',
)
replace_exact(
    store,
    'return consensus.BlockHash(block.Height, block.PreviousHash, block.ProducedAt, block.TransactionIDs)',
    'return consensus.BlockHash(block.ChainID, block.Height, block.PreviousHash, block.ProducedAt, block.StateRoot, block.TransactionIDs)',
)

cs = "internal/ledger/consensus_state.go"
replace_exact(
    cs,
    '\tstate := s.snapshotLocked()\n\tnextState, err := recordProposalIntoState(state, proposal)',
    '\tif err := proposal.ValidateForChain(s.chainID); err != nil {\n\t\treturn err\n\t}\n\tstate := s.snapshotLocked()\n\tnextState, err := recordProposalIntoState(state, proposal)',
)
replace_exact(
    cs,
    '\tstate := s.snapshotLocked()\n\tnextState, tally, certificate, err := recordVoteIntoState(state, vote)',
    '\tif err := vote.ValidateForChain(s.chainID); err != nil {\n\t\treturn VoteTally{}, nil, err\n\t}\n\tstate := s.snapshotLocked()\n\tnextState, tally, certificate, err := recordVoteIntoState(state, vote)',
)

snap = "internal/ledger/snapshot_security.go"
replace_exact(
    snap,
    '"time"\n\n\t"github.com/zephyr-chain/zephyr-chain/internal/protocol"',
    '"time"\n\n\t"github.com/zephyr-chain/zephyr-chain/internal/consensus"\n\t"github.com/zephyr-chain/zephyr-chain/internal/protocol"',
)

Path("internal/ledger/peer_snapshot_restore.go").write_text(
    '''package ledger\n\nimport "time"\n\n// RestoreFromPeerSnapshot is intentionally disabled. Peer snapshots require\n// quorum proofs from the locally trusted validator set.\nfunc (s *Store) RestoreFromPeerSnapshot(snapshot Snapshot, now time.Time) error {\n\treturn ErrSnapshotQuorumRequired\n}\n'''
)

server = "internal/api/server.go"
replace_exact(
    server,
    '"github.com/zephyr-chain/zephyr-chain/internal/ledger"\n\t"github.com/zephyr-chain/zephyr-chain/internal/tx"',
    '"github.com/zephyr-chain/zephyr-chain/internal/ledger"\n\t"github.com/zephyr-chain/zephyr-chain/internal/protocol"\n\t"github.com/zephyr-chain/zephyr-chain/internal/tx"',
)
replace_exact(server, 'type Config struct {\n\tDataDir', 'type Config struct {\n\tChainID                      string\n\tDataDir')
replace_exact(
    server,
    '\treturn Config{\n\t\tDataDir:',
    '\treturn Config{\n\t\tChainID:                      protocol.DefaultChainID,\n\t\tDataDir:',
)
replace_exact(
    server,
    'type StatusResponse struct {\n\tNodeID',
    'type StatusResponse struct {\n\tChainID                       string                           `json:"chainId"`\n\tNodeID',
)
replace_exact(server, '\tstore, err := ledger.NewStore(config.DataDir)', '\tstore, err := ledger.NewStoreWithChainID(config.DataDir, config.ChainID)')
replace_exact(
    server,
    '\tresponse := StatusResponse{\n\t\tNodeID:',
    '\tresponse := StatusResponse{\n\t\tChainID:                       s.config.ChainID,\n\t\tNodeID:',
)
replace_exact(
    server,
    '\tconfig.ValidatorAddress = strings.TrimSpace(config.ValidatorAddress)',
    '\tconfig.ChainID = protocol.ConfiguredChainID(config.ChainID)\n\tconfig.ValidatorAddress = strings.TrimSpace(config.ValidatorAddress)',
)
if "request.ValidateStatic()" in Path(server).read_text():
    replace_exact(server, "request.ValidateStatic()", "request.ValidateForChain(s.config.ChainID)", 1)
replace_exact(
    server,
    'func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {\n\tif r.Method != http.MethodGet {\n\t\tw.WriteHeader(http.StatusMethodNotAllowed)\n\t\treturn\n\t}\n\n\twriteJSON(w, http.StatusOK, SnapshotResponse{Snapshot: s.ledger.Snapshot()})\n}',
    'func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {\n\tif r.Method != http.MethodGet {\n\t\tw.WriteHeader(http.StatusMethodNotAllowed)\n\t\treturn\n\t}\n\tif err := s.validatePeerRequest(r); err != nil {\n\t\twriteJSON(w, statusForError(err), map[string]string{"error": err.Error()})\n\t\treturn\n\t}\n\n\tsnapshot, err := s.signedSnapshot()\n\tif err != nil {\n\t\twriteJSON(w, statusForError(err), map[string]string{"error": err.Error()})\n\t\treturn\n\t}\n\twriteJSON(w, http.StatusOK, SnapshotResponse{Snapshot: snapshot})\n}',
)
replace_exact(
    server,
    'errors.Is(err, tx.ErrInvalidSignature),',
    'errors.Is(err, tx.ErrInvalidSignature),\n\t\terrors.Is(err, tx.ErrNonCanonicalSignature),\n\t\terrors.Is(err, tx.ErrInvalidChainID),\n\t\terrors.Is(err, tx.ErrInvalidDomain),',
)
replace_exact(
    server,
    'errors.Is(err, consensus.ErrInvalidSignature),',
    'errors.Is(err, consensus.ErrInvalidSignature),\n\t\terrors.Is(err, consensus.ErrInvalidChainID),\n\t\terrors.Is(err, consensus.ErrInvalidDomain),\n\t\terrors.Is(err, consensus.ErrInvalidStateRoot),',
)
replace_exact(
    server,
    'errors.Is(err, errTransportIdentityValidatorMismatch):',
    'errors.Is(err, errTransportIdentityValidatorMismatch),\n\t\terrors.Is(err, errTransportIdentityChainMismatch),\n\t\terrors.Is(err, errMissingRequestProof),\n\t\terrors.Is(err, errInvalidRequestProof),\n\t\terrors.Is(err, errRequestChainMismatch),\n\t\terrors.Is(err, errRequestDomainMismatch),\n\t\terrors.Is(err, errRequestTimestamp),\n\t\terrors.Is(err, ledger.ErrInvalidSnapshot),\n\t\terrors.Is(err, ledger.ErrSnapshotChainMismatch),\n\t\terrors.Is(err, ledger.ErrSnapshotProofInvalid),\n\t\terrors.Is(err, ledger.ErrInvalidStateRoot):',
)
replace_exact(
    server,
    'case errors.Is(err, errPeerIdentityRequired),\n\t\terrors.Is(err, errPeerValidatorNotAllowed):',
    'case errors.Is(err, errPeerIdentityRequired),\n\t\terrors.Is(err, errPeerValidatorNotAllowed),\n\t\terrors.Is(err, errRequestReplay),\n\t\terrors.Is(err, errRequestReplayStoreFull),\n\t\terrors.Is(err, ledger.ErrSnapshotQuorumRequired),\n\t\terrors.Is(err, errSnapshotSignerRequired):',
)

api_consensus = "internal/api/consensus_api.go"
replace_exact(
    api_consensus,
    '\tif request.ProposedAt.IsZero() {\n\t\trequest.ProposedAt = time.Now().UTC()\n\t}\n\n\tsourceNode :=',
    '\tif request.ProposedAt.IsZero() {\n\t\trequest.ProposedAt = time.Now().UTC()\n\t}\n\tif err := request.ValidateForChain(s.config.ChainID); err != nil {\n\t\twriteJSON(w, statusForError(err), map[string]string{"error": err.Error()})\n\t\treturn\n\t}\n\n\tsourceNode :=',
)
replace_exact(
    api_consensus,
    '\tif request.VotedAt.IsZero() {\n\t\trequest.VotedAt = time.Now().UTC()\n\t}\n\n\tsourceNode :=',
    '\tif request.VotedAt.IsZero() {\n\t\trequest.VotedAt = time.Now().UTC()\n\t}\n\tif err := request.ValidateForChain(s.config.ChainID); err != nil {\n\t\twriteJSON(w, statusForError(err), map[string]string{"error": err.Error()})\n\t\treturn\n\t}\n\n\tsourceNode :=',
)

automation = "internal/api/consensus_automation.go"
replace_exact(
    automation,
    '\t\tproposal.PreviousHash = previousProposal.PreviousHash\n\t\tproposal.ProducedAt',
    '\t\tproposal.PreviousHash = previousProposal.PreviousHash\n\t\tproposal.StateRoot = previousProposal.StateRoot\n\t\tproposal.ProducedAt',
)
replace_exact(
    automation,
    '\t\tproposal.PreviousHash = block.PreviousHash\n\t\tproposal.ProducedAt',
    '\t\tproposal.PreviousHash = block.PreviousHash\n\t\tproposal.StateRoot = block.StateRoot\n\t\tproposal.ProducedAt',
)

peer_sync = "internal/api/peer_sync.go"
replace_regex(
    peer_sync,
    r'func \(s \*Server\) restoreSnapshotFromPeer\(peerURL string, reason string\) \(peerSnapshotRestoreResult, error\) \{.*?\n\}\n\nfunc \(s \*Server\) broadcastTransaction',
    '''func (s *Server) restoreSnapshotFromPeer(peerURL string, reason string) (peerSnapshotRestoreResult, error) {
\ttrusted := s.ledger.ValidatorSet()
\tif len(trusted.Validators) == 0 {
\t\treturn peerSnapshotRestoreResult{}, ledger.ErrSnapshotQuorumRequired
\t}

\tsnapshot, err := s.fetchPeerSnapshot(peerURL)
\tif err != nil {
\t\treturn peerSnapshotRestoreResult{}, err
\t}
\tif uint64(len(snapshot.Blocks)) < s.ledger.Status().Height {
\t\treturn peerSnapshotRestoreResult{Reason: reason}, nil
\t}
\tif err := ledger.ValidateSnapshotCommittedState(snapshot, s.config.ChainID); err != nil {
\t\treturn peerSnapshotRestoreResult{}, err
\t}

\tproofs := []ledger.SnapshotProof{snapshot.Proof}
\tfor _, candidateURL := range s.config.PeerURLs {
\t\tif candidateURL == peerURL {
\t\t\tcontinue
\t\t}
\t\tother, fetchErr := s.fetchPeerSnapshot(candidateURL)
\t\tif fetchErr != nil || len(other.Blocks) == 0 {
\t\t\tcontinue
\t\t}
\t\totherLatest := other.Blocks[len(other.Blocks)-1]
\t\tlatest := snapshot.Blocks[len(snapshot.Blocks)-1]
\t\tif otherLatest.Height != latest.Height || otherLatest.Hash != latest.Hash || otherLatest.StateRoot != latest.StateRoot || other.ValidatorSnapshot.Version != snapshot.ValidatorSnapshot.Version {
\t\t\tcontinue
\t\t}
\t\tproofs = append(proofs, other.Proof)
\t}

\tnow := time.Now().UTC()
\tif err := s.ledger.RestoreQuorumSnapshot(snapshot, s.config.ChainID, proofs, trusted, now); err != nil {
\t\treturn peerSnapshotRestoreResult{}, err
\t}
\ts.recordSnapshotRestore(peerURL, snapshot, now)
\tresult := peerSnapshotRestoreResult{
\t\tApplied:    true,
\t\tRestoredAt: now,
\t\tHeight:     uint64(len(snapshot.Blocks)),
\t\tReason:     reason,
\t}
\tif len(snapshot.Blocks) > 0 {
\t\tresult.BlockHash = snapshot.Blocks[len(snapshot.Blocks)-1].Hash
\t}
\treturn result, nil
}

func (s *Server) broadcastTransaction''',
)

main = "cmd/node/main.go"
replace_exact(
    main,
    '\tif nodeID := os.Getenv("ZEPHYR_NODE_ID"); nodeID != "" {',
    '\tif chainID := os.Getenv("ZEPHYR_CHAIN_ID"); chainID != "" {\n\t\tconfig.ChainID = chainID\n\t}\n\tif nodeID := os.Getenv("ZEPHYR_NODE_ID"); nodeID != "" {',
)
replace_exact(main, '"zephyr node %s listening on %s', '"zephyr node %s on chain %s listening on %s')
replace_exact(main, '\t\tconfig.NodeID,\n\t\taddr,', '\t\tconfig.NodeID,\n\t\tconfig.ChainID,\n\t\taddr,')
