import type {
  AccountResponse,
  AccountView,
  ApiError,
  BroadcastResponse,
  FaucetResponse,
  SignedTransactionEnvelope
} from '../types'

export async function pingNode(apiBase: string): Promise<boolean> {
  const response = await fetch(url(apiBase, '/health'))
  return response.ok
}

export async function fetchAccount(apiBase: string, address: string): Promise<AccountView> {
  const response = await fetch(url(apiBase, `/v1/accounts/${encodeURIComponent(address)}`))
  const payload = await readJSON<AccountResponse>(response)
  return payload.account
}

export async function fundAccount(apiBase: string, address: string, amount: number): Promise<AccountView> {
  const response = await fetch(url(apiBase, '/v1/dev/faucet'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ address, amount })
  })

  const payload = await readJSON<FaucetResponse>(response)
  return payload.account
}

export async function broadcastTransaction(
  apiBase: string,
  envelope: SignedTransactionEnvelope
): Promise<BroadcastResponse> {
  const response = await fetch(url(apiBase, '/v1/transactions'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(envelope)
  })

  return readJSON<BroadcastResponse>(response)
}

async function readJSON<T>(response: Response): Promise<T> {
  const raw = await response.text()
  const payload = raw ? (JSON.parse(raw) as T | ApiError) : null

  if (!response.ok) {
    const message = payload && typeof payload === 'object' && 'error' in payload ? payload.error : undefined
    throw new Error(message || `Request failed with status ${response.status}`)
  }

  return payload as T
}

function url(apiBase: string, path: string): string {
  return `${apiBase.replace(/\/+$/, '')}${path}`
}
