from pathlib import Path
import re


def replace_func(path: str, name: str, body: str):
    p = Path(path)
    source = p.read_text()
    pattern = rf"func {re.escape(name)}\(.*?(?=\nfunc |\Z)"
    updated, count = re.subn(pattern, body.rstrip() + "\n", source, count=1, flags=re.S)
    if count != 1:
        raise SystemExit(f"{path}: unable to replace function {name}")
    p.write_text(updated)


def replace_test(path: str, name: str, body: str):
    replace_func(path, name, body)


def replace_exact(path: str, old: str, new: str, expected: int = 1):
    p = Path(path)
    source = p.read_text()
    count = source.count(old)
    if count != expected:
        raise SystemExit(f"{path}: expected {expected} occurrences of {old!r}, found {count}")
    p.write_text(source.replace(old, new))


api_test = "internal/api/server_test.go"
p = Path(api_test)
s = p.read_text()
# Undo the semantic script's earlier broad replacement if it landed in the alert test.
s = s.replace(
    '\tstateRoot := consensusTestHash("test-state-root")\n\tif len(stateRoots) > 0 {\n\t\tstateRoot = stateRoots[0]\n\t}\n\tproposal := consensus.Proposal{\n\t\tChainID:        protocol.DefaultChainID,\n\t\tDomain:         protocol.ConsensusProposalDomain,\n\t\tStateRoot:      stateRoot,\n\t\tHeight:    2,',
    '\tproposal := consensus.Proposal{\n\t\tHeight:    2,',
    1,
)
p.write_text(s)

replace_func(api_test, "signedEnvelope", r'''func signedEnvelope(t *testing.T, amount uint64, nonce uint64, memo string) tx.Envelope {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { t.Fatalf("generate key: %v", err) }
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil { t.Fatalf("marshal public key: %v", err) }
	encodedPublicKey := base64.StdEncoding.EncodeToString(publicKeyBytes)
	address, err := tx.DeriveAddressFromPublicKey(encodedPublicKey)
	if err != nil { t.Fatalf("derive address: %v", err) }
	envelope := tx.Envelope{
		ChainID: protocol.DefaultChainID, Domain: protocol.TransactionDomain,
		From: address, To: "zph_receiver", Amount: amount, Nonce: nonce, Memo: memo, PublicKey: encodedPublicKey,
	}
	envelope.Payload = envelope.CanonicalPayload()
	envelope.Signature, err = tx.SignPayload(privateKey, envelope.Payload)
	if err != nil { t.Fatalf("sign transaction: %v", err) }
	return envelope
}''')
replace_func(api_test, "signPayload", r'''func signPayload(t *testing.T, privateKey *ecdsa.PrivateKey, payload string) string {
	t.Helper()
	signature, err := tx.SignPayload(privateKey, payload)
	if err != nil { t.Fatalf("sign payload: %v", err) }
	return signature
}''')
replace_func(api_test, "signedConsensusProposal", r'''func signedConsensusProposal(t *testing.T, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope, stateRoots ...string) consensus.Proposal {
	t.Helper()
	transactionIDs := make([]string, 0, len(transactions))
	for _, envelope := range transactions { transactionIDs = append(transactionIDs, tx.ID(envelope)) }
	stateRoot := consensusTestHash("test-state-root")
	if len(stateRoots) > 0 { stateRoot = stateRoots[0] }
	proposal := consensus.Proposal{
		ChainID: protocol.DefaultChainID, Domain: protocol.ConsensusProposalDomain,
		Height: height, Round: round, PreviousHash: previousHash, StateRoot: stateRoot, ProducedAt: producedAt,
		TransactionIDs: append([]string(nil), transactionIDs...), Transactions: append([]tx.Envelope(nil), transactions...),
		Proposer: signer.address, PublicKey: signer.publicKey,
	}
	proposal.BlockHash = proposal.CandidateHash()
	proposal.Payload = proposal.CanonicalPayload()
	proposal.Signature = signPayload(t, signer.privateKey, proposal.Payload)
	return proposal
}''')
replace_func(api_test, "signedConsensusVote", r'''func signedConsensusVote(t *testing.T, signer consensusSigner, height uint64, round uint64, blockHash string) consensus.Vote {
	t.Helper()
	vote := consensus.Vote{
		ChainID: protocol.DefaultChainID, Domain: protocol.ConsensusVoteDomain,
		Height: height, Round: round, BlockHash: blockHash, Voter: signer.address, PublicKey: signer.publicKey,
	}
	vote.Payload = vote.CanonicalPayload()
	vote.Signature = signPayload(t, signer.privateKey, vote.Payload)
	return vote
}''')
replace_func(api_test, "signedTransportIdentity", r'''func signedTransportIdentity(t *testing.T, signer consensusSigner, nodeID string, signedAt time.Time) TransportIdentity {
	t.Helper()
	identity := TransportIdentity{
		ChainID: protocol.DefaultChainID, Domain: protocol.TransportIdentityDomain,
		NodeID: nodeID, ValidatorAddress: signer.address, PublicKey: signer.publicKey, SignedAt: signedAt.UTC(),
	}
	identity.Payload = identity.CanonicalPayload()
	identity.Signature = signPayload(t, signer.privateKey, identity.Payload)
	return identity
}''')

