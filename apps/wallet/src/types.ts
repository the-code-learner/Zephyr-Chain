export type StoredAccount = {
  address: string
  createdAt: string
  publicKeyJwk: JsonWebKey
  privateKeyJwk: JsonWebKey
  publicKeySpki: string
}

export type TransactionDraft = {
  from: string
  to: string
  amount: number
  nonce: number
  memo: string
}

export type SignedTransactionEnvelope = TransactionDraft & {
  payload: string
  publicKey: string
  signature: string
}

export type BroadcastResponse = {
  id: string
  accepted: boolean
  queuedAt: string
  mempoolSize: number
}

export type AccountView = {
  address: string
  balance: number
  availableBalance: number
  nonce: number
  nextNonce: number
  pendingTransactions: number
}

export type AccountResponse = {
  account: AccountView
}

export type FaucetResponse = {
  account: AccountView
}

export type ApiError = {
  error?: string
}
