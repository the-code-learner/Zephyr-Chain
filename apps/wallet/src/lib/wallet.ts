import type {
  EncryptedWalletBackup,
  SignedTransactionEnvelope,
  StoredAccount,
  TransactionDraft
} from '../types'

const STORAGE_KEY = 'zephyr.wallet.encrypted-account.v1'
const LEGACY_STORAGE_KEY = 'zephyr.wallet.account'
const MIN_PASSPHRASE_LENGTH = 10
const KDF_ITERATIONS = 310_000
const MIN_KDF_ITERATIONS = 100_000
const MAX_KDF_ITERATIONS = 1_000_000
const TRANSACTION_DOMAIN = 'zephyr/transaction/v1' as const
const P256_ORDER = BigInt('0xffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551')
const P256_HALF_ORDER = P256_ORDER >> 1n

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

export function hasStoredAccount(): boolean {
  return localStorage.getItem(STORAGE_KEY) !== null || localStorage.getItem(LEGACY_STORAGE_KEY) !== null
}

export function hasLegacyStoredAccount(): boolean {
  return localStorage.getItem(STORAGE_KEY) === null && localStorage.getItem(LEGACY_STORAGE_KEY) !== null
}

export function loadStoredBackup(): string | null {
  return localStorage.getItem(STORAGE_KEY)
}

export async function unlockStoredAccount(passphrase: string): Promise<StoredAccount> {
  requirePassphrase(passphrase)

  const encryptedRaw = localStorage.getItem(STORAGE_KEY)
  if (encryptedRaw) {
    return decryptBackup(parseEncryptedBackup(encryptedRaw), passphrase)
  }

  const legacyRaw = localStorage.getItem(LEGACY_STORAGE_KEY)
  if (!legacyRaw) {
    throw new Error('No stored wallet found')
  }

  const legacyAccount = await parseLegacyAccount(legacyRaw)
  await saveAccount(legacyAccount, passphrase)
  return legacyAccount
}

export async function saveAccount(account: StoredAccount, passphrase: string): Promise<string> {
  const raw = await exportAccount(account, passphrase)
  localStorage.setItem(STORAGE_KEY, raw)
  localStorage.removeItem(LEGACY_STORAGE_KEY)
  return raw
}

export function clearAccount(): void {
  localStorage.removeItem(STORAGE_KEY)
  localStorage.removeItem(LEGACY_STORAGE_KEY)
}

export async function exportAccount(account: StoredAccount, passphrase: string): Promise<string> {
  requirePassphrase(passphrase)
  await assertAccountIntegrity(account)
  const backup = await encryptAccount(account, passphrase)
  return JSON.stringify(backup, null, 2)
}

export async function importAccount(raw: string, passphrase: string): Promise<StoredAccount> {
  requirePassphrase(passphrase)

  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    throw new Error('Wallet backup is not valid JSON')
  }

  if (isEncryptedBackupCandidate(parsed)) {
    return decryptBackup(parseEncryptedBackup(raw), passphrase)
  }

  return parseLegacyAccount(raw)
}

export async function signTransaction(
  account: StoredAccount,
  draft: TransactionDraft,
  chainId: string
): Promise<SignedTransactionEnvelope> {
  await assertAccountIntegrity(account)
  chainId = chainId.trim()
  if (!/^[A-Za-z0-9._-]{1,64}$/.test(chainId)) {
    throw new Error('Connect to a node with a valid Zephyr chain ID before signing')
  }

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
    amount: draft.amount,
    chainId,
    domain: TRANSACTION_DOMAIN,
    from: draft.from,
    memo: draft.memo,
    nonce: draft.nonce,
    to: draft.to
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
    chainId,
    domain: TRANSACTION_DOMAIN,
    signature: bytesToBase64(normalizeP256Signature(new Uint8Array(signature)))
  }
}