# A stored proposal used for reproposal must itself be executable and commit the correct root.
replace_exact(api_test,
    '\troundZeroProposal := signedConsensusProposal(t, roundZeroProposer, 1, 0, "", producedAt, []tx.Envelope{signedEnvelope(t, 25, 1, "round-zero-candidate")})',
    '\troundZeroTx := signedEnvelope(t, 25, 1, "round-zero-candidate")\n\tif _, err := server.ledger.Credit(roundZeroTx.From, 100); err != nil { t.Fatalf("credit round-zero sender: %v", err) }\n\troundZeroRoot, err := server.ledger.ExpectedStateRoot([]tx.Envelope{roundZeroTx})\n\tif err != nil { t.Fatalf("compute round-zero state root: %v", err) }\n\troundZeroProposal := signedConsensusProposal(t, roundZeroProposer, 1, 0, "", producedAt, []tx.Envelope{roundZeroTx}, roundZeroRoot)')

# Consensus replication needs authenticated validator peers and identical executable state.
replace_exact(api_test,
    'func TestPeerReplicationPropagatesConsensusProposalAndVotes(t *testing.T) {\n\tpeerServer :=',
    'func TestPeerReplicationPropagatesConsensusProposalAndVotes(t *testing.T) {\n\tproposer := newConsensusSigner(t)\n\tvoter := newConsensusSigner(t)\n\tpeerServer :=')
replace_exact(api_test,
    '\t\tNodeID:                  "node-b",\n\t\tBlockInterval:',
    '\t\tNodeID:                  "node-b",\n\t\tValidatorPrivateKey:      encodedPrivateKey(t, voter.privateKey),\n\t\tBlockInterval:', 1)
# Only within this test, add the main validator key and remove the old late declarations.
source = Path(api_test).read_text()
start = source.index('func TestPeerReplicationPropagatesConsensusProposalAndVotes')
end = source.index('\nfunc ', start + 10)
chunk = source[start:end]
chunk = chunk.replace('\t\tNodeID:                  "node-a",\n\t\tPeerURLs:', '\t\tNodeID:                  "node-a",\n\t\tValidatorPrivateKey:      encodedPrivateKey(t, proposer.privateKey),\n\t\tPeerURLs:', 1)
chunk = chunk.replace('\n\tproposer := newConsensusSigner(t)\n\tvoter := newConsensusSigner(t)\n', '\n', 1)
chunk = chunk.replace(
    '\tproposal := signedConsensusProposal(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 13, 15, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "peer-block-1-tx")})',
    '\tproposalTx := signedEnvelope(t, 5, 1, "peer-block-1-tx")\n\tfor _, target := range []*Server{mainServer, peerServer} {\n\t\tif _, err := target.ledger.Credit(proposalTx.From, 10); err != nil { t.Fatalf("credit proposal sender: %v", err) }\n\t}\n\tproposalRoot, err := mainServer.ledger.ExpectedStateRoot([]tx.Envelope{proposalTx})\n\tif err != nil { t.Fatalf("compute proposal state root: %v", err) }\n\tproposal := signedConsensusProposal(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 13, 15, 0, 0, time.UTC), []tx.Envelope{proposalTx}, proposalRoot)',
    1,
)
source = source[:start] + chunk + source[end:]
Path(api_test).write_text(source)

