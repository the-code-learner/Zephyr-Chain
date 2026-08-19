from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    source = p.read_text()
    count = source.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one match, found {count}: {old!r}")
    p.write_text(source.replace(old, new, 1))


peer_sync = "internal/api/peer_sync.go"
replace_once(
    peer_sync,
    '''\t\t\tvar evidenceErr error
\t\t\tif s.config.RequireConsensusCertificates {
\t\t\t\tevidenceTransport, ok := s.transport.(certifiedBlockEvidenceTransport)
\t\t\t\tif !ok {
\t\t\t\t\tevidenceErr = fmt.Errorf("peer transport does not support certified block evidence")
\t\t\t\t} else {
\t\t\t\t\tevidence, fetchEvidenceErr := evidenceTransport.FetchBlockEvidence(peerURL, height)
\t\t\t\t\tif fetchEvidenceErr != nil {
\t\t\t\t\t\tevidenceErr = fetchEvidenceErr
\t\t\t\t\t} else if importEvidenceErr := s.ledger.ImportBlockWithEvidence(block, evidence); importEvidenceErr != nil {
\t\t\t\t\t\tevidenceErr = importEvidenceErr
\t\t\t\t\t} else {
\t\t\t\t\t\tcontinue
\t\t\t\t\t}
\t\t\t\t}
\t\t\t}
''',
    '''\t\t\tvar evidenceErr error
\t\t\tif s.config.RequireConsensusCertificates {
\t\t\t\tif recoveryErr := s.recoverCertifiedBlockFromPeers(peerURL, height, block); recoveryErr != nil {
\t\t\t\t\tevidenceErr = recoveryErr
\t\t\t\t} else {
\t\t\t\t\tcontinue
\t\t\t\t}
\t\t\t}
''',
)

lab = "internal/api/performance_lab_test.go"
replace_once(
    lab,
    '''func (t *labFaultTransport) FetchBlockEvidence(peerURL string, height uint64) (ledger.CertifiedBlockEvidence, error) {
\tif err := t.before(peerURL); err != nil {
\t\treturn ledger.CertifiedBlockEvidence{}, err
\t}
\tevidenceTransport, ok := t.base.(certifiedBlockEvidenceTransport)
\tif !ok {
\t\treturn ledger.CertifiedBlockEvidence{}, fmt.Errorf("lab base transport does not support certified block evidence")
\t}
\treturn evidenceTransport.FetchBlockEvidence(peerURL, height)
}
''',
    '''func (t *labFaultTransport) FetchBlockEvidence(peerURL string, height uint64) ([]ledger.CertifiedBlockEvidence, error) {
\tif err := t.before(peerURL); err != nil {
\t\treturn nil, err
\t}
\tevidenceTransport, ok := t.base.(certifiedBlockEvidenceTransport)
\tif !ok {
\t\treturn nil, fmt.Errorf("lab base transport does not support certified block evidence")
\t}
\treturn evidenceTransport.FetchBlockEvidence(peerURL, height)
}
''',
)

certified_test = "internal/ledger/certified_import_test.go"
source = Path(certified_test).read_text()
needle = '''\tif height := insufficient.Status().Height; height != 0 {
\t\tt.Fatalf("insufficient evidence mutated target height to %d", height)
\t}

\ttampered := newTarget()
'''
if needle in source:
    source = source.replace(
        needle,
        '''\tif height := insufficient.Status().Height; height != 0 {
\t\tt.Fatalf("insufficient evidence mutated target height to %d", height)
\t}

\tlocalPlusRemote := newTarget()
\tif err := localPlusRemote.RecordProposal(proposal); err != nil {
\t\tt.Fatalf("record local recovery proposal: %v", err)
\t}
\tif _, _, err := localPlusRemote.RecordVote(evidence.Votes[0]); err != nil {
\t\tt.Fatalf("record local recovery vote: %v", err)
\t}
\tfourRemoteVotes := evidence
\tfourRemoteVotes.Votes = append([]consensus.Vote(nil), evidence.Votes[1:5]...)
\tif err := localPlusRemote.ImportBlockWithEvidence(block, fourRemoteVotes); err != nil {
\t\tt.Fatalf("expected local vote plus four remote votes to reach quorum: %v", err)
\t}
\tif height := localPlusRemote.Status().Height; height != 1 {
\t\tt.Fatalf("expected local plus remote evidence to import height 1, got %d", height)
\t}

\ttampered := newTarget()
''',
        1,
    )
elif "localPlusRemote := newTarget()" not in source:
    raise SystemExit("internal/ledger/certified_import_test.go: expected insertion point not found")
Path(certified_test).write_text(source)
