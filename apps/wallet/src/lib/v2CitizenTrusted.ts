export interface TrustedValidator {
  id: string
  publicKey: string
  power: string
}

export interface TrustedCitizenBundle {
  network: string
  height: number
  shardId: number
  header: string
  certificate: string
  commitment: string
  commitmentProof: string
  objectId: string
  objectPresent: boolean
  object?: string
  stateProof: string
  validators: TrustedValidator[]
}

export interface CitizenTrustAnchor {
  network: string
  validatorRoot: string
}

export interface TrustedCitizenObject {
  network: string
  height: bigint
  shardId: number
  objectId: string
  objectPresent: boolean
  objectBytes?: Uint8Array
  stateRoot: string
  nextTrustAnchor: CitizenTrustAnchor
}

const ORDER = BigInt('0xffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551')
const HALF_ORDER = ORDER >> 1n
const encoder = new TextEncoder()

class Reader {
  private offset = 0
  constructor(private readonly data: Uint8Array) {}
  u8(): number { return this.take(1)[0] }
  u16(): number { const b = this.take(2); return (b[0] << 8) | b[1] }
  u32(): number { const b = this.take(4); return ((b[0] * 0x1000000) + (b[1] << 16) + (b[2] << 8) + b[3]) >>> 0 }
  u64(): bigint { let n = 0n; for (const b of this.take(8)) n = (n << 8n) | BigInt(b); return n }
  fixed(n: number): Uint8Array { return this.take(n) }
  bytes(max: number): Uint8Array { const n = this.u32(); if (n > max) throw new Error('Citizen field exceeds protocol limit'); return this.take(n) }
  done(): void { if (this.offset !== this.data.length) throw new Error('Citizen payload has trailing data') }
  private take(n: number): Uint8Array { if (n < 0 || this.offset + n > this.data.length) throw new Error('Citizen payload is truncated'); const out = this.data.slice(this.offset, this.offset + n); this.offset += n; return out }
}