# Certified replication uses the validator keys for transport and the exact template root.
source = Path(api_test).read_text()
start = source.index('func TestPeerReplicationImportsCertifiedBlockWhenConsensusRequired')
end = source.index('\nfunc ', start + 10)
chunk = source[start:end]
chunk = chunk.replace('\t\tNodeID:                       "node-b",\n', '\t\tNodeID:                       "node-b",\n\t\tValidatorPrivateKey:           encodedPrivateKey(t, voter.privateKey),\n', 1)
chunk = chunk.replace('\t\tValidatorAddress:             proposer.address,\n', '\t\tValidatorAddress:             proposer.address,\n\t\tValidatorPrivateKey:          encodedPrivateKey(t, proposer.privateKey),\n', 1)
chunk = chunk.replace('templateResponse.Block.Transactions)', 'templateResponse.Block.Transactions, templateResponse.Block.StateRoot)', 1)
source = source[:start] + chunk + source[end:]
Path(api_test).write_text(source)

# Non-consensus replication still requires authenticated node identities.
source = Path(api_test).read_text()
start = source.index('func TestPeerReplicationPropagatesFaucetTransactionAndBlock')
end = source.index('\nfunc ', start + 10)
chunk = source[start:end]
chunk = chunk.replace('{\n\tpeerServer :=', '{\n\tmainSigner := newConsensusSigner(t)\n\tpeerSigner := newConsensusSigner(t)\n\tpeerServer :=', 1)
chunk = chunk.replace('\t\tNodeID:                  "node-b",\n', '\t\tNodeID:                  "node-b",\n\t\tValidatorPrivateKey:     encodedPrivateKey(t, peerSigner.privateKey),\n', 1)
chunk = chunk.replace('\t\tNodeID:                  "node-a",\n', '\t\tNodeID:                  "node-a",\n\t\tValidatorPrivateKey:     encodedPrivateKey(t, mainSigner.privateKey),\n', 1)
source = source[:start] + chunk + source[end:]
Path(api_test).write_text(source)

