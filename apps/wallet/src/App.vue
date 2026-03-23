<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { broadcastTransaction, fetchAccount, fundAccount, pingNode } from './lib/network'
import {
  clearAccount,
  createAccount,
  exportAccount,
  importAccount,
  loadStoredAccount,
  saveAccount,
  signTransaction
} from './lib/wallet'
import type {
  AccountView,
  BroadcastResponse,
  SignedTransactionEnvelope,
  StoredAccount,
  TransactionDraft
} from './types'

const apiBase = ref(import.meta.env.VITE_ZEPHYR_API_BASE ?? 'http://localhost:8080')
const account = ref<StoredAccount | null>(null)
const accountView = ref<AccountView | null>(null)
const backupDraft = ref('')
const signedEnvelope = ref<SignedTransactionEnvelope | null>(null)
const networkResponse = ref<BroadcastResponse | null>(null)
const networkHealthy = ref<boolean | null>(null)
const statusMessage = ref('Create or import an account to begin.')
const faucetAmount = ref(100)
const isBusy = ref(false)
const isRefreshing = ref(false)

const form = ref<TransactionDraft>({
  from: '',
  to: '',
  amount: 1,
  nonce: 1,
  memo: 'Genesis wallet test transfer'
})

const shortAddress = computed(() => {
  if (!account.value) {
    return 'No wallet loaded'
  }

  return `${account.value.address.slice(0, 12)}...${account.value.address.slice(-6)}`
})

const explorerPayload = computed(() => {
  if (!signedEnvelope.value) {
    return 'No signed payload yet.'
  }

  return JSON.stringify(signedEnvelope.value, null, 2)
})

const suggestedNonce = computed(() => accountView.value?.nextNonce ?? 1)

const balancePill = computed(() => {
  if (!accountView.value) {
    return 'Node balance: unavailable'
  }

  return `Node balance: ${accountView.value.availableBalance} available / ${accountView.value.balance} total`
})

onMounted(async () => {
  const existing = loadStoredAccount()
  if (existing) {
    account.value = existing
    form.value.from = existing.address
    backupDraft.value = exportAccount(existing)
    statusMessage.value = 'Recovered wallet from local storage.'
  }

  await refreshNodeState(false)
})

async function refreshNodeState(updateStatus = true) {
  isRefreshing.value = true

  try {
    await refreshHealth()
    await refreshAccount()

    if (!updateStatus) {
      return
    }

    if (networkHealthy.value === false) {
      statusMessage.value = 'Node health check failed. Confirm the API base URL and local node process.'
      return
    }

    if (!account.value) {
      statusMessage.value = 'Node is online. Create or import a wallet to inspect account state.'
      return
    }

    if (accountView.value) {
      statusMessage.value = 'Node is online and account state was refreshed.'
      return
    }

    statusMessage.value = 'Node is online, but this account has no funded state on the current node yet.'
  } finally {
    isRefreshing.value = false
  }
}

async function refreshHealth() {
  try {
    networkHealthy.value = await pingNode(apiBase.value)
  } catch {
    networkHealthy.value = false
  }
}

async function refreshAccount() {
  if (!account.value) {
    accountView.value = null
    return
  }

  try {
    const nextView = await fetchAccount(apiBase.value, account.value.address)
    accountView.value = nextView
    form.value.from = account.value.address

    if (form.value.nonce < nextView.nextNonce) {
      form.value.nonce = nextView.nextNonce
    }
  } catch {
    accountView.value = null
  }
}

async function handleCreateWallet() {
  isBusy.value = true

  try {
    const nextAccount = await createAccount()
    saveAccount(nextAccount)
    account.value = nextAccount
    form.value.from = nextAccount.address
    form.value.nonce = 1
    backupDraft.value = exportAccount(nextAccount)
    signedEnvelope.value = null
    networkResponse.value = null
    await refreshNodeState(false)
    statusMessage.value = 'Fresh wallet created and stored locally on this device.'
  } finally {
    isBusy.value = false
  }
}

