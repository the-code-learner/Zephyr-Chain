from pathlib import Path


def replace_exact(path: str, old: str, new: str, expected: int = 1):
    p = Path(path)
    source = p.read_text()
    count = source.count(old)
    if count != expected:
        raise SystemExit(f"{path}: expected {expected} occurrences of {old!r}, found {count}")
    p.write_text(source.replace(old, new))


store = "internal/ledger/store.go"
replace_exact(
    store,
    '\tErrVotingPowerOverflow   = errors.New("validator voting power overflow")\n',
    '\tErrVotingPowerOverflow   = errors.New("validator voting power overflow")\n\tErrStateChainMismatch    = errors.New("persisted state chain ID does not match configured chain")\n',
)
replace_exact(
    store,
    'type Snapshot struct {\n\tAccounts',
    'type Snapshot struct {\n\tChainID                 string                  `json:"chainId"`\n\tAccounts',
)
replace_exact(
    store,
    'type persistedState struct {\n\tAccounts',
    'type persistedState struct {\n\tChainID                 string                  `json:"chainId"`\n\tAccounts',
)
replace_exact(
    store,
    '\tif errors.Is(err, os.ErrNotExist) {\n\t\treturn nil\n\t}',
    '\tif errors.Is(err, os.ErrNotExist) {\n\t\treturn s.writeState(s.snapshotLocked())\n\t}',
)
replace_exact(
    store,
    '\tif len(raw) == 0 {\n\t\treturn nil\n\t}',
    '\tif len(raw) == 0 {\n\t\treturn s.writeState(s.snapshotLocked())\n\t}',
)
replace_exact(
    store,
    '\tvar state persistedState\n\tif err := json.Unmarshal(raw, &state); err != nil {\n\t\treturn err\n\t}\n\n\tstate = normalizeState(state)',
    '\tvar state persistedState\n\tif err := json.Unmarshal(raw, &state); err != nil {\n\t\treturn err\n\t}\n\tif state.ChainID == "" || state.ChainID != s.chainID {\n\t\treturn ErrStateChainMismatch\n\t}\n\tfor _, block := range state.Blocks {\n\t\tif block.ChainID != s.chainID {\n\t\t\treturn ErrStateChainMismatch\n\t\t}\n\t}\n\n\tstate = normalizeState(state)',
)
replace_exact(
    store,
    'func (s *Store) writeState(state persistedState) error {\n\tstate = normalizeState(state)',
    'func (s *Store) writeState(state persistedState) error {\n\tif state.ChainID == "" {\n\t\tstate.ChainID = s.chainID\n\t}\n\tif state.ChainID != s.chainID {\n\t\treturn ErrStateChainMismatch\n\t}\n\tstate = normalizeState(state)',
)
replace_exact(
    store,
    '\treturn persistedState{\n\t\tAccounts:',
    '\treturn persistedState{\n\t\tChainID:                 s.chainID,\n\t\tAccounts:',
)
replace_exact(
    store,
    '\treturn Snapshot{\n\t\tAccounts:',
    '\treturn Snapshot{\n\t\tChainID:                 state.ChainID,\n\t\tAccounts:',
)
replace_exact(
    store,
    '\treturn normalizeState(persistedState{\n\t\tAccounts:',
    '\treturn normalizeState(persistedState{\n\t\tChainID:                 snapshot.ChainID,\n\t\tAccounts:',
)