# Clean break: no snapshot restore without a locally trusted validator set.
replace_test(api_test, "TestPeerSyncRestoresSnapshotForLateJoiningNode", r'''func TestPeerSyncRestoresSnapshotForLateJoiningNode(t *testing.T) {
	server := newTestServer(t, Config{DataDir: t.TempDir(), NodeID: "late-join", BlockInterval: 0, SyncInterval: 0, EnablePeerSync: false})
	if _, err := server.restoreSnapshotFromPeer("http://peer.example", "initial_sync"); !errors.Is(err, ledger.ErrSnapshotQuorumRequired) {
		t.Fatalf("expected snapshot bootstrap without trust anchor to be rejected, got %v", err)
	}
}''')
replace_test(api_test, "TestPeerSyncRestoresSnapshotWhenSameHeightDiverges", r'''func TestPeerSyncRestoresSnapshotWhenSameHeightDiverges(t *testing.T) {
	server := newTestServer(t, Config{DataDir: t.TempDir(), NodeID: "diverged", BlockInterval: 0, SyncInterval: 0, EnablePeerSync: false})
	if _, err := server.restoreSnapshotFromPeer("http://peer.example", "peer_diverged"); !errors.Is(err, ledger.ErrSnapshotQuorumRequired) {
		t.Fatalf("expected divergence repair without trust anchor to be rejected, got %v", err)
	}
}''')
# The history test remains about persistence; make the expected security failure explicit.
replace_test(api_test, "TestPeerSyncHistoryPersistsAcrossServerRestart", r'''func TestPeerSyncHistoryPersistsAcrossServerRestart(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, Config{DataDir: dataDir, NodeID: "replica", BlockInterval: 0, SyncInterval: 0, EnablePeerSync: false})
	now := time.Now().UTC()
	if err := server.ledger.RecordPeerSyncIncident(ledger.PeerSyncIncident{
		PeerURL: "http://peer.example", State: "sync_error", Reason: "peer_diverged",
		ErrorCode: "snapshot_quorum_required", ErrorMessage: ledger.ErrSnapshotQuorumRequired.Error(), ObservedAt: now,
	}); err != nil { t.Fatalf("record peer sync incident: %v", err) }
	server.Close()
	reopened, err := NewServerWithConfig(Config{DataDir: dataDir, NodeID: "replica", BlockInterval: 0, SyncInterval: 0, EnablePeerSync: false})
	if err != nil { t.Fatalf("reopen replica: %v", err) }
	defer reopened.Close()
	history := reopened.ledger.PeerSyncHistory()
	if len(history.Recent) != 1 || history.Recent[0].State != "sync_error" || history.Recent[0].ErrorCode != "snapshot_quorum_required" {
		t.Fatalf("unexpected persisted peer sync history %+v", history)
	}
}''')

# Internal endpoints always require a proof, even when invoked without PublicHandler.
Path(api_test).write_text(Path(api_test).read_text() + r'''

func TestInternalBlockEndpointRejectsMissingRequestProof(t *testing.T) {
	server := newTestServer(t, Config{DataDir: t.TempDir(), BlockInterval: 0, SyncInterval: 0, EnablePeerSync: false})
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/blocks", bytes.NewBufferString(`{}`))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden { t.Fatalf("expected internal endpoint to require peer proof, got %d", recorder.Code) }
}
''')