class Writer {
  private chunks: Uint8Array[] = []
  u32(n: number): void { this.chunks.push(new Uint8Array([(n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255])) }
  u64(n: bigint): void { const out = new Uint8Array(8); for (let i = 7; i >= 0; i--) { out[i] = Number(n & 255n); n >>= 8n }; this.chunks.push(out) }
  fixed(v: Uint8Array): void { this.chunks.push(v) }
  bytes(v: Uint8Array): void { this.u32(v.length); this.fixed(v) }
  result(): Uint8Array { return concat(...this.chunks) }
}

type Header = {
  raw: Uint8Array
  network: Uint8Array
  height: bigint
  shardRoot: Uint8Array
  validatorRoot: Uint8Array
  nextValidatorRoot: Uint8Array
  certificateHash: Uint8Array
}
type Commitment = { raw: Uint8Array, shard: number, stateRoot: Uint8Array }
type MerkleProof = { index: number, leafCount: number, siblings: Uint8Array[] }
type StateProof = { exists: boolean, bitmap: Uint8Array, siblings: Uint8Array[] }
type Vote = { network: Uint8Array, height: bigint, round: bigint, headerHash: Uint8Array, voter: Uint8Array, publicKey: Uint8Array, signature: Uint8Array }
type Certificate = { network: Uint8Array, height: bigint, round: bigint, headerHash: Uint8Array, votes: Vote[] }

export async function fetchTrustedCitizenObject(baseURL: string, shardId: number, objectId: string, anchor: CitizenTrustAnchor): Promise<TrustedCitizenObject> {
  const url = new URL('/v2/light/object', new URL(baseURL, window.location.origin))
  url.searchParams.set('shard', String(shardId))
  url.searchParams.set('id', objectId)
  const response = await fetch(url)
  if (!response.ok) throw new Error(`Citizen proof request failed (${response.status})`)
  return verifyTrustedCitizenBundle(await response.json() as TrustedCitizenBundle, anchor)
}

export async function verifyTrustedCitizenBundle(bundle: TrustedCitizenBundle, anchor: CitizenTrustAnchor): Promise<TrustedCitizenObject> {
  const trustedNetwork = hex32(anchor.network, 'network trust anchor')
  const trustedValidatorRoot = hex32(anchor.validatorRoot, 'validator trust anchor')
  const header = parseHeader(b64(bundle.header))
  if (!eq(header.network, trustedNetwork) || bundle.network.toLowerCase() !== toHex(trustedNetwork)) throw new Error('Citizen bundle is from an untrusted network')
  if (!eq(header.validatorRoot, trustedValidatorRoot)) throw new Error('Citizen header uses an untrusted validator set')

  const calculatedValidatorRoot = await validatorRoot(bundle.validators)
  if (!eq(calculatedValidatorRoot, header.validatorRoot)) throw new Error('Citizen validator set does not match header commitment')
  const certificate = parseCertificate(b64(bundle.certificate))
  await verifyCertificate(header, certificate, bundle.validators)

  const commitment = parseCommitment(b64(bundle.commitment))
  if (commitment.shard !== bundle.shardId) throw new Error('Citizen shard commitment mismatch')
  if (!await verifyMerkle(header.shardRoot, await leaf('shard-commitment', commitment.raw), parseMerkleProof(b64(bundle.commitmentProof)))) throw new Error('Shard commitment is not finalized')

  const id = hex32(bundle.objectId, 'object ID')
  const proof = parseStateProof(b64(bundle.stateProof))
  if (proof.exists !== bundle.objectPresent) throw new Error('Citizen state-proof presence mismatch')
  let objectBytes: Uint8Array | undefined
  let value: Uint8Array | undefined
  if (bundle.objectPresent) {
    if (!bundle.object) throw new Error('Citizen object bytes are missing')
    objectBytes = b64(bundle.object)
    if (objectBytes.length < 32 || !eq(objectBytes.slice(0, 32), id)) throw new Error('Citizen object ID mismatch')
    value = await dh('zephyr/object/v2', objectBytes)
  }
  if (!await verifySMT(commitment.stateRoot, id, value, proof)) throw new Error('Citizen object state proof is invalid')

  const nextRoot = isZero(header.nextValidatorRoot) ? header.validatorRoot : header.nextValidatorRoot
  return {
    network: toHex(header.network), height: header.height, shardId: commitment.shard,
    objectId: toHex(id), objectPresent: bundle.objectPresent, objectBytes,
    stateRoot: toHex(commitment.stateRoot),
    nextTrustAnchor: { network: toHex(header.network), validatorRoot: toHex(nextRoot) }
  }
}

function parseHeader(raw: Uint8Array): Header {
  if (raw.length !== 234) throw new Error('Invalid Zephyr v2 GlobalHeader size')
  const r = new Reader(raw)
  if (r.u16() !== 2) throw new Error('Unsupported Zephyr header version')
  const network = r.fixed(32)
  const height = r.u64()
  r.fixed(32)
  const shardRoot = r.fixed(32)
  const validatorRoot = r.fixed(32)
  const nextValidatorRoot = r.fixed(32)
  r.fixed(32)
  const certificateHash = r.fixed(32)
  r.done()
  if (height === 0n || isZero(network) || isZero(shardRoot) || isZero(validatorRoot)) throw new Error('Invalid Zephyr v2 GlobalHeader')
  return { raw, network, height, shardRoot, validatorRoot, nextValidatorRoot, certificateHash }
}

function parseCommitment(raw: Uint8Array): Commitment {
  const r = new Reader(raw)
  const shard = r.u32()
  const stateRoot = r.fixed(32)
  r.fixed(32)
  r.fixed(32)
  r.done()
  if (isZero(stateRoot)) throw new Error('Invalid shard commitment')
  return { raw, shard, stateRoot }
}

function parseMerkleProof(raw: Uint8Array): MerkleProof {
  const r = new Reader(raw)
  const index = r.u32(), leafCount = r.u32(), count = r.u32()
  if (leafCount === 0 || index >= leafCount || count > 32) throw new Error('Invalid Merkle proof')
  const siblings: Uint8Array[] = []
  for (let i = 0; i < count; i++) siblings.push(r.fixed(32))
  r.done()
  return { index, leafCount, siblings }
}

function parseStateProof(raw: Uint8Array): StateProof {
  const r = new Reader(raw)
  const exists = r.u8()
  if (exists > 1) throw new Error('Invalid state proof')
  const bitmap = r.fixed(32), count = r.u16()
  if (count > 256) throw new Error('Invalid state proof')
  const siblings: Uint8Array[] = []
  for (let i = 0; i < count; i++) siblings.push(r.fixed(32))
  r.done()
  let bits = 0
  for (let i = 0; i < 256; i++) if (bitmapBit(bitmap, i)) bits++
  if (bits !== siblings.length) throw new Error('Invalid state-proof bitmap')
  return { exists: exists === 1, bitmap, siblings }
}

function parseVote(raw: Uint8Array): Vote {
  const r = new Reader(raw)
  const network = r.fixed(32), height = r.u64(), round = r.u64(), headerHash = r.fixed(32), voter = r.fixed(32)
  const publicKey = r.bytes(65), signature = r.bytes(64)
  r.done()
  if (height === 0n || publicKey.length !== 65 || signature.length !== 64) throw new Error('Invalid validator vote')
  return { network, height, round, headerHash, voter, publicKey, signature }
}

function parseCertificate(raw: Uint8Array): Certificate {
  const r = new Reader(raw)
  const network = r.fixed(32), height = r.u64(), round = r.u64(), headerHash = r.fixed(32), count = r.u32()
  if (height === 0n || count === 0 || count > 4096) throw new Error('Invalid quorum certificate')
  const votes: Vote[] = []
  for (let i = 0; i < count; i++) votes.push(parseVote(r.bytes(512)))
  r.done()
  return { network, height, round, headerHash, votes }
}

async function verifyCertificate(header: Header, certificate: Certificate, validators: TrustedValidator[]): Promise<void> {
  const unsigned = header.raw.slice()
  unsigned.fill(0, unsigned.length - 32)
  const headerHash = await dh('zephyr/global-header-consensus/v2', unsigned)
  if (!eq(certificate.network, header.network) || certificate.height !== header.height || !eq(certificate.headerHash, headerHash)) throw new Error('QC does not target finalized header')

  const set = new Map<string, { key: Uint8Array, power: bigint }>()
  let total = 0n
  for (const validator of validators) {
    const id = hex32(validator.id, 'validator ID'), key = b64(validator.publicKey), power = BigInt(validator.power)
    if (key.length !== 65 || power <= 0n || power > 0xffffffffffffffffn || !eq(await dh('zephyr/validator-id/v2', key), id)) throw new Error('Invalid validator identity')
    const idHex = toHex(id)
    if (set.has(idHex)) throw new Error('Duplicate validator')
    set.set(idHex, { key, power }); total += power
  }
  let signed = 0n
  const seen = new Set<string>()
  for (const vote of certificate.votes) {
    if (!eq(vote.network, certificate.network) || vote.height !== certificate.height || vote.round !== certificate.round || !eq(vote.headerHash, certificate.headerHash)) throw new Error('QC contains vote for another target')
    const id = toHex(vote.voter), validator = set.get(id)
    if (!validator || seen.has(id) || !eq(validator.key, vote.publicKey)) throw new Error('QC contains unauthorized or duplicate vote')
    seen.add(id)
    if (!lowS(vote.signature) || !await verifyVote(vote)) throw new Error('QC contains invalid signature')
    signed += validator.power
  }
  if (signed < (total * 2n) / 3n + 1n) throw new Error('QC is below 2/3+ voting power')
  if (!eq(await certificateHash(certificate), header.certificateHash)) throw new Error('QC hash does not match header')
}

async function verifyVote(vote: Vote): Promise<boolean> {
  const w = new Writer(); w.fixed(vote.network); w.u64(vote.height); w.u64(vote.round); w.fixed(vote.headerHash); w.fixed(vote.voter)
  const framed = frame('zephyr/consensus/vote/v2', w.result())
  try {
    const key = await crypto.subtle.importKey('raw', ab(vote.publicKey), { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify'])
    return crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, key, ab(vote.signature), ab(framed))
  } catch { return false }
}

async function certificateHash(c: Certificate): Promise<Uint8Array> {
  const votes = [...c.votes].sort((a, b) => cmp(a.voter, b.voter))
  const w = new Writer(); w.fixed(c.network); w.u64(c.height); w.u64(c.round); w.fixed(c.headerHash); w.u32(votes.length)
  for (const vote of votes) { w.fixed(vote.voter); w.bytes(vote.publicKey); w.bytes(vote.signature) }
  return dh('zephyr/quorum-certificate/v2', w.result())
}

async function validatorRoot(validators: TrustedValidator[]): Promise<Uint8Array> {
  const items: Array<{ id: Uint8Array, key: Uint8Array, power: bigint }> = []
  for (const validator of validators) items.push({ id: hex32(validator.id, 'validator ID'), key: b64(validator.publicKey), power: BigInt(validator.power) })
  items.sort((a, b) => cmp(a.id, b.id))
  const leaves: Uint8Array[] = []
  for (const item of items) {
    const w = new Writer(); w.fixed(item.id); w.bytes(item.key); w.u64(item.power)
    leaves.push(await leaf('validator', w.result()))
  }
  return merkleRoot(leaves)
}

async function verifyMerkle(root: Uint8Array, leafHash: Uint8Array, proof: MerkleProof): Promise<boolean> {
  let target = 1; while (target < proof.leafCount) target <<= 1
  let depth = 0; for (let n = target; n > 1; n >>= 1) depth++
  if (proof.siblings.length !== depth) return false
  let current = leafHash, position = proof.index
  for (const sibling of proof.siblings) { current = position % 2 === 0 ? await branch(current, sibling) : await branch(sibling, current); position = Math.floor(position / 2) }
  return eq(current, root)
}

async function merkleRoot(leaves: Uint8Array[]): Promise<Uint8Array> {
  const empty = await dh('zephyr/merkle/empty/v2', new Uint8Array())
  if (leaves.length === 0) return empty
  let current: Uint8Array[] = leaves.map(x => x.slice())
  let target = 1; while (target < current.length) target <<= 1
  while (current.length < target) current.push(empty.slice())
  while (current.length > 1) { const next: Uint8Array[] = []; for (let i = 0; i < current.length; i += 2) next.push(await branch(current[i], current[i + 1])); current = next }
  return current[0]
}

let smtDefaultsPromise: Promise<Uint8Array[]> | undefined
async function smtDefaults(): Promise<Uint8Array[]> {
  if (!smtDefaultsPromise) smtDefaultsPromise = (async () => { const d: Uint8Array[] = new Array(257); d[256] = await dh('zephyr/smt/empty-leaf/v2', new Uint8Array()); for (let i = 255; i >= 0; i--) d[i] = await smtBranch(d[i + 1], d[i + 1]); return d })()
  return smtDefaultsPromise
}

async function verifySMT(root: Uint8Array, key: Uint8Array, value: Uint8Array | undefined, proof: StateProof): Promise<boolean> {
  if (proof.exists !== (value !== undefined)) return false
  const defaults = await smtDefaults()
  let current = value ? await smtLeaf(key, value) : defaults[256], siblingIndex = 0
  for (let i = 0; i < 256; i++) {
    const depth = 256 - i
    let sibling = defaults[depth]
    if (bitmapBit(proof.bitmap, i)) { if (siblingIndex >= proof.siblings.length) return false; sibling = proof.siblings[siblingIndex++] }
    const bitIndex = depth - 1, bit = (key[Math.floor(bitIndex / 8)] >> (7 - (bitIndex % 8))) & 1
    current = bit === 0 ? await smtBranch(current, sibling) : await smtBranch(sibling, current)
  }
  return siblingIndex === proof.siblings.length && eq(current, root)
}

async function leaf(domain: string, payload: Uint8Array): Promise<Uint8Array> { return dh(`zephyr/merkle/leaf/v2/${domain}`, payload) }
async function branch(a: Uint8Array, b: Uint8Array): Promise<Uint8Array> { return dh('zephyr/merkle/branch/v2', concat(a, b)) }
async function smtBranch(a: Uint8Array, b: Uint8Array): Promise<Uint8Array> { return dh('zephyr/smt/branch/v2', concat(a, b)) }
async function smtLeaf(key: Uint8Array, value: Uint8Array): Promise<Uint8Array> { const w = new Writer(); w.fixed(key); w.bytes(value); return dh('zephyr/smt/leaf/v2', w.result()) }
async function dh(domain: string, payload: Uint8Array): Promise<Uint8Array> { return new Uint8Array(await crypto.subtle.digest('SHA-256', ab(frame(domain, payload)))) }
function frame(domain: string, payload: Uint8Array): Uint8Array { const w = new Writer(); w.bytes(encoder.encode(domain)); w.bytes(payload); return w.result() }
function bitmapBit(bitmap: Uint8Array, index: number): boolean { return (bitmap[Math.floor(index / 8)] & (1 << (index % 8))) !== 0 }
function lowS(sig: Uint8Array): boolean { if (sig.length !== 64) return false; const r = bigint(sig.slice(0, 32)), s = bigint(sig.slice(32)); return r > 0n && s > 0n && r < ORDER && s <= HALF_ORDER }
function bigint(v: Uint8Array): bigint { let n = 0n; for (const b of v) n = (n << 8n) | BigInt(b); return n }
function b64(v: string): Uint8Array { const raw = atob(v), out = new Uint8Array(raw.length); for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i); return out }
function hex32(v: string, label: string): Uint8Array { const s = v.trim().toLowerCase(); if (!/^[0-9a-f]{64}$/.test(s)) throw new Error(`${label} must be 32-byte hex`); const out = new Uint8Array(32); for (let i = 0; i < 32; i++) out[i] = Number.parseInt(s.slice(i * 2, i * 2 + 2), 16); return out }
function toHex(v: Uint8Array): string { return Array.from(v, b => b.toString(16).padStart(2, '0')).join('') }
function isZero(v: Uint8Array): boolean { let x = 0; for (const b of v) x |= b; return x === 0 }
function eq(a: Uint8Array, b: Uint8Array): boolean { if (a.length !== b.length) return false; let x = 0; for (let i = 0; i < a.length; i++) x |= a[i] ^ b[i]; return x === 0 }
function cmp(a: Uint8Array, b: Uint8Array): number { for (let i = 0; i < Math.min(a.length, b.length); i++) if (a[i] !== b[i]) return a[i] - b[i]; return a.length - b.length }
function concat(...values: Uint8Array[]): Uint8Array { const out = new Uint8Array(values.reduce((n, v) => n + v.length, 0)); let at = 0; for (const v of values) { out.set(v, at); at += v.length }; return out }
function ab(v: Uint8Array): ArrayBuffer { return v.buffer.slice(v.byteOffset, v.byteOffset + v.byteLength) as ArrayBuffer }