commitment = "internal/ledger/state_commitment.go"
replace_exact(
    commitment,
    'type committedStatePayload struct {\n\tAccounts',
    'type committedStatePayload struct {\n\tAccounts            []committedAccount   `json:"accounts"`\n\tAppliedFundingIDs   []string             `json:"appliedFundingIds"`\n',
)
# Remove the duplicate Accounts line left by the structural replacement.
replace_exact(
    commitment,
    '\tAppliedFundingIDs   []string             `json:"appliedFundingIds"`\n            []committedAccount   `json:"accounts"`\n',
    '\tAppliedFundingIDs   []string             `json:"appliedFundingIds"`\n',
)
replace_exact(
    commitment,
    'func StateRoot(chainID string, accounts map[string]AccountState, snapshot ValidatorSnapshot) (string, error) {',
    'func StateRoot(chainID string, accounts map[string]AccountState, snapshot ValidatorSnapshot) (string, error) {\n\treturn stateRootWithFundingIDs(chainID, accounts, snapshot, nil)\n}\n\nfunc stateRootWithFundingIDs(chainID string, accounts map[string]AccountState, snapshot ValidatorSnapshot, appliedFundingIDs []string) (string, error) {',
)
replace_exact(
    commitment,
    '\tpayload, err := json.Marshal(committedStatePayload{\n\t\tAccounts:            committedAccounts,',
    '\tpayload, err := json.Marshal(committedStatePayload{\n\t\tAccounts:            committedAccounts,\n\t\tAppliedFundingIDs:   uniqueSortedStrings(appliedFundingIDs),',
)
replace_exact(
    commitment,
    '\treturn StateRoot(chainID, state.Accounts, state.ValidatorSnapshot)',
    '\treturn stateRootWithFundingIDs(chainID, state.Accounts, state.ValidatorSnapshot, state.AppliedFundingIDs)',
)

snapshot = "internal/ledger/snapshot_security.go"
replace_exact(
    snapshot,
    '\tif len(snapshot.Blocks) == 0 {',
    '\tif snapshot.ChainID != chainID {\n\t\treturn ErrSnapshotChainMismatch\n\t}\n\tif len(snapshot.Blocks) == 0 {',
)
replace_exact(
    snapshot,
    '\troot, err := StateRoot(chainID, snapshot.Accounts, snapshot.ValidatorSnapshot)',
    '\troot, err := stateRootWithFundingIDs(chainID, snapshot.Accounts, snapshot.ValidatorSnapshot, snapshot.AppliedFundingIDs)',
)
replace_exact(
    snapshot,
    '\tincoming.Mempool = make([]MempoolEntry, 0)\n\tincoming.AppliedFundingIDs = make([]string, 0)\n',
    '\tincoming.Mempool = make([]MempoolEntry, 0)\n',
)

request_auth = "internal/api/request_auth.go"
replace_exact(
    request_auth,
    '\terrRequestReplayStoreFull = errors.New("peer request replay store is full")\n',
    '\terrRequestReplayStoreFull = errors.New("peer request replay store is full")\n\terrRequestBodyTooLarge    = errors.New("peer request body exceeds the allowed limit")\n',
)
replace_exact(
    request_auth,
    '\tbody, err := readAndRestoreRequestBody(r)\n\tif err != nil {\n\t\treturn nil, nil, errInvalidRequestProof\n\t}',
    '\tbody, err := readAndRestoreRequestBody(r)\n\tif err != nil {\n\t\treturn nil, nil, err\n\t}',
)
replace_exact(
    request_auth,
    '\tbody, err := io.ReadAll(r.Body)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tr.Body = io.NopCloser(bytes.NewReader(body))\n\treturn body, nil',
    '\tbody, err := io.ReadAll(io.LimitReader(r.Body, maxPublicRequestBodyBytes+1))\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tif int64(len(body)) > maxPublicRequestBodyBytes {\n\t\treturn nil, errRequestBodyTooLarge\n\t}\n\tr.Body = io.NopCloser(bytes.NewReader(body))\n\treturn body, nil',
)

server = "internal/api/server.go"
replace_exact(
    server,
    '\tcase errors.Is(err, ledger.ErrBlockInvariant),\n\t\terrors.Is(err, ledger.ErrInvalidBlock):\n\t\treturn http.StatusBadRequest',
    '\tcase errors.Is(err, errRequestBodyTooLarge):\n\t\treturn http.StatusRequestEntityTooLarge\n\tcase errors.Is(err, ledger.ErrBlockInvariant),\n\t\terrors.Is(err, ledger.ErrInvalidBlock),\n\t\terrors.Is(err, ledger.ErrStateChainMismatch):\n\t\treturn http.StatusBadRequest',
)
