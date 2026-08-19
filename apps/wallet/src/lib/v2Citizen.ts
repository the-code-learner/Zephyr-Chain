export interface CitizenValidatorDTO {
  id: string
  publicKey: string
  power: string
}

export interface CitizenObjectBundle {
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
  validators: CitizenValidatorDTO[]
}

export interface VerifiedCitizenObject {
  network: string
  height: bigint
  shardId: number
  objectId: string
  objectPresent: boolean
  objectBytes?: Uint8Array
  stateRoot: string
}

export interface CitizenPowerState {
  batteryPercent: number
  charging: boolean
  wifi: boolean
  lowPower: boolean
  appActive: boolean
}

export interface CitizenMode {
  verifyHeaders: boolean
  relay: boolean
  sampleDA: boolean
  executeRecent: boolean
  serveCache: boolean
}

const P256_ORDER = BigInt('0xffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551')
const P256_HALF_ORDER = P256_ORDER >> 1n
const textEncoder = new TextEncoder()

class BinaryReader {
  private offset = 0

  constructor(private readonly data: Uint8Array) {}

  u8(): number {
    return this.take(1)[0]
  }

  u16(): number {
    const value = this.take(2)
    return (value[0] << 8) | value[1]
  }

  u32(): number {
    const value = this.take(4)
    return ((value[0] * 0x1000000) + (value[1] << 16) + (value[2] << 8) + value[3]) >>> 0
  }

  u64(): bigint {
    const value = this.take(8)
    let out = 0n
    for (const byte of value) out = (out << 8n) | BigInt(byte)
    return out
  }

  fixed(length: number): Uint8Array {
    return this.take(length)
  }

  bytes(max: number): Uint8Array {
    const length = this.u32()
    if (length > max) throw new Error('Citizen proof field exceeds limit')
    return this.take(length)
  }

  done(): void {
    if (this.offset !== this.data.length) throw new Error('Citizen proof has trailing data')
  }

  private take(length: number): Uint8Array {
    if (length < 0 || this.offset + length > this.data.length) throw new Error('Citizen proof is truncated')
    const out = this.data.slice(this.offset, this.offset + length)
    this.offset += length
    return out
  }
}

class BinaryWriter {
  private readonly chunks: Uint8Array[] = []

  u32(value: number): void {
    this.chunks.push(new Uint8Array([(value >>> 24) & 0xff, (value >>> 16) & 0xff, (value >>> 8) & 0xff, value & 0xff]))
  }

  u64(value: bigint): void {
    const out = new Uint8Array(8)
    let remaining = value
    for (let i = 7; i >= 0; i--) {
      out[i] = Number(remaining & 0xffn)
      remaining >>= 8n
    }
    this.chunks.push(out)
  }

  fixed(value: Uint8Array): void {
    this.chunks.push(value)
  }

  bytes(value: Uint8Array): void {
    this.u32(value.length)
    this.fixed(value)
  }

  result(): Uint8Array {
    return concatBytes(...this.chunks)
  }
}

interface ParsedHeader {
  raw: Uint8Array
  network: Uint8Array
  height: bigint
  shardCommitmentRoot: Uint8Array
  certificateHash: Uint8Array
}

interface ParsedCommitment {
  raw: Uint8Array
  shardId: number
  stateRoot: Uint8Array
  receiptRoot: Uint8Array
  dataRoot: Uint8Array
}

interface MerkleProof {
  index: number
  leafCount: number
  siblings: Uint8Array[]
}

interface StateProof {
  exists: boolean
  bitmap: Uint8Array
  siblings: Uint8Array[]
}

interface ParsedVote {
  network: Uint8Array
  height: bigint
  round: bigint
  headerHash: Uint8Array
  voter: Uint8Array
  publicKey: Uint8Array
  signature: Uint8Array
}

interface ParsedCertificate {
  network: Uint8Array
  height: bigint
  round: bigint
  headerHash: Uint8Array
  votes: ParsedVote[]
}

export function selectCitizenMode(power: CitizenPowerState): CitizenMode {
  const mode: CitizenMode = { verifyHeaders: true, relay: false, sampleDA: false, executeRecent: false, serveCache: false }
  if (power.lowPower || power.batteryPercent < 15) return mode
  if (power.appActive) mode.relay = true
  if (power.wifi && power.batteryPercent >= 30) {
    mode.sampleDA = true
    mode.serveCache = power.appActive
  }
  if (power.wifi && power.charging && power.batteryPercent >= 50) {
    mode.executeRecent = true
    mode.serveCache = true
  }
  return mode
}

