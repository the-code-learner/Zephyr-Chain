from pathlib import Path
import re

api = Path("internal/api/server_test.go")
source = api.read_text()
start = source.index("func TestPeerReplicationPropagatesConsensusProposalAndVotes")
end = source.index("\nfunc ", start + 10)
chunk = source[start:end]
lines = chunk.splitlines()
seen = set()
out = []
for line in lines:
    if "ValidatorPrivateKey:" in line and "encodedPrivateKey" in line:
        key = line.split("encodedPrivateKey", 1)[1]
        if key in seen:
            continue
        seen.add(key)
    out.append(line)
source = source[:start] + "\n".join(out) + source[end:]
api.write_text(source)

ledger = Path("internal/ledger/store_test.go")
source = ledger.read_text()

# Mismatched ProducedAt is a consensus-template mismatch, not an invalid-block error.
start = source.index("func TestStoreProduceBlockWithConsensusRequiresProposalAndCertificate")
end = source.index("\nfunc ", start + 10)
chunk = source[start:end]
chunk = re.sub(
    r'if _, err := store\.ProduceBlockWithOptions\([^\n]*mismatched[^\n]*\); !errors\.Is\(err, [A-Za-z0-9_]+\) \{',
    lambda m: m.group(0).rsplit('errors.Is(err, ', 1)[0] + 'errors.Is(err, ErrConsensusTemplateMismatch) {',
    chunk,
    count=1,
)
source = source[:start] + chunk + source[end:]

# A block whose committed hash/root has been tampered is rejected as an invalid block before consensus matching.
start = source.index("func TestStoreImportBlockWithConsensusRequiresProposalAndCertificate")
end = source.index("\nfunc ", start + 10)
chunk = source[start:end]
chunk = re.sub(
    r'if err := replica\.ImportBlockWithOptions\(mismatched, true\); !errors\.Is\(err, [A-Za-z0-9_]+\) \{',
    'if err := replica.ImportBlockWithOptions(mismatched, true); !errors.Is(err, ErrInvalidBlock) {',
    chunk,
    count=1,
)
source = source[:start] + chunk + source[end:]
ledger.write_text(source)
