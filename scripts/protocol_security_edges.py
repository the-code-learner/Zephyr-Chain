from pathlib import Path


def replace_exact(path, old, new, expected=1):
    p = Path(path)
    s = p.read_text()
    count = s.count(old)
    if count != expected:
        raise SystemExit(f"{path}: expected {expected} occurrences of {old!r}, found {count}")
    p.write_text(s.replace(old, new))


# Internal endpoints are never public, even if tests or embedders use Handler directly.
peer = "internal/api/peer_admission.go"
replace_exact(
    peer,
    'func (s *Server) validatePeerRequest(r *http.Request) error {\n\tif requestSourceNode(r) == "" {\n\t\treturn nil\n\t}',
    'func (s *Server) validatePeerRequest(r *http.Request) error {\n\tif requestSourceNode(r) == "" {\n\t\tif strings.HasPrefix(r.URL.Path, "/v1/internal/") {\n\t\t\treturn errPeerIdentityRequired\n\t\t}\n\t\treturn nil\n\t}',
)

public = "internal/api/public_handler.go"
replace_exact(
    public,
    'if r.URL.Path == "/v1/internal/snapshot" && requestSourceNode(r) == "" {\n\t\t\twriteJSON(w, http.StatusForbidden, map[string]string{"error": "internal snapshot requires a peer source"})',
    'if strings.HasPrefix(r.URL.Path, "/v1/internal/") && requestSourceNode(r) == "" {\n\t\t\twriteJSON(w, http.StatusForbidden, map[string]string{"error": "internal endpoint requires a signed peer request"})',
)

# Remove the old unsigned snapshot restore bypass. All snapshot replacement must
# flow through RestoreQuorumSnapshot.
store = "internal/ledger/store.go"
start = Path(store).read_text()
old = '''func (s *Store) Restore(snapshot Snapshot) error {
\ts.mu.Lock()
\tdefer s.mu.Unlock()

\tstate := persistedFromSnapshot(snapshot)
\tif err := s.writeState(state); err != nil {
\t\treturn err
\t}

\ts.applyStateLocked(state)
\treturn nil
}'''
new = '''func (s *Store) Restore(snapshot Snapshot) error {
\treturn ErrSnapshotQuorumRequired
}'''
if old not in start:
    raise SystemExit("unsafe Store.Restore implementation not found")
Path(store).write_text(start.replace(old, new, 1))
