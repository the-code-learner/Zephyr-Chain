from pathlib import Path
import re


def scoped(path: str, func_name: str, transform):
    p = Path(path)
    source = p.read_text()
    marker = f"func {func_name}("
    start = source.index(marker)
    end = source.find("\nfunc ", start + len(marker))
    if end < 0:
        end = len(source)
    chunk = source[start:end]
    chunk = transform(chunk)
    p.write_text(source[:start] + chunk + source[end:])


# Authentication absence is an authorization failure; malformed proofs remain 400.
server = Path("internal/api/server.go")
source = server.read_text()
source = source.replace("\n\t\terrors.Is(err, errMissingRequestProof),", "", 1)
source = source.replace(
    "case errors.Is(err, errPeerIdentityRequired),\n\t\terrors.Is(err, errPeerValidatorNotAllowed),",
    "case errors.Is(err, errPeerIdentityRequired),\n\t\terrors.Is(err, errMissingRequestProof),\n\t\terrors.Is(err, errPeerValidatorNotAllowed),",
    1,
)
server.write_text(source)

# Force the two precise ledger sentinel assertions after all earlier fixture transforms.
def fix_produce(chunk: str) -> str:
    return re.sub(
        r'(if _, err := store\.ProduceBlockWithOptions\(10, producedAt\.Add\(time\.Second\), true\); !errors\.Is\(err, )[A-Za-z0-9_]+(\) \{)',
        r'\1ErrConsensusTemplateMismatch\2', chunk, count=1,
    )
scoped("internal/ledger/store_test.go", "TestStoreProduceBlockWithConsensusRequiresProposalAndCertificate", fix_produce)

def fix_import(chunk: str) -> str:
    return re.sub(
        r'(if err := replica\.ImportBlockWithOptions\(mismatched, true\); !errors\.Is\(err, )[A-Za-z0-9_]+(\) \{)',
        r'\1ErrInvalidBlock\2', chunk, count=1,
    )
scoped("internal/ledger/store_test.go", "TestStoreImportBlockWithConsensusRequiresProposalAndCertificate", fix_import)