# Ledger helpers and valid proposal roots.
ledger_test = "internal/ledger/store_test.go"
replace_func(ledger_test, "signedEnvelope", r'''func signedEnvelope(t *testing.T, amount uint64, nonce uint64, memo string) tx.Envelope {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { t.Fatalf("generate key: %v", err) }
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil { t.Fatalf("marshal public key: %v", err) }
	encodedPublicKey := base64.StdEncoding.EncodeToString(publicKeyBytes)
	address, err := tx.DeriveAddressFromPublicKey(encodedPublicKey)
	if err != nil { t.Fatalf("derive address: %v", err) }
	envelope := tx.Envelope{ChainID: protocol.DefaultChainID, Domain: protocol.TransactionDomain, From: address, To: "zph_receiver", Amount: amount, Nonce: nonce, Memo: memo, PublicKey: encodedPublicKey}
	envelope.Payload = envelope.CanonicalPayload()
	envelope.Signature, err = tx.SignPayload(privateKey, envelope.Payload)
	if err != nil { t.Fatalf("sign transaction: %v", err) }
	return envelope
}''')
replace_func(ledger_test, "signPayload", r'''func signPayload(t *testing.T, privateKey *ecdsa.PrivateKey, payload string) string {
	t.Helper()
	signature, err := tx.SignPayload(privateKey, payload)
	if err != nil { t.Fatalf("sign payload: %v", err) }
	return signature
}''')
replace_func(ledger_test, "signedProposalWithSigner", r'''func signedProposalWithSigner(t *testing.T, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope, stateRoots ...string) consensus.Proposal {
	t.Helper()
	transactionIDs := make([]string, 0, len(transactions))
	for _, envelope := range transactions { transactionIDs = append(transactionIDs, tx.ID(envelope)) }
	stateRoot := testHash("test-state-root")
	if len(stateRoots) > 0 { stateRoot = stateRoots[0] }
	proposal := consensus.Proposal{ChainID: protocol.DefaultChainID, Domain: protocol.ConsensusProposalDomain, Height: height, Round: round, PreviousHash: previousHash, StateRoot: stateRoot, ProducedAt: producedAt, TransactionIDs: append([]string(nil), transactionIDs...), Transactions: append([]tx.Envelope(nil), transactions...), Proposer: signer.address, PublicKey: signer.publicKey}
	proposal.BlockHash = proposal.CandidateHash()
	proposal.Payload = proposal.CanonicalPayload()
	proposal.Signature = signPayload(t, signer.privateKey, proposal.Payload)
	return proposal
}''')
replace_func(ledger_test, "signedVoteWithSigner", r'''func signedVoteWithSigner(t *testing.T, signer consensusSigner, height uint64, round uint64, blockHash string) consensus.Vote {
	t.Helper()
	vote := consensus.Vote{ChainID: protocol.DefaultChainID, Domain: protocol.ConsensusVoteDomain, Height: height, Round: round, BlockHash: blockHash, Voter: signer.address, PublicKey: signer.publicKey}
	vote.Payload = vote.CanonicalPayload()
	vote.Signature = signPayload(t, signer.privateKey, vote.Payload)
	return vote
}''')
# Append store-aware test helper.
source = Path(ledger_test).read_text()
anchor = 'func signedVoteWithSigner('
idx = source.index(anchor)
next_idx = source.index('\nfunc ', idx + len(anchor))
helper = r'''
func signedProposalForStore(t *testing.T, store *Store, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope) consensus.Proposal {
	t.Helper()
	root, err := store.ExpectedStateRoot(transactions)
	if err != nil { t.Fatalf("compute proposal state root: %v", err) }
	return signedProposalWithSigner(t, signer, height, round, previousHash, producedAt, transactions, root)
}

func signedFundedProposalForStore(t *testing.T, store *Store, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope) consensus.Proposal {
	t.Helper()
	for _, envelope := range transactions {
		view := store.View(envelope.From)
		if view.Balance < envelope.Amount {
			if _, err := store.Credit(envelope.From, envelope.Amount-view.Balance); err != nil { t.Fatalf("fund proposal sender: %v", err) }
		}
	}
	return signedProposalForStore(t, store, signer, height, round, previousHash, producedAt, transactions)
}
'''
source = source[:next_idx] + '\n' + helper + source[next_idx:]
Path(ledger_test).write_text(source)

# Valid proposals that are not template-backed must use an executable state root.
s = Path(ledger_test).read_text()
s = s.replace('proposal := signedProposalWithSigner(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 9, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "block-1-tx")})', 'proposal := signedFundedProposalForStore(t, store, proposer, 1, 0, "", time.Date(2026, time.March, 23, 9, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "block-1-tx")})', 1)
s = s.replace('roundOneProposal := signedProposalWithSigner(t, second, 1, 1, "", producedAt, []tx.Envelope{signedEnvelope(t, 5, 1, "round-one-proposal")})', 'roundOneProposal := signedFundedProposalForStore(t, store, second, 1, 1, "", producedAt, []tx.Envelope{signedEnvelope(t, 5, 1, "round-one-proposal")})', 1)
s = s.replace('proposal := signedProposalWithSigner(t, proposer, 1, 0, "", producedAt, []tx.Envelope{envelope})', 'proposal := signedProposalForStore(t, store, proposer, 1, 0, "", producedAt, []tx.Envelope{envelope})', 1)
s = s.replace('proposal := signedProposalWithSigner(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 9, 30, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "pending-artifacts-tx")})', 'proposal := signedFundedProposalForStore(t, store, proposer, 1, 0, "", time.Date(2026, time.March, 23, 9, 30, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "pending-artifacts-tx")})', 1)
s = s.replace('proposal := signedProposalWithSigner(t, validator, 1, 0, "", producedAt, []tx.Envelope{envelope})', 'proposal := signedProposalForStore(t, store, validator, 1, 0, "", producedAt, []tx.Envelope{envelope})', 1)
s = s.replace('!errors.Is(err, ErrConsensusTemplateMismatch)', '!errors.Is(err, ErrInvalidBlock)', 1)
Path(ledger_test).write_text(s)

