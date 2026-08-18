from pathlib import Path


def replace_exact(path, old, new, expected=1):
    p = Path(path)
    s = p.read_text()
    count = s.count(old)
    if count != expected:
        raise SystemExit(f"{path}: expected {expected} occurrences of {old!r}, found {count}")
    p.write_text(s.replace(old, new))


wallet = "apps/wallet/src/lib/wallet.ts"
replace_exact(
    wallet,
    "const MAX_KDF_ITERATIONS = 1_000_000\n",
    "const MAX_KDF_ITERATIONS = 1_000_000\nconst TRANSACTION_DOMAIN = 'zephyr/transaction/v1' as const\nconst P256_ORDER = BigInt('0xffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551')\nconst P256_HALF_ORDER = P256_ORDER >> 1n\n",
)
replace_exact(
    wallet,
    "export async function signTransaction(\n  account: StoredAccount,\n  draft: TransactionDraft\n): Promise<SignedTransactionEnvelope> {\n  await assertAccountIntegrity(account)\n",
    "export async function signTransaction(\n  account: StoredAccount,\n  draft: TransactionDraft,\n  chainId: string\n): Promise<SignedTransactionEnvelope> {\n  await assertAccountIntegrity(account)\n  chainId = chainId.trim()\n  if (!/^[A-Za-z0-9._-]{1,64}$/.test(chainId)) {\n    throw new Error('Connect to a node with a valid Zephyr chain ID before signing')\n  }\n",
)
replace_exact(
    wallet,
    "  const payload = canonicalize({\n    from: draft.from,\n    to: draft.to,\n    amount: draft.amount,\n    nonce: draft.nonce,\n    memo: draft.memo\n  })",
    "  const payload = canonicalize({\n    amount: draft.amount,\n    chainId,\n    domain: TRANSACTION_DOMAIN,\n    from: draft.from,\n    memo: draft.memo,\n    nonce: draft.nonce,\n    to: draft.to\n  })",
)
replace_exact(
    wallet,
    "    signature: bytesToBase64(new Uint8Array(signature))\n  }",
    "    chainId,\n    domain: TRANSACTION_DOMAIN,\n    signature: bytesToBase64(normalizeP256Signature(new Uint8Array(signature)))\n  }",
)
replace_exact(
    wallet,
    "function bytesToHex(bytes: Uint8Array): string {",
    "function normalizeP256Signature(signature: Uint8Array): Uint8Array {\n  if (signature.length !== 64) {\n    throw new Error('Browser returned an invalid P-256 signature')\n  }\n  const r = bytesToBigInt(signature.slice(0, 32))\n  let s = bytesToBigInt(signature.slice(32))\n  if (s > P256_HALF_ORDER) {\n    s = P256_ORDER - s\n  }\n  const normalized = new Uint8Array(64)\n  normalized.set(bigIntTo32Bytes(r), 0)\n  normalized.set(bigIntTo32Bytes(s), 32)\n  return normalized\n}\n\nfunction bytesToBigInt(bytes: Uint8Array): bigint {\n  const hex = bytesToHex(bytes) || '0'\n  return BigInt(`0x${hex}`)\n}\n\nfunction bigIntTo32Bytes(value: bigint): Uint8Array {\n  const hex = value.toString(16).padStart(64, '0')\n  if (hex.length > 64) {\n    throw new Error('P-256 signature integer is out of range')\n  }\n  const bytes = new Uint8Array(32)\n  for (let index = 0; index < 32; index += 1) {\n    bytes[index] = Number.parseInt(hex.slice(index * 2, index * 2 + 2), 16)\n  }\n  return bytes\n}\n\nfunction bytesToHex(bytes: Uint8Array): string {",
)

app = "apps/wallet/src/App.vue"
replace_exact(
    app,
    "import { broadcastTransaction, fetchAccount, fundAccount, pingNode } from './lib/network'",
    "import { broadcastTransaction, fetchAccount, fetchNodeStatus, fundAccount, pingNode } from './lib/network'",
)
replace_exact(
    app,
    "const networkHealthy = ref<boolean | null>(null)\n",
    "const networkHealthy = ref<boolean | null>(null)\nconst chainId = ref('')\n",
)
replace_exact(
    app,
    "async function refreshHealth() {\n  try {\n    networkHealthy.value = await pingNode(apiBase.value)\n  } catch {\n    networkHealthy.value = false\n  }\n}",
    "async function refreshHealth() {\n  try {\n    networkHealthy.value = await pingNode(apiBase.value)\n    if (!networkHealthy.value) {\n      chainId.value = ''\n      return\n    }\n    const status = await fetchNodeStatus(apiBase.value)\n    chainId.value = status.chainId\n  } catch {\n    networkHealthy.value = false\n    chainId.value = ''\n  }\n}",
)
replace_exact(
    app,
    "    const envelope = await signTransaction(account.value, form.value)",
    "    if (!chainId.value) {\n      throw new Error('Node chain identity is unavailable; refresh the node connection before signing')\n    }\n    const envelope = await signTransaction(account.value, form.value, chainId.value)",
)
replace_exact(
    app,
    "        <span class=\"pill\">{{ balancePill }}</span>",
    "        <span class=\"pill\">Chain: {{ chainId || 'unavailable' }}</span>\n        <span class=\"pill\">{{ balancePill }}</span>",
)