async function encryptAccount(account: StoredAccount, passphrase: string): Promise<EncryptedWalletBackup> {
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const key = await deriveEncryptionKey(passphrase, salt)

  const metadata = {
    version: 1 as const,
    address: account.address,
    createdAt: account.createdAt,
    publicKeyJwk: account.publicKeyJwk,
    publicKeySpki: account.publicKeySpki
  }

  const plaintext = new TextEncoder().encode(JSON.stringify(account.privateKeyJwk))
  const ciphertext = await crypto.subtle.encrypt(
    {
      name: 'AES-GCM',
      iv: bytesToArrayBuffer(iv),
      additionalData: new TextEncoder().encode(walletAAD(metadata)),
      tagLength: 128
    },
    key,
    plaintext
  )

  return {
    ...metadata,
    encryption: {
      algorithm: 'AES-GCM',
      kdf: 'PBKDF2',
      hash: 'SHA-256',
      iterations: KDF_ITERATIONS,
      salt: bytesToBase64(salt),
      iv: bytesToBase64(iv),
      ciphertext: bytesToBase64(new Uint8Array(ciphertext))
    }
  }
}

async function decryptBackup(backup: EncryptedWalletBackup, passphrase: string): Promise<StoredAccount> {
  const salt = base64ToBytes(backup.encryption.salt)
  const iv = base64ToBytes(backup.encryption.iv)
  const ciphertext = base64ToBytes(backup.encryption.ciphertext)
  const key = await deriveEncryptionKey(passphrase, salt, backup.encryption.iterations)

  let plaintext: ArrayBuffer
  try {
    plaintext = await crypto.subtle.decrypt(
      {
        name: 'AES-GCM',
        iv: bytesToArrayBuffer(iv),
        additionalData: new TextEncoder().encode(walletAAD(backup)),
        tagLength: 128
      },
      key,
      bytesToArrayBuffer(ciphertext)
    )
  } catch {
    throw new Error('Invalid wallet passphrase or corrupted backup')
  }

  let privateKeyJwk: JsonWebKey
  try {
    privateKeyJwk = JSON.parse(new TextDecoder().decode(plaintext)) as JsonWebKey
  } catch {
    throw new Error('Encrypted wallet payload is corrupted')
  }

  const account: StoredAccount = {
    address: backup.address,
    createdAt: backup.createdAt,
    publicKeyJwk: backup.publicKeyJwk,
    privateKeyJwk,
    publicKeySpki: backup.publicKeySpki
  }

  await assertAccountIntegrity(account)
  return account
}

async function deriveEncryptionKey(
  passphrase: string,
  salt: Uint8Array,
  iterations = KDF_ITERATIONS
): Promise<CryptoKey> {
  const keyMaterial = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(passphrase),
    'PBKDF2',
    false,
    ['deriveKey']
  )

  return crypto.subtle.deriveKey(
    {
      name: 'PBKDF2',
      hash: 'SHA-256',
      salt: bytesToArrayBuffer(salt),
      iterations
    },
    keyMaterial,
    {
      name: 'AES-GCM',
      length: 256
    },
    false,
    ['encrypt', 'decrypt']
  )
}

function parseEncryptedBackup(raw: string): EncryptedWalletBackup {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    throw new Error('Wallet backup is not valid JSON')
  }

  if (!isEncryptedBackupCandidate(parsed)) {
    throw new Error('Invalid encrypted wallet backup')
  }

  const backup = parsed as EncryptedWalletBackup
  if (
    !backup.address ||
    !backup.createdAt ||
    !backup.publicKeyJwk ||
    !backup.publicKeySpki ||
    backup.encryption.algorithm !== 'AES-GCM' ||
    backup.encryption.kdf !== 'PBKDF2' ||
    backup.encryption.hash !== 'SHA-256' ||
    !backup.encryption.salt ||
    !backup.encryption.iv ||
    !backup.encryption.ciphertext
  ) {
    throw new Error('Invalid encrypted wallet backup')
  }

  if (
    !Number.isInteger(backup.encryption.iterations) ||
    backup.encryption.iterations < MIN_KDF_ITERATIONS ||
    backup.encryption.iterations > MAX_KDF_ITERATIONS
  ) {
    throw new Error('Unsupported wallet key-derivation parameters')
  }

  return backup
}

async function parseLegacyAccount(raw: string): Promise<StoredAccount> {
  let parsed: StoredAccount
  try {
    parsed = JSON.parse(raw) as StoredAccount
  } catch {
    throw new Error('Wallet backup is not valid JSON')
  }

  await assertAccountIntegrity(parsed)
  return parsed
}

function isEncryptedBackupCandidate(value: unknown): value is EncryptedWalletBackup {
  if (!value || typeof value !== 'object') {
    return false
  }

  const candidate = value as Partial<EncryptedWalletBackup>
  return candidate.version === 1 && !!candidate.encryption && typeof candidate.encryption === 'object'
}

