from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    source = p.read_text()
    count = source.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one match, found {count}: {old!r}")
    p.write_text(source.replace(old, new, 1))


replace_once(
    "internal/api/server.go",
    '\ts.mux.HandleFunc("/v1/internal/blocks", s.handleImportBlock)\n\ts.mux.HandleFunc("/v1/internal/snapshot", s.handleSnapshot)\n',
    '\ts.mux.HandleFunc("/v1/internal/blocks", s.handleImportBlock)\n\ts.mux.HandleFunc("/v1/internal/block-evidence/", s.handleBlockEvidence)\n\ts.mux.HandleFunc("/v1/internal/snapshot", s.handleSnapshot)\n',
)

replace_once(
    "internal/api/performance_lab_test.go",
    '''func (t *labFaultTransport) FetchBlock(peerURL string, height uint64) (ledger.Block, error) {
\tif err := t.before(peerURL); err != nil {
\t\treturn ledger.Block{}, err
\t}
\treturn t.base.FetchBlock(peerURL, height)
}

func (t *labFaultTransport) FetchSnapshot(peerURL string) (ledger.Snapshot, error) {''',
    '''func (t *labFaultTransport) FetchBlock(peerURL string, height uint64) (ledger.Block, error) {
\tif err := t.before(peerURL); err != nil {
\t\treturn ledger.Block{}, err
\t}
\treturn t.base.FetchBlock(peerURL, height)
}

func (t *labFaultTransport) FetchBlockEvidence(peerURL string, height uint64) (ledger.CertifiedBlockEvidence, error) {
\tif err := t.before(peerURL); err != nil {
\t\treturn ledger.CertifiedBlockEvidence{}, err
\t}
\treturn t.base.FetchBlockEvidence(peerURL, height)
}

func (t *labFaultTransport) FetchSnapshot(peerURL string) (ledger.Snapshot, error) {''',
)

peer_sync = Path("internal/api/peer_sync.go")
source = peer_sync.read_text()
old = '''\t\tif err := s.ledger.ImportBlockWithOptions(block, s.config.RequireConsensusCertificates); err != nil {
\t\t\tnow := time.Now().UTC()
\t\t\tresult.ImportErrorCode = consensusDiagnosticCode(err)
\t\t\tresult.ImportErrorMessage = err.Error()
\t\t\tresult.ImportFailureAt = cloneAPITimeValue(now)
\t\t\tresult.ImportFailureHeight = block.Height
\t\t\tresult.ImportFailureBlockHash = block.Hash
\t\t\ts.recordBlockImportFailure("peer_sync", block, err, peerURL)
\t\t\trestore, restoreErr := s.restoreSnapshotFromPeer(peerURL, "import_repair")
\t\t\tif restore.Applied {
\t\t\t\tresult.UsedSnapshot = true
\t\t\t\tresult.SnapshotRestoreAt = cloneAPITimeValue(restore.RestoredAt)
\t\t\t\tresult.SnapshotRestoreHeight = restore.Height
\t\t\t\tresult.SnapshotRestoreBlockHash = restore.BlockHash
\t\t\t\tresult.SnapshotRestoreReason = restore.Reason
\t\t\t}
\t\t\tif restoreErr != nil {
\t\t\t\treturn result, restoreErr
\t\t\t}
\t\t\tif !restore.Applied {
\t\t\t\treturn result, fmt.Errorf("peer snapshot from %s is older than local state", peerURL)
\t\t\t}
\t\t\treturn result, nil
\t\t}
'''
new = '''\t\tif err := s.ledger.ImportBlockWithOptions(block, s.config.RequireConsensusCertificates); err != nil {
\t\t\tnow := time.Now().UTC()
\t\t\tresult.ImportErrorCode = consensusDiagnosticCode(err)
\t\t\tresult.ImportErrorMessage = err.Error()
\t\t\tresult.ImportFailureAt = cloneAPITimeValue(now)
\t\t\tresult.ImportFailureHeight = block.Height
\t\t\tresult.ImportFailureBlockHash = block.Hash
\t\t\ts.recordBlockImportFailure("peer_sync", block, err, peerURL)

\t\t\tvar evidenceErr error
\t\t\tif s.config.RequireConsensusCertificates {
\t\t\t\tevidence, fetchEvidenceErr := s.transport.FetchBlockEvidence(peerURL, height)
\t\t\t\tif fetchEvidenceErr != nil {
\t\t\t\t\tevidenceErr = fetchEvidenceErr
\t\t\t\t} else if importEvidenceErr := s.ledger.ImportBlockWithEvidence(block, evidence); importEvidenceErr != nil {
\t\t\t\t\tevidenceErr = importEvidenceErr
\t\t\t\t} else {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t}

\t\t\trestore, restoreErr := s.restoreSnapshotFromPeer(peerURL, "import_repair")
\t\t\tif restore.Applied {
\t\t\t\tresult.UsedSnapshot = true
\t\t\t\tresult.SnapshotRestoreAt = cloneAPITimeValue(restore.RestoredAt)
\t\t\t\tresult.SnapshotRestoreHeight = restore.Height
\t\t\t\tresult.SnapshotRestoreBlockHash = restore.BlockHash
\t\t\t\tresult.SnapshotRestoreReason = restore.Reason
\t\t\t}
\t\t\tif restoreErr != nil {
\t\t\t\tif evidenceErr != nil {
\t\t\t\t\treturn result, fmt.Errorf("certified block recovery failed: %v; snapshot recovery failed: %w", evidenceErr, restoreErr)
\t\t\t\t}
\t\t\t\treturn result, restoreErr
\t\t\t}
\t\t\tif !restore.Applied {
\t\t\t\tif evidenceErr != nil {
\t\t\t\t\treturn result, fmt.Errorf("certified block recovery failed: %v; peer snapshot from %s is older than local state", evidenceErr, peerURL)
\t\t\t\t}
\t\t\t\treturn result, fmt.Errorf("peer snapshot from %s is older than local state", peerURL)
\t\t\t}
\t\t\treturn result, nil
\t\t}
'''
count = source.count(old)
if count != 1:
    raise SystemExit(f"internal/api/peer_sync.go: expected one import recovery block, found {count}")
peer_sync.write_text(source.replace(old, new, 1))