# Timing test is a valid proposal, so fund it and derive the root.
timing = "internal/ledger/consensus_timing_test.go"
s = Path(timing).read_text()
s = s.replace(
    '\ttransaction := signedEnvelope(t, 1, 1, "timing")\n\tproposal := signedProposalWithSigner(t, second, 1, 1, "", time.Now().UTC(), []tx.Envelope{transaction})',
    '\ttransaction := signedEnvelope(t, 1, 1, "timing")\n\tproposal := signedFundedProposalForStore(t, store, second, 1, 1, "", time.Now().UTC(), []tx.Envelope{transaction})',
    1,
)
Path(timing).write_text(s)

# Unsafe direct Restore/peer restore APIs must fail closed; add explicit quorum tests.
replace_test(ledger_test, "TestStoreRestoreFromPeerSnapshotPreservesLocalRecoveryAndDiagnostics", r'''func TestStoreRestoreFromPeerSnapshotPreservesLocalRecoveryAndDiagnostics(t *testing.T) {
	store := newTestStore(t)
	if err := store.RestoreFromPeerSnapshot(Snapshot{}, time.Now().UTC()); !errors.Is(err, ErrSnapshotQuorumRequired) {
		t.Fatalf("expected unproven peer snapshot restore to fail closed, got %v", err)
	}
}''')

Path("internal/ledger/snapshot_security_test.go").write_text(r'''package ledger

import (
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestRestoreQuorumSnapshotRequiresValidatorQuorumAndRejectsTampering(t *testing.T) {
	first := newConsensusSigner(t)
	second := newConsensusSigner(t)
	validators := []dpos.Validator{
		{Rank: 1, Address: first.address, VotingPower: 60, SelfStake: 60},
		{Rank: 2, Address: second.address, VotingPower: 40, SelfStake: 40},
	}
	producer := newTestStore(t)
	if _, err := producer.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil { t.Fatal(err) }
	envelope := signedEnvelope(t, 25, 1, "quorum-snapshot")
	if _, err := producer.Credit(envelope.From, 100); err != nil { t.Fatal(err) }
	if _, err := producer.Accept(envelope); err != nil { t.Fatal(err) }
	if _, err := producer.ProduceBlock(10); err != nil { t.Fatal(err) }
	snapshot := producer.Snapshot()

	makeProof := func(signer consensusSigner) SnapshotProof {
		proof, err := BuildSnapshotProofTemplate(snapshot, protocol.DefaultChainID, signer.address)
		if err != nil { t.Fatal(err) }
		proof.PublicKey = signer.publicKey
		proof.Payload = proof.CanonicalPayload()
		proof.Signature, err = tx.SignPayload(signer.privateKey, proof.Payload)
		if err != nil { t.Fatal(err) }
		return proof
	}
	firstProof := makeProof(first)
	secondProof := makeProof(second)

	replica := newTestStore(t)
	if _, err := replica.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil { t.Fatal(err) }
	trusted := replica.ValidatorSet()
	if err := replica.RestoreQuorumSnapshot(snapshot, protocol.DefaultChainID, []SnapshotProof{firstProof}, trusted, time.Now().UTC()); err != ErrSnapshotQuorumRequired {
		t.Fatalf("expected 60%% proof to miss a 2/3 quorum, got %v", err)
	}
	if err := replica.RestoreQuorumSnapshot(snapshot, protocol.DefaultChainID, []SnapshotProof{firstProof, secondProof}, trusted, time.Now().UTC()); err != nil {
		t.Fatalf("expected quorum snapshot restore, got %v", err)
	}
	if got := replica.View(envelope.From); got.Balance != 75 || got.Nonce != 1 { t.Fatalf("unexpected restored account %+v", got) }

	tampered := snapshot
	tampered.Accounts = cloneAccounts(snapshot.Accounts)
	account := tampered.Accounts[envelope.From]
	account.Balance++
	tampered.Accounts[envelope.From] = account
	if err := ValidateSnapshotQuorum(tampered, protocol.DefaultChainID, []SnapshotProof{firstProof, secondProof}, trusted); err == nil {
		t.Fatal("expected tampered account state to invalidate snapshot commitment")
	}
}
''')