export async function fetchAndVerifyCitizenObject(baseURL: string, shardId: number, objectId: string): Promise<VerifiedCitizenObject> {
  const endpoint = new URL('/v2/light/object', normalizeBaseURL(baseURL))
  endpoint.searchParams.set('shard', String(shardId))
  endpoint.searchParams.set('id', objectId)
  const response = await fetch(endpoint)
  if (!response.ok) throw new Error(`Citizen proof request failed (${response.status})`)
  return verifyCitizenObjectBundle(await response.json() as CitizenObjectBundle)
}

export async function verifyCitizenObjectBundle(bundle: CitizenObjectBundle): Promise<VerifiedCitizenObject> {
  const header = parseHeader(base64ToBytes(bundle.header))
  const commitment = parseCommitment(base64ToBytes(bundle.commitment))
  const commitmentProof = parseMerkleProof(base64ToBytes(bundle.commitmentProof))
  const certificate = parseCertificate(base64ToBytes(bundle.certificate))

  if (bundle.network.toLowerCase() !== bytesToHex(header.network) || bundle.shardId !== commitment.shardId) {
    throw new Error('Citizen bundle network or shard mismatch')
  }
  await verifyFinality(header, certificate, bundle.validators)

  const commitmentLeaf = await merkleLeaf('shard-commitment', commitment.raw)
  if (!await verifyMerkle(header.shardCommitmentRoot, commitmentLeaf, commitmentProof)) {
    throw new Error('Shard commitment is not included in finalized header')
  }

  const objectId = hexToBytes(bundle.objectId)
  if (objectId.length !== 32) throw new Error('Invalid Citizen object ID')
  const stateProof = parseStateProof(base64ToBytes(bundle.stateProof))
  if (stateProof.exists !== bundle.objectPresent) throw new Error('Object presence does not match state proof')

  let objectBytes: Uint8Array | undefined
  let value: Uint8Array | undefined
  if (bundle.objectPresent) {
    if (!bundle.object) throw new Error('Citizen bundle omitted present object')
    objectBytes = base64ToBytes(bundle.object)
    if (objectBytes.length < 32 || !equalBytes(objectBytes.slice(0, 32), objectId)) throw new Error('Citizen object identity mismatch')
    value = await domainHash('zephyr/object/v2', objectBytes)
  }
  if (!await verifySparseMerkle(commitment.stateRoot, objectId, value, stateProof)) {
    throw new Error('Object Sparse-Merkle proof is invalid')
  }

  return {
    network: bytesToHex(header.network), height: header.height, shardId: commitment.shardId,
    objectId: bytesToHex(objectId), objectPresent: bundle.objectPresent, objectBytes,
    stateRoot: bytesToHex(commitment.stateRoot)
  }
}

async function verifyFinality(header: ParsedHeader, certificate: ParsedCertificate, validators: CitizenValidatorDTO[]): Promise<void> {
  const headerConsensusBytes = header.raw.slice()
  headerConsensusBytes.fill(0, headerConsensusBytes.length - 32)
  const headerHash = await domainHash('zephyr/global-header-consensus/v2', headerConsensusBytes)
  if (!equalBytes(certificate.network, header.network) || certificate.height !== header.height || !equalBytes(certificate.headerHash, headerHash)) {
    throw new Error('Citizen certificate does not target header')
  }

  const validatorMap = new Map<string, { publicKey: Uint8Array, power: bigint }>()
  let totalPower = 0n
  for (const validator of validators) {
    const id = validator.id.toLowerCase()
    if (validatorMap.has(id)) throw new Error('Duplicate validator in Citizen bundle')
    const publicKey = base64ToBytes(validator.publicKey)
    const derived = await domainHash('zephyr/validator-id/v2', publicKey)
    if (bytesToHex(derived) !== id) throw new Error('Validator identity does not match public key')
    const power = BigInt(validator.power)
    if (power <= 0n) throw new Error('Invalid validator voting power')
    totalPower += power
    validatorMap.set(id, { publicKey, power })
  }
  if (totalPower <= 0n) throw new Error('Citizen bundle has no validator power')

  let signedPower = 0n
  const seen = new Set<string>()
  for (const vote of certificate.votes) {
    if (!equalBytes(vote.network, certificate.network) || vote.height !== certificate.height || vote.round !== certificate.round || !equalBytes(vote.headerHash, certificate.headerHash)) {
      throw new Error('Certificate contains vote for another target')
    }
    const voter = bytesToHex(vote.voter)
    if (seen.has(voter)) throw new Error('Certificate contains duplicate validator vote')
    seen.add(voter)
    const validator = validatorMap.get(voter)
    if (!validator || !equalBytes(validator.publicKey, vote.publicKey)) throw new Error('Certificate vote is not from active validator set')
    if (!isCanonicalLowS(vote.signature) || !await verifyVoteSignature(vote)) throw new Error('Certificate contains invalid vote signature')
    signedPower += validator.power
  }
  const quorum = (totalPower * 2n) / 3n + 1n
  if (signedPower < quorum) throw new Error('Certificate is below 2/3+ quorum')

  const certificateHash = await hashCertificate(certificate)
  if (!equalBytes(certificateHash, header.certificateHash)) throw new Error('Header certificate hash mismatch')
}

