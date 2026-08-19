import {
  type CitizenObjectBundle,
  type CitizenValidatorDTO,
  type VerifiedCitizenObject,
  verifyCitizenObjectBundle
} from './v2Citizen'

export interface CitizenTrustAnchor {
  // Genesis-derived Zephyr NetworkID in lowercase hex.
  network: string
  // Validator-set Merkle root trusted from genesis or a previously verified checkpoint.
  validatorRoot: string
}

// This is the wallet-facing entry point. It rejects a self-consistent but
// self-signed light bundle unless its validator set is anchored to the trusted
// genesis/checkpoint root before delegating the remaining QC/shard/state proof
// verification to v2Citizen.ts.
export async function fetchAndVerifyTrustedCitizenObject(
  baseURL: string,
  shardId: number,
  objectId: string,
  anchor: CitizenTrustAnchor
): Promise<VerifiedCitizenObject> {
  const endpoint = new URL('/v2/light/object', normalizeBaseURL(baseURL))
  endpoint.searchParams.set('shard', String(shardId))
  endpoint.searchParams.set('id', objectId)
  const response = await fetch(endpoint)
  if (!response.ok) throw new Error(`Citizen proof request failed (${response.status})`)
  return verifyTrustedCitizenObjectBundle(await response.json() as CitizenObjectBundle, anchor)
}

export async function verifyTrustedCitizenObjectBundle(
  bundle: CitizenObjectBundle,
  anchor: CitizenTrustAnchor
): Promise<VerifiedCitizenObject> {
  const expectedNetwork = normalizeHex32(anchor.network, 'Citizen trust-anchor network')
  const expectedValidatorRoot = normalizeHex32(anchor.validatorRoot, 'Citizen trust-anchor validator root')
  const header = base64ToBytes(bundle.header)
  if (header.length !== 202) throw new Error('Invalid canonical Zephyr GlobalHeader length')
  if (readU16(header, 0) !== 2) throw new Error('Unsupported Zephyr GlobalHeader version')

  const headerNetwork = bytesToHex(header.slice(2, 34))
  const headerValidatorRoot = bytesToHex(header.slice(106, 138))
  if (bundle.network.toLowerCase() !== expectedNetwork || headerNetwork !== expectedNetwork) {
    throw new Error('Citizen bundle does not belong to the trusted Zephyr network')
  }
  if (headerValidatorRoot !== expectedValidatorRoot) {
    throw new Error('Citizen header validator root is not trusted by this wallet checkpoint')
  }

  const suppliedValidatorRoot = bytesToHex(await validatorSetRoot(bundle.validators))
  if (suppliedValidatorRoot !== expectedValidatorRoot) {
    throw new Error('Citizen validator set does not match the committed trusted validator root')
  }

  return verifyCitizenObjectBundle(bundle)
}

async function validatorSetRoot(validators: CitizenValidatorDTO[]): Promise<Uint8Array> {
  if (validators.length === 0) throw new Error('Citizen validator set is empty')
  const canonical: Array<{ id: Uint8Array, publicKey: Uint8Array, power: bigint }> = []
  const seen = new Set<string>()
  for (const validator of validators) {
    const id = hexToBytes(normalizeHex32(validator.id, 'validator ID'))
    const idHex = bytesToHex(id)
    if (seen.has(idHex)) throw new Error('Duplicate Citizen validator')
    seen.add(idHex)
    const publicKey = base64ToBytes(validator.publicKey)
    if (publicKey.length !== 65) throw new Error('Invalid Citizen validator public key')
    const derivedID = await domainHash('zephyr/validator-id/v2', publicKey)
    if (!equalBytes(derivedID, id)) throw new Error('Citizen validator ID does not match public key')
    const power = BigInt(validator.power)
    if (power <= 0n || power > 0xffffffffffffffffn) throw new Error('Invalid Citizen validator voting power')
    canonical.push({ id, publicKey, power })
  }
  canonical.sort((a, b) => compareBytes(a.id, b.id))
  const leaves: Uint8Array[] = []
  for (const validator of canonical) {
    const payload = concatBytes(
      validator.id,
      u32(validator.publicKey.length),
      validator.publicKey,
      u64(validator.power)
    )
    leaves.push(await domainHash('zephyr/merkle/leaf/v2/validator', payload))
  }
  return merkleRoot(leaves)
}