async function handleImportWallet() {
  isBusy.value = true

  try {
    const nextAccount = importAccount(backupDraft.value)
    saveAccount(nextAccount)
    account.value = nextAccount
    form.value.from = nextAccount.address
    signedEnvelope.value = null
    networkResponse.value = null
    await refreshNodeState(false)
    statusMessage.value = 'Wallet backup imported successfully.'
  } catch (error) {
    statusMessage.value = error instanceof Error ? error.message : 'Failed to import wallet backup.'
  } finally {
    isBusy.value = false
  }
}

function handleClearWallet() {
  clearAccount()
  account.value = null
  accountView.value = null
  signedEnvelope.value = null
  networkResponse.value = null
  form.value.from = ''
  form.value.nonce = 1
  backupDraft.value = ''
  statusMessage.value = 'Wallet removed from local storage.'
}

async function handleFundAccount() {
  if (!account.value) {
    statusMessage.value = 'Create or import a wallet before funding it.'
    return
  }

  isBusy.value = true

  try {
    accountView.value = await fundAccount(apiBase.value, account.value.address, faucetAmount.value)
    form.value.from = account.value.address

    if (form.value.nonce < accountView.value.nextNonce) {
      form.value.nonce = accountView.value.nextNonce
    }

    statusMessage.value = `Funded ${account.value.address} with ${faucetAmount.value} test tokens.`
  } catch (error) {
    statusMessage.value = error instanceof Error ? error.message : 'Unable to fund the account.'
  } finally {
    isBusy.value = false
  }
}

function applySuggestedNonce() {
  form.value.nonce = suggestedNonce.value
  statusMessage.value = `Transaction nonce set to ${suggestedNonce.value}.`
}

async function handleSignTransaction() {
  if (!account.value) {
    statusMessage.value = 'Create or import a wallet before signing.'
    return
  }

  isBusy.value = true

  try {
    const envelope = await signTransaction(account.value, form.value)
    signedEnvelope.value = envelope
    statusMessage.value = 'Transaction signed locally using your device key.'
  } catch (error) {
    statusMessage.value = error instanceof Error ? error.message : 'Unable to sign transaction.'
  } finally {
    isBusy.value = false
  }
}

async function handleBroadcast() {
  if (!signedEnvelope.value) {
    statusMessage.value = 'Sign a transaction before broadcasting.'
    return
  }

  isBusy.value = true

  try {
    networkResponse.value = await broadcastTransaction(apiBase.value, signedEnvelope.value)
    await refreshNodeState(false)
    statusMessage.value = 'Transaction accepted by the Zephyr node mempool.'
  } catch (error) {
    statusMessage.value = error instanceof Error ? error.message : 'Broadcast failed.'
  } finally {
    isBusy.value = false
  }
}
</script>