# Request proof adversarial tests: replay, cross-path reuse, and restart persistence.
Path("internal/api/request_auth_test.go").write_text(r'''package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
)

func signedPeerRequest(t *testing.T, server *Server, method, path string, body []byte) *http.Request {
	t.Helper()
	proof, err := server.identitySigner.buildRequestProof(method, path, body, time.Now().UTC())
	if err != nil { t.Fatal(err) }
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set(sourceNodeHeader, proof.NodeID)
	r.Header.Set(sourceValidatorHeader, proof.ValidatorAddress)
	r.Header.Set(sourceIdentityPayloadHeader, proof.Payload)
	r.Header.Set(sourcePublicKeyHeader, proof.PublicKey)
	r.Header.Set(sourceSignatureHeader, proof.Signature)
	r.Header.Set(sourceSignedAtHeader, proof.SignedAt.Format(time.RFC3339Nano))
	r.Header.Set(sourceChainIDHeader, proof.ChainID)
	r.Header.Set(sourceRequestDomainHeader, proof.Domain)
	r.Header.Set(sourceRequestNonceHeader, proof.Nonce)
	return r
}

func TestRequestProofRejectsReplayCrossPathAndSurvivesRestart(t *testing.T) {
	dataDir := t.TempDir()
	signer := newConsensusSigner(t)
	config := Config{DataDir: dataDir, NodeID: "validator-a", ValidatorPrivateKey: encodedPrivateKey(t, signer.privateKey), BlockInterval: 0, SyncInterval: 0, EnablePeerSync: false}
	server := newTestServer(t, config)
	if _, err := server.ledger.SetValidators([]dpos.Validator{{Rank: 1, Address: signer.address, VotingPower: 100, SelfStake: 100}}, dpos.ElectionConfig{MaxValidators: 1}); err != nil { t.Fatal(err) }

	body := []byte(`{"height":1}`)
	request := signedPeerRequest(t, server, http.MethodPost, "/v1/internal/blocks", body)
	if _, err := validateAndRememberRequestProof(server, request); err != nil { t.Fatalf("first proof should pass: %v", err) }
	request.Body = http.NoBody
	request = signedPeerRequest(t, server, http.MethodPost, "/v1/internal/blocks", body)
	proof, _, err := requestProofFromRequest(request, server.config.ChainID)
	if err != nil { t.Fatal(err) }
	if proof == nil { t.Fatal("expected proof") }
	guard, err := replayGuardForServer(server)
	if err != nil { t.Fatal(err) }
	if err := guard.remember(proof); err != nil { t.Fatalf("fresh nonce should pass: %v", err) }
	if err := guard.remember(proof); !errors.Is(err, errRequestReplay) { t.Fatalf("expected replay rejection, got %v", err) }

	crossPath := httptest.NewRequest(http.MethodPost, "/v1/internal/snapshot", bytes.NewReader(body))
	for key, values := range request.Header { for _, value := range values { crossPath.Header.Add(key, value) } }
	if _, _, err := requestProofFromRequest(crossPath, server.config.ChainID); !errors.Is(err, errInvalidRequestProof) { t.Fatalf("expected cross-path proof rejection, got %v", err) }

	server.Close()
	reopened, err := NewServerWithConfig(config)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	if _, err := reopened.ledger.SetValidators([]dpos.Validator{{Rank: 1, Address: signer.address, VotingPower: 100, SelfStake: 100}}, dpos.ElectionConfig{MaxValidators: 1}); err != nil { t.Fatal(err) }
	if _, err := validateAndRememberRequestProof(reopened, request); !errors.Is(err, errRequestReplay) { t.Fatalf("expected replay rejection after restart, got %v", err) }
}
''')