async function verifyVoteSignature(vote: ParsedVote): Promise<boolean> {
  const body = new BinaryWriter()
  body.fixed(vote.network)
  body.u64(vote.height)
  body.u64(vote.round)
  body.fixed(vote.headerHash)
  body.fixed(vote.voter)
  const signingPayload = domainFrame('zephyr/consensus/vote/v2', body.result())
  try {
    const key = await crypto.subtle.importKey('raw', toArrayBuffer(vote.publicKey), { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify'])
    return crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, key, toArrayBuffer(vote.signature), toArrayBuffer(signingPayload))
  } catch {
    return false
  }
}

async function hashCertificate(certificate: ParsedCertificate): Promise<Uint8Array> {
  const votes = [...certificate.votes].sort((a, b) => compareBytes(a.voter, b.voter))
  const writer = new BinaryWriter()
  writer.fixed(certificate.network)
  writer.u64(certificate.height)
  writer.u64(certificate.round)
  writer.fixed(certificate.headerHash)
  writer.u32(votes.length)
  for (const vote of votes) {
    writer.fixed(vote.voter)
    writer.bytes(vote.publicKey)
    writer.bytes(vote.signature)
  }
  return domainHash('zephyr/quorum-certificate/v2', writer.result())
}

function parseHeader(raw: Uint8Array): ParsedHeader {
  const reader = new BinaryReader(raw)
  if (reader.u16() !== 2) throw new Error('Unsupported Zephyr header version')
  const network = reader.fixed(32)
  const height = reader.u64()
  reader.fixed(32)
  const shardCommitmentRoot = reader.fixed(32)
  reader.fixed(32)
  reader.fixed(32)
  const certificateHash = reader.fixed(32)
  reader.done()
  if (height === 0n) throw new Error('Invalid finalized height')
  return { raw, network, height, shardCommitmentRoot, certificateHash }
}

function parseCommitment(raw: Uint8Array): ParsedCommitment {
  const reader = new BinaryReader(raw)
  const shardId = reader.u32()
  const stateRoot = reader.fixed(32)
  const receiptRoot = reader.fixed(32)
  const dataRoot = reader.fixed(32)
  reader.done()
  return { raw, shardId, stateRoot, receiptRoot, dataRoot }
}

function parseMerkleProof(raw: Uint8Array): MerkleProof {
  const reader = new BinaryReader(raw)
  const index = reader.u32()
  const leafCount = reader.u32()
  const count = reader.u32()
  if (leafCount === 0 || index >= leafCount || count > 32) throw new Error('Invalid Merkle proof')
  const siblings: Uint8Array[] = []
  for (let i = 0; i < count; i++) siblings.push(reader.fixed(32))
  reader.done()
  return { index, leafCount, siblings }
}

function parseStateProof(raw: Uint8Array): StateProof {
  const reader = new BinaryReader(raw)
  const existsRaw = reader.u8()
  if (existsRaw !== 0 && existsRaw !== 1) throw new Error('Invalid state proof existence bit')
  const bitmap = reader.fixed(32)
  const count = reader.u16()
  if (count > 256) throw new Error('Invalid state proof sibling count')
  const siblings: Uint8Array[] = []
  for (let i = 0; i < count; i++) siblings.push(reader.fixed(32))
  reader.done()
  let bits = 0
  for (let i = 0; i < 256; i++) if (bitmapBit(bitmap, i)) bits++
  if (bits !== siblings.length) throw new Error('State proof bitmap does not match siblings')
  return { exists: existsRaw === 1, bitmap, siblings }
}

function parseCertificate(raw: Uint8Array): ParsedCertificate {
  const reader = new BinaryReader(raw)
  const network = reader.fixed(32)
  const height = reader.u64()
  const round = reader.u64()
  const headerHash = reader.fixed(32)
  const count = reader.u32()
  if (height === 0n || count === 0 || count > 4096) throw new Error('Invalid quorum certificate')
  const votes: ParsedVote[] = []
  for (let i = 0; i < count; i++) votes.push(parseVote(reader.bytes(512)))
  reader.done()
  return { network, height, round, headerHash, votes }
}

function parseVote(raw: Uint8Array): ParsedVote {
  const reader = new BinaryReader(raw)
  const network = reader.fixed(32)
  const height = reader.u64()
  const round = reader.u64()
  const headerHash = reader.fixed(32)
  const voter = reader.fixed(32)
  const publicKey = reader.bytes(65)
  const signature = reader.bytes(64)
  reader.done()
  if (height === 0n || publicKey.length !== 65 || signature.length !== 64) throw new Error('Invalid quorum vote')
  return { network, height, round, headerHash, voter, publicKey, signature }
}

async function verifyMerkle(root: Uint8Array, leaf: Uint8Array, proof: MerkleProof): Promise<boolean> {
  let target = 1
  while (target < proof.leafCount) target <<= 1
  let requiredDepth = 0
  for (let n = target; n > 1; n >>= 1) requiredDepth++
  if (proof.siblings.length !== requiredDepth) return false
  let current = leaf
  let position = proof.index
  for (const sibling of proof.siblings) {
    current = position % 2 === 0 ? await merkleBranch(current, sibling) : await merkleBranch(sibling, current)
    position = Math.floor(position / 2)
  }
  return equalBytes(current, root)
}

let defaultsPromise: Promise<Uint8Array[]> | undefined

async function sparseDefaults(): Promise<Uint8Array[]> {
  if (!defaultsPromise) {
    defaultsPromise = (async () => {
      const defaults = new Array<Uint8Array>(257)
      defaults[256] = await domainHash('zephyr/smt/empty-leaf/v2', new Uint8Array())
      for (let depth = 255; depth >= 0; depth--) defaults[depth] = await smtBranch(defaults[depth + 1], defaults[depth + 1])
      return defaults
    })()
  }
  return defaultsPromise
}

async function verifySparseMerkle(root: Uint8Array, key: Uint8Array, value: Uint8Array | undefined, proof: StateProof): Promise<boolean> {
  if (proof.exists !== (value !== undefined) || key.length !== 32) return false
  const defaults = await sparseDefaults()
  let current = proof.exists && value ? await smtLeaf(key, value) : defaults[256]
  let siblingIndex = 0
  for (let i = 0; i < 256; i++) {
    const depth = 256 - i
    let sibling = defaults[depth]
    if (bitmapBit(proof.bitmap, i)) {
      if (siblingIndex >= proof.siblings.length) return false
      sibling = proof.siblings[siblingIndex++]
    }
    const bitIndex = depth - 1
    const byteIndex = Math.floor(bitIndex / 8)
    const shift = 7 - (bitIndex % 8)
    const bit = (key[byteIndex] >> shift) & 1
    current = bit === 0 ? await smtBranch(current, sibling) : await smtBranch(sibling, current)
  }
  return siblingIndex === proof.siblings.length && equalBytes(current, root)
}

async function merkleLeaf(domain: string, payload: Uint8Array): Promise<Uint8Array> {
  return domainHash(`zephyr/merkle/leaf/v2/${domain}`, payload)
}

async function merkleBranch(left: Uint8Array, right: Uint8Array): Promise<Uint8Array> {
  return domainHash('zephyr/merkle/branch/v2', concatBytes(left, right))
}

async function smtLeaf(key: Uint8Array, value: Uint8Array): Promise<Uint8Array> {
  const writer = new BinaryWriter()
  writer.fixed(key)
  writer.bytes(value)
  return domainHash('zephyr/smt/leaf/v2', writer.result())
}

async function smtBranch(left: Uint8Array, right: Uint8Array): Promise<Uint8Array> {
  return domainHash('zephyr/smt/branch/v2', concatBytes(left, right))
}

async function domainHash(domain: string, payload: Uint8Array): Promise<Uint8Array> {
  return sha256(domainFrame(domain, payload))
}

function domainFrame(domain: string, payload: Uint8Array): Uint8Array {
  const writer = new BinaryWriter()
  writer.bytes(textEncoder.encode(domain))
  writer.bytes(payload)
  return writer.result()
}

async function sha256(value: Uint8Array): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', toArrayBuffer(value)))
}

function bitmapBit(bitmap: Uint8Array, index: number): boolean {
  return (bitmap[Math.floor(index / 8)] & (1 << (index % 8))) !== 0
}

function isCanonicalLowS(signature: Uint8Array): boolean {
  if (signature.length !== 64) return false
  const r = bytesToBigInt(signature.slice(0, 32))
  const s = bytesToBigInt(signature.slice(32))
  return r > 0n && s > 0n && r < P256_ORDER && s <= P256_HALF_ORDER
}

function bytesToBigInt(value: Uint8Array): bigint {
  let out = 0n
  for (const byte of value) out = (out << 8n) | BigInt(byte)
  return out
}

function normalizeBaseURL(value: string): string {
  const url = new URL(value, window.location.origin)
  if (!url.pathname.endsWith('/')) url.pathname += '/'
  return url.toString()
}

function base64ToBytes(value: string): Uint8Array {
  const raw = atob(value)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

function hexToBytes(value: string): Uint8Array {
  if (!/^[0-9a-fA-F]*$/.test(value) || value.length % 2 !== 0) throw new Error('Invalid hex')
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
