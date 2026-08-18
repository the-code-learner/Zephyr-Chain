from pathlib import Path

path = Path("scripts/protocol_fixture_refresh.py")
source = path.read_text()
block = '''replace_exact(api_test,
    '\\t\\tNodeID:                  "node-b",\\n\\t\\tBlockInterval:',
    '\\t\\tNodeID:                  "node-b",\\n\\t\\tValidatorPrivateKey:      encodedPrivateKey(t, voter.privateKey),\\n\\t\\tBlockInterval:', 1)
'''
if block not in source:
    raise SystemExit("ambiguous peer configuration patch not found")
path.write_text(source.replace(block, "", 1))
