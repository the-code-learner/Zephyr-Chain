from pathlib import Path


def replace_exact(path, old, new, expected=1):
    p = Path(path)
    s = p.read_text()
    count = s.count(old)
    if count != expected:
        raise SystemExit(f"{path}: expected {expected} occurrences of {old!r}, found {count}")
    p.write_text(s.replace(old, new))


Path("internal/ledger/proposal_state.go").write_text(r'''package ledger

import "github.com/zephyr-chain/zephyr-chain/internal/tx"

// ExpectedStateRoot deterministically executes a proposal body against the
// current committed state without mutating the store.
func (s *Store) ExpectedStateRoot(transactions []tx.Envelope) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return expectedStateRootFromState(s.snapshotLocked(), s.chainID, transactions)
}

func expectedStateRootFromState(state persistedState, chainID string, transactions []tx.Envelope) (string, error) {
	state = normalizeState(state)
	accounts := cloneAccounts(state.Accounts)
	for _, envelope := range transactions {
		if err := envelope.ValidateForChain(chainID); err != nil {
			return "", ErrBlockInvariant
		}
		sender := accounts[envelope.From]
		sender.Address = envelope.From
		expectedNonce, ok := nextUint64(sender.Nonce)
		if !ok || sender.Balance < envelope.Amount || expectedNonce != envelope.Nonce {
			return "", ErrBlockInvariant
		}
		sender.Balance -= envelope.Amount
		sender.Nonce = envelope.Nonce
		accounts[envelope.From] = sender

		receiver := accounts[envelope.To]
		receiver.Address = envelope.To
		receiverBalance, ok := addUint64(receiver.Balance, envelope.Amount)
		if !ok {
			return "", ErrBlockInvariant
		}
		receiver.Balance = receiverBalance
		accounts[envelope.To] = receiver
	}
	rootState := state
	rootState.Accounts = accounts
	return stateRootFromState(chainID, rootState)
}
''')

cs = "internal/ledger/consensus_state.go"
replace_exact(
    cs,
    "\tnextState, err := recordProposalIntoState(state, proposal)",
    "\tnextState, err := recordProposalIntoState(state, proposal, s.chainID)",
)
replace_exact(
    cs,
    "func recordProposalIntoState(state persistedState, proposal consensus.Proposal) (persistedState, error) {",
    "func recordProposalIntoState(state persistedState, proposal consensus.Proposal, chainID string) (persistedState, error) {",
)
replace_exact(
    cs,
    "\tif expected := proposerForHeightRound(state.ValidatorSnapshot.Validators, proposal.Height, proposal.Round); expected != \"\" && proposal.Proposer != expected {\n\t\treturn state, ErrUnexpectedProposer\n\t}\n",
    "\tif expected := proposerForHeightRound(state.ValidatorSnapshot.Validators, proposal.Height, proposal.Round); expected != \"\" && proposal.Proposer != expected {\n\t\treturn state, ErrUnexpectedProposer\n\t}\n\texpectedRoot, err := expectedStateRootFromState(state, chainID, proposal.Transactions)\n\tif err != nil || proposal.StateRoot != expectedRoot {\n\t\treturn state, ErrConsensusTemplateMismatch\n\t}\n",
)

# API test helpers: clean-break fields and canonical low-S signing.
api_test = "internal/api/server_test.go"
s = Path(api_test).read_text()
if 'internal/protocol' not in s:
    s = s.replace(
        '"github.com/zephyr-chain/zephyr-chain/internal/ledger"\n\t"github.com/zephyr-chain/zephyr-chain/internal/tx"',
        '"github.com/zephyr-chain/zephyr-chain/internal/ledger"\n\t"github.com/zephyr-chain/zephyr-chain/internal/protocol"\n\t"github.com/zephyr-chain/zephyr-chain/internal/tx"',
    )
s = s.replace(
    '\tenvelope := tx.Envelope{\n\t\tFrom:      address,',
    '\tenvelope := tx.Envelope{\n\t\tChainID:   protocol.DefaultChainID,\n\t\tDomain:    protocol.TransactionDomain,\n\t\tFrom:      address,',
    1,
)
old_sign = '''func signPayload(t *testing.T, privateKey *ecdsa.PrivateKey, payload string) string {
\tt.Helper()

\tdigest := sha256.Sum256([]byte(payload))
\tr, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
\tif err != nil {
\t\tt.Fatalf("sign payload: %v", err)
\t}

\tsignature := append(pad32(r), pad32(s)...)
\treturn base64.StdEncoding.EncodeToString(signature)
}'''
new_sign = '''func signPayload(t *testing.T, privateKey *ecdsa.PrivateKey, payload string) string {
\tt.Helper()
\tsignature, err := tx.SignPayload(privateKey, payload)
\tif err != nil {
\t\tt.Fatalf("sign payload: %v", err)
\t}
\treturn signature
}'''
if old_sign in s:
    s = s.replace(old_sign, new_sign, 1)