async function assertAccountIntegrity(account: StoredAccount): Promise<void> {
  if (
    !account ||
    !account.address ||
    !account.createdAt ||
    !account.publicKeyJwk ||
    !account.privateKeyJwk ||
    !account.publicKeySpki
  ) {
    throw new Error('Invalid wallet backup')
  }

  if (
    account.publicKeyJwk.kty !== 'EC' ||
    account.publicKeyJwk.crv !== 'P-256' ||
    account.privateKeyJwk.kty !== 'EC' ||
    account.privateKeyJwk.crv !== 'P-256' ||
    !account.privateKeyJwk.d ||
    account.publicKeyJwk.x !== account.privateKeyJwk.x ||
    account.publicKeyJwk.y !== account.privateKeyJwk.y
  ) {
    throw new Error('Wallet key material is inconsistent')
  }

  const publicKeyBytes = base64ToBytes(account.publicKeySpki)
  const publicKeyBuffer = bytesToArrayBuffer(publicKeyBytes)
  const derivedAddress = await deriveAddress(publicKeyBuffer)
  if (derivedAddress !== account.address) {
    throw new Error('Wallet address does not match its public key')
  }

  try {
    const [spkiPublicKey] = await Promise.all([
      crypto.subtle.importKey(
        'spki',
        publicKeyBuffer,
        { name: 'ECDSA', namedCurve: 'P-256' },
        true,
        ['verify']
      ),
      crypto.subtle.importKey(
        'jwk',
        account.publicKeyJwk,
        { name: 'ECDSA', namedCurve: 'P-256' },
        false,
        ['verify']
      ),
      crypto.subtle.importKey(
        'jwk',
        account.privateKeyJwk,
        { name: 'ECDSA', namedCurve: 'P-256' },
        false,
        ['sign']
      )
    ])

    const spkiJwk = await crypto.subtle.exportKey('jwk', spkiPublicKey)
    if (spkiJwk.x !== account.publicKeyJwk.x || spkiJwk.y !== account.publicKeyJwk.y) {
      throw new Error('Wallet SPKI and JWK public keys do not match')
    }
  } catch (error) {
    if (error instanceof Error && error.message === 'Wallet SPKI and JWK public keys do not match') {
      throw error
    }
    throw new Error('Wallet key material is invalid')
  }
}

function requirePassphrase(passphrase: string): void {
  if (passphrase.length < MIN_PASSPHRASE_LENGTH) {
    throw new Error(`Wallet passphrase must be at least ${MIN_PASSPHRASE_LENGTH} characters`)
  }
}

function walletAAD(value: Pick<EncryptedWalletBackup, 'version' | 'address' | 'createdAt' | 'publicKeySpki'>): string {
  return canonicalize({
    version: value.version,
    address: value.address,
    createdAt: value.createdAt,
    publicKeySpki: value.publicKeySpki
  })
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

function normalizeP256Signature(signature: Uint8Array): Uint8Array {
  if (signature.length !== 64) {
    throw new Error('Browser returned an invalid P-256 signature')
  }
  const r = bytesToBigInt(signature.slice(0, 32))
  let s = bytesToBigInt(signature.slice(32))
  if (s > P256_HALF_ORDER) {
    s = P256_ORDER - s
  }
  const normalized = new Uint8Array(64)
  normalized.set(bigIntTo32Bytes(r), 0)
  normalized.set(bigIntTo32Bytes(s), 32)
  return normalized
}

function bytesToBigInt(bytes: Uint8Array): bigint {
  const hex = bytesToHex(bytes) || '0'
  return BigInt(`0x${hex}`)
}

function bigIntTo32Bytes(value: bigint): Uint8Array {
  const hex = value.toString(16).padStart(64, '0')
  if (hex.length > 64) {
    throw new Error('P-256 signature integer is out of range')
  }
  const bytes = new Uint8Array(32)
  for (let index = 0; index < 32; index += 1) {
    bytes[index] = Number.parseInt(hex.slice(index * 2, index * 2 + 2), 16)
  }
  return bytes
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

function base64ToBytes(value: string): Uint8Array {
  let binary: string
  try {
    binary = atob(value)
  } catch {
    throw new Error('Wallet backup contains invalid base64 data')
  }

  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index)
  }
  return bytes
}

function bytesToArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}