async function merkleRoot(leaves: Uint8Array[]): Promise<Uint8Array> {
  const empty = await domainHash('zephyr/merkle/empty/v2', new Uint8Array())
  if (leaves.length === 0) return empty
  const level = leaves.map(leaf => leaf.slice())
  let target = 1
  while (target < level.length) target <<= 1
  while (level.length < target) level.push(empty.slice())
  let current = level
  while (current.length > 1) {
    const next: Uint8Array[] = []
    for (let i = 0; i < current.length; i += 2) {
      next.push(await domainHash('zephyr/merkle/branch/v2', concatBytes(current[i], current[i + 1])))
    }
    current = next
  }
  return current[0]
}

async function domainHash(domain: string, payload: Uint8Array): Promise<Uint8Array> {
  const encodedDomain = new TextEncoder().encode(domain)
  const framed = concatBytes(u32(encodedDomain.length), encodedDomain, u32(payload.length), payload)
  return new Uint8Array(await crypto.subtle.digest('SHA-256', toArrayBuffer(framed)))
}

function normalizeHex32(value: string, label: string): string {
  const normalized = value.trim().toLowerCase()
  if (!/^[0-9a-f]{64}$/.test(normalized)) throw new Error(`${label} must be a 32-byte hex value`)
  return normalized
}

function readU16(value: Uint8Array, offset: number): number {
  return (value[offset] << 8) | value[offset + 1]
}

function u32(value: number): Uint8Array {
  if (!Number.isInteger(value) || value < 0 || value > 0xffffffff) throw new Error('u32 overflow')
  return new Uint8Array([(value >>> 24) & 0xff, (value >>> 16) & 0xff, (value >>> 8) & 0xff, value & 0xff])
}

function u64(value: bigint): Uint8Array {
  if (value < 0n || value > 0xffffffffffffffffn) throw new Error('u64 overflow')
  const out = new Uint8Array(8)
  let remaining = value
  for (let i = 7; i >= 0; i--) {
    out[i] = Number(remaining & 0xffn)
    remaining >>= 8n
  }
  return out
}

function base64ToBytes(value: string): Uint8Array {
  const raw = atob(value)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

function hexToBytes(value: string): Uint8Array {
  const out = new Uint8Array(value.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = Number.parseInt(value.slice(i * 2, i * 2 + 2), 16)
  return out
}

function bytesToHex(value: Uint8Array): string {
  return Array.from(value, byte => byte.toString(16).padStart(2, '0')).join('')
}

function equalBytes(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false
  let diff = 0
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i]
  return diff === 0
}

function compareBytes(a: Uint8Array, b: Uint8Array): number {
  for (let i = 0; i < Math.min(a.length, b.length); i++) {
    if (a[i] !== b[i]) return a[i] - b[i]
  }
  return a.length - b.length
}

function concatBytes(...values: Uint8Array[]): Uint8Array {
  const length = values.reduce((total, value) => total + value.length, 0)
  const out = new Uint8Array(length)
  let offset = 0
  for (const value of values) {
    out.set(value, offset)
    offset += value.length
  }
  return out
}

function toArrayBuffer(value: Uint8Array): ArrayBuffer {
  return value.buffer.slice(value.byteOffset, value.byteOffset + value.byteLength) as ArrayBuffer
}

function normalizeBaseURL(value: string): string {
  const url = new URL(value, window.location.origin)
  if (!url.pathname.endsWith('/')) url.pathname += '/'
  return url.toString()
}