s = s.replace(
    '\tproposal := consensus.Proposal{\n\t\tHeight:',
    '\tstateRoot := consensusTestHash("test-state-root")\n\tif len(stateRoots) > 0 {\n\t\tstateRoot = stateRoots[0]\n\t}\n\tproposal := consensus.Proposal{\n\t\tChainID:        protocol.DefaultChainID,\n\t\tDomain:         protocol.ConsensusProposalDomain,\n\t\tStateRoot:      stateRoot,\n\t\tHeight:',
    1,
)
s = s.replace(
    'func signedConsensusProposal(t *testing.T, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope) consensus.Proposal {',
    'func signedConsensusProposal(t *testing.T, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope, stateRoots ...string) consensus.Proposal {',
    1,
)
s = s.replace(
    '\tvote := consensus.Vote{\n\t\tHeight:',
    '\tvote := consensus.Vote{\n\t\tChainID:   protocol.DefaultChainID,\n\t\tDomain:    protocol.ConsensusVoteDomain,\n\t\tHeight:',
    1,
)
s = s.replace(
    '\tidentity := TransportIdentity{\n\t\tNodeID:',
    '\tidentity := TransportIdentity{\n\t\tChainID:          protocol.DefaultChainID,\n\t\tDomain:           protocol.TransportIdentityDomain,\n\t\tNodeID:',
    1,
)
Path(api_test).write_text(s)

# Ledger test helpers: clean-break fields + optional state root.
store_test = "internal/ledger/store_test.go"
s = Path(store_test).read_text()
if 'internal/protocol' not in s:
    s = s.replace(
        '"github.com/zephyr-chain/zephyr-chain/internal/dpos"\n\t"github.com/zephyr-chain/zephyr-chain/internal/tx"',
        '"github.com/zephyr-chain/zephyr-chain/internal/dpos"\n\t"github.com/zephyr-chain/zephyr-chain/internal/protocol"\n\t"github.com/zephyr-chain/zephyr-chain/internal/tx"',
    )
s = s.replace(
    '\tenvelope := tx.Envelope{\n\t\tFrom:      address,',
    '\tenvelope := tx.Envelope{\n\t\tChainID:   protocol.DefaultChainID,\n\t\tDomain:    protocol.TransactionDomain,\n\t\tFrom:      address,',
    1,
)
if old_sign in s:
    s = s.replace(old_sign, new_sign, 1)
s = s.replace(
    'func signedProposalWithSigner(t *testing.T, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope) consensus.Proposal {',
    'func signedProposalWithSigner(t *testing.T, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope, stateRoots ...string) consensus.Proposal {',
    1,
)
s = s.replace(
    '\tproposal := consensus.Proposal{\n\t\tHeight:',
    '\tstateRoot := testHash("test-state-root")\n\tif len(stateRoots) > 0 {\n\t\tstateRoot = stateRoots[0]\n\t}\n\tproposal := consensus.Proposal{\n\t\tChainID:        protocol.DefaultChainID,\n\t\tDomain:         protocol.ConsensusProposalDomain,\n\t\tStateRoot:      stateRoot,\n\t\tHeight:',
    1,
)
s = s.replace(
    '\tvote := consensus.Vote{\n\t\tHeight:',
    '\tvote := consensus.Vote{\n\t\tChainID:   protocol.DefaultChainID,\n\t\tDomain:    protocol.ConsensusVoteDomain,\n\t\tHeight:',
    1,
)
# Template-backed proposals already know the exact committed root.
s = s.replace(
    'signedProposalWithSigner(t, proposer, template.Height, 0, template.PreviousHash, template.ProducedAt, template.Transactions)',
    'signedProposalWithSigner(t, proposer, template.Height, 0, template.PreviousHash, template.ProducedAt, template.Transactions, template.StateRoot)',
)
Path(store_test).write_text(s)

# Consensus timing fixture needs explicit chain/domain/root and canonical signer.
timing_test = "internal/ledger/consensus_timing_test.go"
s = Path(timing_test).read_text()
if 'internal/protocol' not in s:
    s = s.replace(
        '"github.com/zephyr-chain/zephyr-chain/internal/consensus"',
        '"github.com/zephyr-chain/zephyr-chain/internal/consensus"\n\t"github.com/zephyr-chain/zephyr-chain/internal/protocol"',
    )
s = s.replace(
    '\tproposal := consensus.Proposal{\n\t\tHeight:',
    '\tproposal := consensus.Proposal{\n\t\tChainID:      protocol.DefaultChainID,\n\t\tDomain:       protocol.ConsensusProposalDomain,\n\t\tStateRoot:    testHash("timing-state-root"),\n\t\tHeight:',
)
Path(timing_test).write_text(s)
