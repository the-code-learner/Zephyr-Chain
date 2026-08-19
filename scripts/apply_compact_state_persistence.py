from pathlib import Path

path = Path("internal/ledger/store.go")
source = path.read_text()
old = 'raw, err := json.MarshalIndent(state, "", "  ")'
new = 'raw, err := json.Marshal(state)'
count = source.count(old)
if count != 1:
    raise SystemExit(f"expected one MarshalIndent state persistence call, found {count}")
path.write_text(source.replace(old, new, 1))
