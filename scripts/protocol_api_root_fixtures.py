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


def rewrite_test_calls(path: str, test_name: str, server_var: str):
    p = Path(path)
    source = p.read_text()
    marker = f"func {test_name}("
    start = source.index(marker)
    end = source.find("\nfunc ", start + len(marker))
    if end < 0:
        end = len(source)
    chunk = source[start:end]
    chunk = chunk.replace("signedConsensusProposal(t,", f"signedConsensusProposalForServer(t, {server_var},")
    source = source[:start] + chunk + source[end:]
    p.write_text(source)


api = "internal/api/server_test.go"
source = Path(api).read_text()
helper = r'''
func signedConsensusProposalForServer(t *testing.T, server *Server, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope, stateRoots ...string) consensus.Proposal {
	t.Helper()
	if len(stateRoots) > 0 {
		return signedConsensusProposal(t, signer, height, round, previousHash, producedAt, transactions, stateRoots...)
	}
	required := make(map[string]uint64)
	for _, envelope := range transactions {
		next := required[envelope.From] + envelope.Amount
		if next < required[envelope.From] { t.Fatal("test fixture balance overflow") }
		required[envelope.From] = next
	}
	for address, amount := range required {
		view := server.ledger.View(address)
		if view.Balance < amount {
			if _, err := server.ledger.Credit(address, amount-view.Balance); err != nil { t.Fatalf("fund proposal sender: %v", err) }
		}
	}
	root, err := server.ledger.ExpectedStateRoot(transactions)
	if err != nil { t.Fatalf("compute proposal state root: %v", err) }
	return signedConsensusProposal(t, signer, height, round, previousHash, producedAt, transactions, root)
}
'''
if "func signedConsensusProposalForServer(" not in source:
    source += "\n" + helper
    Path(api).write_text(source)

single_server_tests = [
    "TestHandleConsensusProposalAndVotesExposeArtifacts",
    "TestHandleConsensusExposesRoundEvidence",
    "TestHandleConsensusExposesReproposalAndTimeoutWarnings",
    "TestHandleConsensusExposesRoundHistoryAcrossRounds",
    "TestHandleStatusExposesConsensusRecovery",
    "TestHandleBlockTemplateExposesBlockReadinessAcrossCertificateLifecycle",
    "TestHandleStatusRecordsConsensusDiagnosticForTemplateMismatch",
    "TestHandleBlockTemplateAndConsensusGatedProduceBlock",
    "TestHandleConsensusGatedProduceBlockUsesProposalBodyWithoutMempool",
]
for test_name in single_server_tests:
    rewrite_test_calls(api, test_name, "server")
for test_name in [
    "TestHandleImportBlockRejectsAndExposesPendingImportRecovery",
    "TestPeerSyncConsensusImportFailureRestoresSnapshotAndRecordsRecoveryHistory",
]:
    rewrite_test_calls(api, test_name, "producer")

# Auth policy fixtures use the new request-bound proof, not the discovery identity.
source = Path(api).read_text()
auth_helper = r'''
func signedPeerRequestWithSigner(t *testing.T, signer consensusSigner, method string, path string, body []byte) *http.Request {
	t.Helper()
	requestSigner := &transportIdentitySigner{
		chainID: protocol.DefaultChainID, nodeID: "peer-node", validatorAddress: signer.address,
		publicKey: signer.publicKey, privateKey: signer.privateKey,
	}
	proof, err := requestSigner.buildRequestProof(method, path, body, time.Now().UTC())
	if err != nil { t.Fatalf("build peer request proof: %v", err) }
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set(sourceNodeHeader, proof.NodeID)
	request.Header.Set(sourceValidatorHeader, proof.ValidatorAddress)
	request.Header.Set(sourceIdentityPayloadHeader, proof.Payload)
	request.Header.Set(sourcePublicKeyHeader, proof.PublicKey)
	request.Header.Set(sourceSignatureHeader, proof.Signature)
	request.Header.Set(sourceSignedAtHeader, proof.SignedAt.UTC().Format(time.RFC3339Nano))
	request.Header.Set(sourceChainIDHeader, proof.ChainID)
	request.Header.Set(sourceRequestDomainHeader, proof.Domain)
	request.Header.Set(sourceRequestNonceHeader, proof.Nonce)
	return request
}
'''
if "func signedPeerRequestWithSigner(" not in source:
    source += "\n" + auth_helper
    Path(api).write_text(source)

replace_func(api, "TestHandleBroadcastTransactionRejectsInvalidTransportIdentity", r'''func TestHandleBroadcastTransactionRejectsInvalidTransportIdentity(t *testing.T) {
	server := newTestServer(t, Config{DataDir: t.TempDir(), BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10, EnableBlockProduction: true, EnablePeerSync: false})
	envelope := signedEnvelope(t, 25, 1, "peer-identity")
	body, err := json.Marshal(envelope)
	if err != nil { t.Fatal(err) }
	signer := newConsensusSigner(t)
	request := signedPeerRequestWithSigner(t, signer, http.MethodPost, "/v1/transactions", body)
	request.Header.Set(sourceSignatureHeader, base64.StdEncoding.EncodeToString(make([]byte, 64)))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest { t.Fatalf("expected malformed proof status 400, got %d", recorder.Code) }
}''')

replace_func(api, "TestHandleBroadcastTransactionRejectsUnboundPeerValidator", r'''func TestHandleBroadcastTransactionRejectsUnboundPeerValidator(t *testing.T) {
	allowedSigner := newConsensusSigner(t)
	peerSigner := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir: t.TempDir(), BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		EnableBlockProduction: true, EnablePeerSync: false, RequirePeerIdentity: true,
		PeerValidatorBindings: map[string]string{"http://peer.example": allowedSigner.address},
	})
	envelope := signedEnvelope(t, 25, 1, "peer-unbound-validator")
	body, err := json.Marshal(envelope)
	if err != nil { t.Fatal(err) }
	request := signedPeerRequestWithSigner(t, peerSigner, http.MethodPost, "/v1/transactions", body)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden { t.Fatalf("expected unbound validator status 403, got %d", recorder.Code) }
}''')

# Public internal endpoint test now asserts the clean-break rule: a source name alone is insufficient.
public_test = "internal/api/public_handler_test.go"
source = Path(public_test).read_text()
source = source.replace(
    'if recorder.Code != http.StatusOK {\n\t\tt.Fatalf("expected internal snapshot status 200 for peer source when strict identity is disabled, got %d", recorder.Code)\n\t}',
    'if recorder.Code != http.StatusForbidden {\n\t\tt.Fatalf("expected source-only internal snapshot request to be forbidden without a signed proof, got %d", recorder.Code)\n\t}',
    1,
)
Path(public_test).write_text(source)
