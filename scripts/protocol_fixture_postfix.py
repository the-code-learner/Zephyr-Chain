from pathlib import Path
import re


def replace_func(path: str, name: str, body: str):
    p = Path(path)
    source = p.read_text()
    pattern = rf"func {re.escape(name)}\(.*?(?=\nfunc |\Z)"
    updated, count = re.subn(pattern, body.rstrip() + "\n", source, count=1, flags=re.S)
    if count != 1:
        raise SystemExit(f"{path}: unable to replace {name}")
    p.write_text(updated)


api = Path("internal/api/server_test.go")
source = api.read_text()
start = source.index("func TestPeerReplicationPropagatesConsensusProposalAndVotes")
end = source.index("\nfunc ", start + 10)
chunk = source[start:end]
# Put consensus validator identities before server construction, once.
chunk = re.sub(r'\n\tproposer := newConsensusSigner\(t\)\n\tvoter := newConsensusSigner\(t\)\n', '\n', chunk)
chunk = chunk.replace(
    'func TestPeerReplicationPropagatesConsensusProposalAndVotes(t *testing.T) {\n',
    'func TestPeerReplicationPropagatesConsensusProposalAndVotes(t *testing.T) {\n\tproposer := newConsensusSigner(t)\n\tvoter := newConsensusSigner(t)\n',
    1,
)
if 'ValidatorPrivateKey:     encodedPrivateKey(t, voter.privateKey)' not in chunk:
    chunk = chunk.replace('\t\tNodeID:                  "node-b",\n', '\t\tNodeID:                  "node-b",\n\t\tValidatorPrivateKey:     encodedPrivateKey(t, voter.privateKey),\n', 1)
if 'ValidatorPrivateKey:     encodedPrivateKey(t, proposer.privateKey)' not in chunk:
    chunk = chunk.replace('\t\tNodeID:                  "node-a",\n', '\t\tNodeID:                  "node-a",\n\t\tValidatorPrivateKey:     encodedPrivateKey(t, proposer.privateKey),\n', 1)
source = source[:start] + chunk + source[end:]
source = source.replace('ObservedAt: now,', 'FirstObservedAt: now, LastObservedAt: now,', 1)
api.write_text(source)

# Direct state setup is acceptable inside the ledger package arithmetic test;
# it does not reopen an unsigned snapshot API.
arith = Path("internal/ledger/store_arithmetic_test.go")
s = arith.read_text()
old = '''\tif err := store.Restore(Snapshot{Accounts: map[string]AccountState{
\t\t"sender": {Address: "sender", Balance: 1, Nonce: math.MaxUint64},
\t}}); err != nil {
\t\tt.Fatalf("restore exhausted account: %v", err)
\t}
'''
new = '''\tstore.mu.Lock()
\tstore.accounts["sender"] = AccountState{Address: "sender", Balance: 1, Nonce: math.MaxUint64}
\tstore.pending = rebuildPendingState(store.accounts, store.mempool)
\tstore.mu.Unlock()
'''
if old not in s:
    raise SystemExit("arithmetic restore fixture not found")
arith.write_text(s.replace(old, new, 1))

ledger = Path("internal/ledger/store_test.go")
replace_func(str(ledger), "TestStoreSnapshotRestoreRehydratesState", r'''func TestStoreSnapshotRestoreRehydratesState(t *testing.T) {
	store := newTestStore(t)
	if err := store.Restore(Snapshot{}); !errors.Is(err, ErrSnapshotQuorumRequired) {
		t.Fatalf("expected unsigned snapshot restore to fail closed, got %v", err)
	}
}''')
s = ledger.read_text()
old = '''\treplica := newTestStore(t)
\tif err := replica.Restore(store.Snapshot()); err != nil {
\t\tt.Fatalf("restore snapshot: %v", err)
\t}
\tif replica.ConsensusArtifacts().LatestCertificate == nil {
\t\tt.Fatal("expected certificate after snapshot restore")
\t}
'''
new = '''\treplica := newTestStore(t)
\tif err := replica.Restore(store.Snapshot()); !errors.Is(err, ErrSnapshotQuorumRequired) {
\t\tt.Fatalf("expected unsigned consensus snapshot restore to fail closed, got %v", err)
\t}
'''
if old not in s:
    raise SystemExit("consensus artifact snapshot fixture not found")
s = s.replace(old, new, 1)
s = s.replace('expected template mismatch error for mismatched producedAt, got %v', 'expected template mismatch for mismatched producedAt, got %v')
# Root/hash validation now fails earlier than consensus template matching for a tampered imported block.
s = s.replace('if err := replica.ImportBlockWithOptions(mismatched, true); !errors.Is(err, ErrConsensusTemplateMismatch) {', 'if err := replica.ImportBlockWithOptions(mismatched, true); !errors.Is(err, ErrInvalidBlock) {', 1)
s = s.replace('expected template mismatch error for mismatched import, got %v', 'expected invalid block error for mismatched import, got %v', 1)
ledger.write_text(s)