<template>
  <main class="shell">
    <section class="hero">
      <p class="eyebrow">Zephyr Chain / Phase 1 MVP</p>
      <h1>Light wallet control without heavy infrastructure.</h1>
      <p class="lede">
        This starter wallet keeps keys on-device, signs transactions in the browser, inspects the
        current node account view, and can use a local dev faucet to exercise the hardened API flow.
      </p>
      <div class="hero-meta">
        <span class="pill">Wallet address: {{ shortAddress }}</span>
        <span class="pill" :class="networkHealthy ? 'ok' : 'warn'">
          Node health:
          {{ networkHealthy === null ? 'checking' : networkHealthy ? 'online' : 'offline' }}
        </span>
        <span class="pill">{{ balancePill }}</span>
      </div>
    </section>

    <section class="grid">
      <article class="panel">
        <div class="panel-header">
          <div>
            <p class="panel-kicker">Account</p>
            <h2>Manage your wallet</h2>
          </div>
          <button class="ghost" type="button" @click="refreshNodeState()" :disabled="isBusy || isRefreshing">
            {{ isRefreshing ? 'Refreshing...' : 'Refresh node state' }}
          </button>
        </div>

        <label class="stack">
          <span>Node API base URL</span>
          <input v-model="apiBase" type="url" placeholder="http://localhost:8080" />
        </label>

        <div class="actions">
          <button type="button" @click="handleCreateWallet" :disabled="isBusy">Create wallet</button>
          <button type="button" class="secondary" @click="handleImportWallet" :disabled="isBusy">
            Import backup
          </button>
          <button type="button" class="ghost" @click="handleClearWallet" :disabled="isBusy">
            Clear local wallet
          </button>
        </div>

        <label class="stack">
          <span>Wallet backup JSON</span>
          <textarea
            v-model="backupDraft"
            rows="10"
            spellcheck="false"
            placeholder="Generate a wallet or paste a saved backup here."
          />
        </label>

        <div class="account-card" v-if="account">
          <p><strong>Address</strong> {{ account.address }}</p>
          <p><strong>Created</strong> {{ new Date(account.createdAt).toLocaleString() }}</p>
        </div>

        <div class="account-card" v-if="accountView">
          <p><strong>Node account state</strong></p>
          <div class="stat-grid">
            <p><strong>Balance</strong> {{ accountView.balance }}</p>
            <p><strong>Available</strong> {{ accountView.availableBalance }}</p>
            <p><strong>Confirmed nonce</strong> {{ accountView.nonce }}</p>
            <p><strong>Next nonce</strong> {{ accountView.nextNonce }}</p>
            <p><strong>Pending txs</strong> {{ accountView.pendingTransactions }}</p>
          </div>

          <div class="split compact-split">
            <label class="stack">
              <span>Dev faucet amount</span>
              <input v-model.number="faucetAmount" type="number" min="1" step="1" />
            </label>
            <label class="stack">
              <span>Suggested nonce</span>
              <input :value="suggestedNonce" type="number" readonly />
            </label>
          </div>

          <div class="actions compact-actions">
            <button type="button" @click="handleFundAccount" :disabled="isBusy">Fund account</button>
            <button type="button" class="ghost" @click="applySuggestedNonce" :disabled="isBusy">
              Use next nonce
            </button>
          </div>
        </div>
      </article>

      <article class="panel">
        <div class="panel-header">
          <div>
            <p class="panel-kicker">Transactions</p>
            <h2>Sign and submit</h2>
          </div>
        </div>

        <label class="stack">
          <span>From</span>
          <input v-model="form.from" type="text" placeholder="zph_sender" />
        </label>

        <label class="stack">
          <span>To</span>
          <input v-model="form.to" type="text" placeholder="zph_recipient" />
        </label>

        <div class="split">
          <label class="stack">
            <span>Amount</span>
            <input v-model.number="form.amount" type="number" min="0" step="1" />
          </label>
          <label class="stack">
            <span>Nonce</span>
            <input v-model.number="form.nonce" type="number" min="0" step="1" />
          </label>
        </div>

        <p class="hint" v-if="accountView">
          Current available balance: {{ accountView.availableBalance }}. Next expected nonce:
          {{ accountView.nextNonce }}.
        </p>

        <label class="stack">
          <span>Memo</span>
          <input v-model="form.memo" type="text" maxlength="120" />
        </label>

        <div class="actions">
          <button type="button" @click="handleSignTransaction" :disabled="isBusy">Sign locally</button>
          <button type="button" class="secondary" @click="handleBroadcast" :disabled="isBusy">
            Broadcast
          </button>
        </div>

        <label class="stack">
          <span>Signed envelope</span>
          <textarea :value="explorerPayload" rows="14" readonly spellcheck="false" />
        </label>
      </article>
    </section>

    <section class="panel status-panel">
      <div class="panel-header">
        <div>
          <p class="panel-kicker">Status</p>
          <h2>Operator console</h2>
        </div>
      </div>

      <p class="status-copy">{{ statusMessage }}</p>

      <div class="account-card" v-if="networkResponse">
        <p><strong>Transaction ID</strong> {{ networkResponse.id }}</p>
        <p><strong>Mempool size</strong> {{ networkResponse.mempoolSize }}</p>
        <p><strong>Queued at</strong> {{ new Date(networkResponse.queuedAt).toLocaleString() }}</p>
      </div>
    </section>
  </main>
</template>

