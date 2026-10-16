<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  fetchTrustedCitizenObject,
  type CitizenTrustAnchor,
  type TrustedCitizenObject
} from '../lib/v2CitizenTrusted'

const props = defineProps<{ apiBase?: string }>()
const STORAGE_KEY = 'zephyr:v2:citizen-trust-anchor'

const network = ref('')
const validatorRoot = ref('')
const shardId = ref(0)
const objectId = ref('')
const result = ref<TrustedCitizenObject | null>(null)
const errorMessage = ref('')
const verifying = ref(false)
const online = ref(typeof navigator === 'undefined' ? true : navigator.onLine)
const active = ref(typeof document === 'undefined' ? true : document.visibilityState === 'visible')

const mode = computed(() => {
  if (!online.value) return 'offline verifier'
  if (!active.value) return 'header/proof cache only'
  return 'verify + relay/sampling eligible'
})

const trustConfigured = computed(() => /^[0-9a-fA-F]{64}$/.test(network.value) && /^[0-9a-fA-F]{64}$/.test(validatorRoot.value))

onMounted(() => {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored) {
    try {
      const anchor = JSON.parse(stored) as CitizenTrustAnchor
      network.value = anchor.network
      validatorRoot.value = anchor.validatorRoot
    } catch {
      localStorage.removeItem(STORAGE_KEY)
    }
  }
  window.addEventListener('online', syncOnline)
  window.addEventListener('offline', syncOnline)
  document.addEventListener('visibilitychange', syncVisibility)
})

onBeforeUnmount(() => {
  window.removeEventListener('online', syncOnline)
  window.removeEventListener('offline', syncOnline)
  document.removeEventListener('visibilitychange', syncVisibility)
})

function syncOnline() {
  online.value = navigator.onLine
}

function syncVisibility() {
  active.value = document.visibilityState === 'visible'
}

function saveAnchor(anchor: CitizenTrustAnchor) {
  network.value = anchor.network
  validatorRoot.value = anchor.validatorRoot
  localStorage.setItem(STORAGE_KEY, JSON.stringify(anchor))
}

function resetAnchor() {
  localStorage.removeItem(STORAGE_KEY)
  network.value = ''
  validatorRoot.value = ''
  result.value = null
  errorMessage.value = ''
}

async function verifyObject() {
  if (!trustConfigured.value) {
    errorMessage.value = 'Enter the 32-byte genesis/checkpoint NetworkID and ValidatorRoot first.'
    return
  }
  if (!/^[0-9a-fA-F]{64}$/.test(objectId.value)) {
    errorMessage.value = 'Object ID must be a 32-byte hex value.'
    return
  }
  verifying.value = true
  errorMessage.value = ''
  try {
    const verified = await fetchTrustedCitizenObject(
      props.apiBase ?? '',
      shardId.value,
      objectId.value.toLowerCase(),
      { network: network.value.toLowerCase(), validatorRoot: validatorRoot.value.toLowerCase() }
    )
    result.value = verified
    // Advance only after QC + validator-root + shard + state proof verification.
    saveAnchor(verified.nextTrustAnchor)
  } catch (error) {
    result.value = null
    errorMessage.value = error instanceof Error ? error.message : 'Citizen proof verification failed.'
  } finally {
    verifying.value = false
  }
}
</script>

<template>
  <section class="citizen-shell">
    <div class="citizen-header">
      <div>
        <p class="citizen-kicker">Zephyr v2 / Citizen Node</p>
        <h2>Verify the chain on this device</h2>
        <p>
          The RPC supplies bytes and proofs; this wallet verifies the validator quorum, shard commitment,
          and object Sparse-Merkle proof locally before accepting state.
        </p>
      </div>
      <span class="citizen-mode">{{ mode }}</span>
    </div>

    <div class="citizen-grid">
      <label>
        <span>Trusted NetworkID</span>
        <input v-model.trim="network" autocomplete="off" spellcheck="false" placeholder="64 hex chars from genesis" />
      </label>
      <label>
        <span>Trusted ValidatorRoot</span>
        <input v-model.trim="validatorRoot" autocomplete="off" spellcheck="false" placeholder="64 hex chars from genesis/checkpoint" />
      </label>
      <label>
        <span>Shard</span>
        <input v-model.number="shardId" type="number" min="0" step="1" />
      </label>
      <label>
        <span>Object ID</span>
        <input v-model.trim="objectId" autocomplete="off" spellcheck="false" placeholder="64 hex chars" />
      </label>
    </div>

    <div class="citizen-actions">
      <button type="button" :disabled="verifying || !online" @click="verifyObject">
        {{ verifying ? 'Verifying locally…' : 'Fetch + verify proof' }}
      </button>
      <button type="button" class="secondary" @click="resetAnchor">Reset trust anchor</button>
    </div>

    <p v-if="errorMessage" class="citizen-error">{{ errorMessage }}</p>
    <div v-if="result" class="citizen-proof">
      <strong>Cryptographically verified</strong>
      <span>height {{ result.height.toString() }} · shard {{ result.shardId }}</span>
      <span>state root {{ result.stateRoot }}</span>
      <span>{{ result.objectPresent ? 'object included in finalized state' : 'verified absence proof' }}</span>
      <span>next validator root {{ result.nextTrustAnchor.validatorRoot }}</span>
    </div>
  </section>
</template>

<style scoped>
.citizen-shell {
  max-width: 1180px;
  margin: 0 auto 32px;
  padding: 24px;
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 20px;
  background: rgba(15, 23, 42, 0.86);
  color: #e2e8f0;
}
.citizen-header { display: flex; gap: 24px; justify-content: space-between; align-items: flex-start; }
.citizen-header h2 { margin: 4px 0 8px; }
.citizen-header p { margin: 0; max-width: 760px; line-height: 1.55; }
.citizen-kicker { text-transform: uppercase; letter-spacing: .12em; font-size: .75rem; opacity: .72; }
.citizen-mode { white-space: nowrap; border: 1px solid rgba(45, 212, 191, .45); border-radius: 999px; padding: 7px 11px; color: #5eead4; }
.citizen-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-top: 22px; }
.citizen-grid label { display: grid; gap: 6px; }
.citizen-grid span { font-size: .85rem; color: #cbd5e1; }
.citizen-grid input { width: 100%; box-sizing: border-box; border: 1px solid #334155; border-radius: 10px; padding: 10px 12px; background: #0f172a; color: #f8fafc; }
.citizen-actions { display: flex; gap: 10px; margin-top: 18px; }
.citizen-actions button { border: 0; border-radius: 10px; padding: 10px 14px; cursor: pointer; font-weight: 700; }
.citizen-actions button:disabled { opacity: .55; cursor: not-allowed; }
.citizen-actions .secondary { background: #334155; color: #e2e8f0; }
.citizen-error { color: #fca5a5; margin-bottom: 0; }
.citizen-proof { display: grid; gap: 5px; margin-top: 18px; padding: 14px; border-radius: 12px; background: rgba(13, 148, 136, .12); overflow-wrap: anywhere; }
.citizen-proof strong { color: #5eead4; }
@media (max-width: 760px) {
  .citizen-header { display: grid; }
  .citizen-grid { grid-template-columns: 1fr; }
  .citizen-mode { width: fit-content; }
}
</style>
