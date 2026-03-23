import type { SignedTransactionEnvelope, StoredAccount, TransactionDraft } from '../types'

const STORAGE_KEY = 'zephyr.wallet.account'

export async function createAccount(): Promise<StoredAccount> {
  const keyPair = await crypto.subtle.generateKey(
    {
      name: 'ECDSA',
      namedCurve: 'P-256'
    },
    true,
    ['sign', 'verify']
  )

  const [publicKeyJwk, privateKeyJwk, publicKeySpki] = await Promise.all([
    crypto.subtle.exportKey('jwk', keyPair.publicKey),
    crypto.subtle.exportKey('jwk', keyPair.privateKey),
    crypto.subtle.exportKey('spki', keyPair.publicKey)
  ])

  const encodedSpki = bytesToBase64(new Uint8Array(publicKeySpki))

  return {
    address: await deriveAddress(publicKeySpki),
    createdAt: new Date().toISOString(),
    publicKeyJwk,
    privateKeyJwk,
    publicKeySpki: encodedSpki
  }
}

export function loadStoredAccount(): StoredAccount | null {
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) {
    return null
  }

  try {
    return JSON.parse(raw) as StoredAccount
  } catch {
    localStorage.removeItem(STORAGE_KEY)
    return null
  }
}

export function saveAccount(account: StoredAccount): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(account))
}

export function clearAccount(): void {
  localStorage.removeItem(STORAGE_KEY)
}

export function exportAccount(account: StoredAccount): string {
  return JSON.stringify(account, null, 2)
}

export function importAccount(raw: string): StoredAccount {
  const parsed = JSON.parse(raw) as StoredAccount
  if (!parsed.address || !parsed.publicKeyJwk || !parsed.privateKeyJwk || !parsed.publicKeySpki) {
    throw new Error('Invalid wallet backup')
  }

  return parsed
}

export async function signTransaction(
  account: StoredAccount,
  draft: TransactionDraft
): Promise<SignedTransactionEnvelope> {
  const privateKey = await crypto.subtle.importKey(
    'jwk',
    account.privateKeyJwk,
    {
      name: 'ECDSA',
      namedCurve: 'P-256'
    },
    false,
    ['sign']
  )

  const payload = canonicalize({
    from: draft.from,
    to: draft.to,
    amount: draft.amount,
    nonce: draft.nonce,
    memo: draft.memo
  })

  const signature = await crypto.subtle.sign(
    {
      name: 'ECDSA',
      hash: 'SHA-256'
    },
    privateKey,
    new TextEncoder().encode(payload)
  )

  return {
    ...draft,
    payload,
    publicKey: account.publicKeySpki,
    signature: bytesToBase64(new Uint8Array(signature))
  }
}

async function deriveAddress(publicKeySpki: ArrayBuffer): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', publicKeySpki)
  const hex = bytesToHex(new Uint8Array(digest))
  return `zph_${hex.slice(0, 40)}`
}

function canonicalize(value: unknown): string {
  return JSON.stringify(sortObject(value))
}

function sortObject(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(sortObject)
  }

  if (value && typeof value === 'object') {
    return Object.keys(value as Record<string, unknown>)
      .sort()
      .reduce<Record<string, unknown>>((accumulator, key) => {
        accumulator[key] = sortObject((value as Record<string, unknown>)[key])
        return accumulator
      }, {})
  }

  return value
}

function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte)
  })
  return btoa(binary)
}

