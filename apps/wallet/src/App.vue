<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { broadcastTransaction, fetchAccount, fundAccount, pingNode } from './lib/network'
import {
  clearAccount,
  createAccount,
  hasLegacyStoredAccount,
  hasStoredAccount,
  importAccount,
  loadStoredBackup,
  saveAccount,
  signTransaction,
  unlockStoredAccount
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
const walletPassphrase = ref('')
const storedWalletPresent = ref(false)
const legacyWalletPresent = ref(false)
const signedEnvelope = ref<SignedTransactionEnvelope | null>(null)
const networkResponse = ref<BroadcastResponse | null>(null)
const networkHealthy = ref<boolean | null>(null)
const statusMessage = ref('Create, unlock, or import an account to begin.')
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
    return storedWalletPresent.value ? 'Encrypted wallet locked' : 'No wallet loaded'
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
  storedWalletPresent.value = hasStoredAccount()
  legacyWalletPresent.value = hasLegacyStoredAccount()

  if (legacyWalletPresent.value) {
    statusMessage.value =
      'Legacy unencrypted wallet storage detected. Enter a passphrase and unlock it to migrate the private key into encrypted storage.'
  } else if (storedWalletPresent.value) {
    statusMessage.value = 'Encrypted wallet found on this device. Enter its passphrase to unlock it.'
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
      statusMessage.value = storedWalletPresent.value
        ? 'Node is online. Unlock the encrypted wallet to inspect account state.'
        : 'Node is online. Create or import a wallet to inspect account state.'
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

async function activateAccount(nextAccount: StoredAccount) {
  account.value = nextAccount
  form.value.from = nextAccount.address
  form.value.nonce = 1
  signedEnvelope.value = null
  networkResponse.value = null
  storedWalletPresent.value = true
  legacyWalletPresent.value = false
  await refreshNodeState(false)
}

async function handleCreateWallet() {
  isBusy.value = true

  try {
    const nextAccount = await createAccount()
    backupDraft.value = await saveAccount(nextAccount, walletPassphrase.value)
    await activateAccount(nextAccount)
    walletPassphrase.value = ''
    statusMessage.value =
      'Fresh wallet created. Its private key is encrypted at rest; keep the passphrase and encrypted backup safe.'
  } catch (error) {
    statusMessage.value = error instanceof Error ? error.message : 'Unable to create wallet.'
  } finally {
    isBusy.value = false
  }
}

async function handleUnlockWallet() {
  if (!storedWalletPresent.value) {
    statusMessage.value = 'No stored wallet is available to unlock.'
    return
  }

  isBusy.value = true

  try {
    const wasLegacy = legacyWalletPresent.value
    const nextAccount = await unlockStoredAccount(walletPassphrase.value)
    await activateAccount(nextAccount)
    backupDraft.value = loadStoredBackup() ?? ''
    walletPassphrase.value = ''
    statusMessage.value = wasLegacy
      ? 'Legacy wallet migrated into encrypted storage and unlocked.'
      : 'Encrypted wallet unlocked for this browser session.'
  } catch (error) {
    statusMessage.value = error instanceof Error ? error.message : 'Unable to unlock wallet.'
  } finally {
    isBusy.value = false
  }
}

async function handleImportWallet() {
  isBusy.value = true

  try {
    const nextAccount = await importAccount(backupDraft.value, walletPassphrase.value)
    backupDraft.value = await saveAccount(nextAccount, walletPassphrase.value)
    await activateAccount(nextAccount)
    walletPassphrase.value = ''
    statusMessage.value = 'Wallet backup imported and stored locally in encrypted form.'
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
  storedWalletPresent.value = false
  legacyWalletPresent.value = false
  signedEnvelope.value = null
  networkResponse.value = null
  form.value.from = ''
  form.value.nonce = 1
  backupDraft.value = ''
  walletPassphrase.value = ''
  statusMessage.value = 'Wallet removed from local storage.'
}

async function handleFundAccount() {
  if (!account.value) {
    statusMessage.value = 'Create, unlock, or import a wallet before funding it.'
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
    statusMessage.value = 'Create, unlock, or import a wallet before signing.'
    return
  }

  isBusy.value = true

  try {
    const envelope = await signTransaction(account.value, form.value)
    signedEnvelope.value = envelope
    statusMessage.value = 'Transaction signed locally using the unlocked device key.'
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
        This starter wallet keeps keys on-device, encrypts private key material at rest, signs
        transactions in the browser, inspects the current node account view, and can use an explicitly
        enabled local dev faucet to exercise the hardened API flow.
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

        <label class="stack">
          <span>Wallet passphrase</span>
          <input
            v-model="walletPassphrase"
            type="password"
            minlength="10"
            autocomplete="current-password"
            placeholder="At least 10 characters"
          />
        </label>
        <p class="hint">
          The passphrase never leaves the browser. It derives an AES-GCM key used to encrypt private
          key material before it is written to local storage or exported as a backup.
        </p>

        <div class="actions">
          <button type="button" @click="handleCreateWallet" :disabled="isBusy">Create wallet</button>
          <button
            v-if="storedWalletPresent"
            type="button"
            class="secondary"
            @click="handleUnlockWallet"
            :disabled="isBusy"
          >
            Unlock stored wallet
          </button>
          <button type="button" class="secondary" @click="handleImportWallet" :disabled="isBusy">
            Import backup
          </button>
          <button type="button" class="ghost" @click="handleClearWallet" :disabled="isBusy">
            Clear local wallet
          </button>
        </div>

        <label class="stack">
          <span>Encrypted wallet backup JSON</span>
          <textarea
            v-model="backupDraft"
            rows="10"
            spellcheck="false"
            placeholder="Create a wallet or paste an encrypted backup here. Legacy plaintext backups are accepted once and migrated into encrypted storage."
          />
        </label>

        <div class="account-card" v-if="account">
          <p><strong>Address</strong> {{ account.address }}</p>
          <p><strong>Created</strong> {{ new Date(account.createdAt).toLocaleString() }}</p>
          <p><strong>Key state</strong> Unlocked in memory for this browser session</p>
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
