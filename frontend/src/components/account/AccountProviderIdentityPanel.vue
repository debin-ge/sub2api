<template>
  <section
    class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800/60"
    data-testid="provider-identity-panel"
    @keydown.enter.prevent
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h4 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
          {{ t('admin.accounts.providerIdentity.title') }}
        </h4>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.providerIdentity.description') }}
        </p>
      </div>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadState">
        {{ t('common.refresh') }}
      </button>
    </div>

    <p v-if="loading" class="mt-3 text-xs text-gray-500">{{ t('common.loading') }}</p>
    <template v-else-if="state">
      <div class="mt-3 grid grid-cols-1 gap-2 text-xs sm:grid-cols-3">
        <div class="rounded border border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-700">
          <span class="text-gray-500">{{ t('admin.accounts.providerIdentity.state') }}</span>
          <div class="mt-1 font-medium" :class="stateClass">{{ stateLabel }}</div>
        </div>
        <div class="rounded border border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-700">
          <span class="text-gray-500">{{ t('admin.accounts.providerIdentity.identityVersion') }}</span>
          <div class="mt-1 font-mono">{{ state.identity_version }}</div>
        </div>
        <div class="rounded border border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-700">
          <span class="text-gray-500">{{ t('admin.accounts.providerIdentity.fingerprint') }}</span>
          <div class="mt-1 font-mono">{{ state.binding?.principal_fingerprint || '—' }}</div>
        </div>
      </div>

      <p class="mt-3 text-xs text-amber-700 dark:text-amber-300">
        {{ t('admin.accounts.providerIdentity.rawNotStored') }}
      </p>

      <div
        v-if="state.isolation_state === 'unverified'"
        class="mt-4 space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <h5 class="text-sm font-medium">{{ t('admin.accounts.providerIdentity.proposeTitle') }}</h5>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <select v-model="proposal.principal_kind" class="input">
            <option v-for="option in principalKinds" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </select>
          <input
            v-model="proposal.principal"
            type="password"
            class="input font-mono"
            autocomplete="new-password"
            data-1p-ignore
            data-lpignore="true"
            data-bwignore="true"
            data-testid="provider-identity-principal"
            :placeholder="t('admin.accounts.providerIdentity.principalPlaceholder')"
          />
          <input
            v-model="proposal.evidence_ref"
            class="input font-mono"
            data-testid="provider-identity-evidence"
            :placeholder="t('admin.accounts.providerIdentity.evidencePlaceholder')"
          />
          <input
            v-model="proposal.reason"
            class="input"
            data-testid="provider-identity-reason"
            :placeholder="t('admin.accounts.providerIdentity.reasonPlaceholder')"
          />
        </div>
        <button type="button" class="btn btn-primary btn-sm" data-testid="provider-identity-propose" :disabled="busy || !canPropose" @click="propose">
          {{ t('admin.accounts.providerIdentity.propose') }}
        </button>
      </div>

      <div
        v-if="pendingReviews.length"
        class="mt-4 space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600"
      >
        <h5 class="text-sm font-medium">{{ t('admin.accounts.providerIdentity.pendingTitle') }}</h5>
        <div
          v-for="review in pendingReviews"
          :key="review.id"
          class="rounded border border-gray-200 bg-white p-3 text-xs dark:border-dark-600 dark:bg-dark-700"
        >
          <div class="flex flex-wrap gap-x-4 gap-y-1">
            <span>#{{ review.id }}</span>
            <span>{{ review.principal_kind }}</span>
            <span class="font-mono">{{ review.principal_fingerprint }}</span>
            <span>{{ t('admin.accounts.providerIdentity.proposedBy', { id: review.proposed_by }) }}</span>
          </div>
          <p class="mt-2 text-gray-600 dark:text-gray-300">{{ review.reason }} · {{ review.evidence_ref }}</p>
          <input
            v-model="decisionReasons[review.id]"
            class="input mt-2"
            :placeholder="t('admin.accounts.providerIdentity.decisionPlaceholder')"
          />
          <div class="mt-2 flex gap-2">
            <button
              type="button"
              class="btn btn-primary btn-sm"
              :data-testid="`provider-identity-approve-${review.id}`"
              :disabled="busy || review.proposed_by === currentAdminID || !decisionReasons[review.id]?.trim()"
              @click="decide(review, true)"
            >
              {{ t('admin.accounts.providerIdentity.approve') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :data-testid="`provider-identity-reject-${review.id}`"
              :disabled="busy || !decisionReasons[review.id]?.trim()"
              @click="decide(review, false)"
            >
              {{ t('admin.accounts.providerIdentity.reject') }}
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="state.isolation_state === 'verified'"
        class="mt-4 space-y-3 border-t border-red-200 pt-4 dark:border-red-900/60"
      >
        <h5 class="text-sm font-medium text-red-700 dark:text-red-300">
          {{ t('admin.accounts.providerIdentity.revokeTitle') }}
        </h5>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <input
            v-model="revocation.evidence_ref"
            class="input font-mono"
            data-testid="provider-identity-revoke-evidence"
            :placeholder="t('admin.accounts.providerIdentity.evidencePlaceholder')"
          />
          <input
            v-model="revocation.reason"
            class="input"
            data-testid="provider-identity-revoke-reason"
            :placeholder="t('admin.accounts.providerIdentity.revokeReasonPlaceholder')"
          />
        </div>
        <p class="text-xs text-red-600 dark:text-red-300">
          {{ t('admin.accounts.providerIdentity.revokeWarning') }}
        </p>
        <button type="button" class="btn btn-danger btn-sm" data-testid="provider-identity-revoke" :disabled="busy || !canRevoke" @click="revoke">
          {{ t('admin.accounts.providerIdentity.revoke') }}
        </button>
      </div>

      <details v-if="state.reviews.length" class="mt-4 border-t border-gray-200 pt-3 text-xs dark:border-dark-600">
        <summary class="cursor-pointer font-medium">{{ t('admin.accounts.providerIdentity.history') }}</summary>
        <ul class="mt-2 space-y-2">
          <li v-for="review in state.reviews" :key="review.id" class="rounded bg-white p-2 dark:bg-dark-700">
            #{{ review.id }} · {{ review.status }} · {{ review.principal_kind }} ·
            <span class="font-mono">{{ review.principal_fingerprint }}</span>
          </li>
        </ul>
      </details>
    </template>
  </section>
  <TotpStepUpDialog :controller="stepUp" />
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Account } from '@/types'
import type { AccountProviderIdentityReview, AccountProviderIdentityState } from '@/api/admin/accounts'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { isStepUpCancelled, useStepUp } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'

const props = defineProps<{ account: Account }>()
const emit = defineEmits<{ changed: [state: AccountProviderIdentityState] }>()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const stepUp = useStepUp()
const state = ref<AccountProviderIdentityState | null>(null)
const loading = ref(false)
const busy = ref(false)
const decisionReasons = reactive<Record<number, string>>({})
const operationKeys = new Map<string, string>()
const proposal = reactive({
  principal_kind: 'project' as AccountProviderIdentityReview['principal_kind'],
  principal: '',
  reason: '',
  evidence_ref: ''
})
const revocation = reactive({ reason: '', evidence_ref: '' })

const currentAdminID = computed(() => authStore.user?.id ?? 0)
const pendingReviews = computed(() => state.value?.reviews.filter(review => review.status === 'pending') ?? [])
const canPropose = computed(() =>
  proposal.principal.trim().length >= 3 && proposal.reason.trim().length >= 4 && proposal.evidence_ref.trim().length >= 3
)
const canRevoke = computed(() => revocation.reason.trim().length >= 4 && revocation.evidence_ref.trim().length >= 3)
const stateLabel = computed(() => t(`admin.accounts.providerIdentity.states.${state.value?.isolation_state ?? 'unverified'}`))
const stateClass = computed(() =>
  state.value?.isolation_state === 'verified'
    ? 'text-emerald-600'
    : state.value?.isolation_state === 'revoked'
      ? 'text-red-600'
      : 'text-amber-600'
)
const principalKinds = computed(() =>
  (['account', 'organization', 'project', 'tenant', 'workspace'] as const).map(value => ({
    value,
    label: t(`admin.accounts.providerIdentity.kinds.${value}`)
  }))
)

function operationKey(scope: string): string {
  const storageKey = `sub2api:provider-identity:${props.account.id}:${scope}`
  let value = operationKeys.get(storageKey) ?? null
  try {
    value = sessionStorage.getItem(storageKey) ?? value
  } catch {
    // In-memory retry protection remains available when browser storage is blocked.
  }
  if (!value) {
    value = `provider-identity-${props.account.id}-${scope}-${globalThis.crypto?.randomUUID?.() ?? Date.now()}`
    operationKeys.set(storageKey, value)
    try {
      sessionStorage.setItem(storageKey, value)
    } catch {
      // Keep the in-memory key for ambiguous retries in this page session.
    }
  }
  return value
}

function clearOperationKey(scope: string): void {
  const storageKey = `sub2api:provider-identity:${props.account.id}:${scope}`
  operationKeys.delete(storageKey)
  try {
    sessionStorage.removeItem(storageKey)
  } catch {
    // The in-memory key has already been removed.
  }
}

async function loadState(): Promise<void> {
  loading.value = true
  try {
    state.value = await adminAPI.accounts.getProviderIdentity(props.account.id)
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.accounts.providerIdentity.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function propose(): Promise<void> {
  if (!state.value || !canPropose.value) return
  const scope = 'propose'
  const accountID = props.account.id
  const version = state.value.identity_version
  const payload = { ...proposal }
  const key = operationKey(scope)
  busy.value = true
  try {
    const result = await stepUp.run(() => adminAPI.accounts.proposeProviderIdentity(
      accountID,
      version,
      payload,
      key
    ))
    clearOperationKey(scope)
    proposal.principal = ''
    state.value = result.state
    emit('changed', result.state)
    appStore.showSuccess(t('admin.accounts.providerIdentity.proposed'))
  } catch (error: any) {
    if (isStepUpCancelled(error)) return
    appStore.showError(error?.message || t('admin.accounts.providerIdentity.operationFailed'))
  } finally {
    busy.value = false
  }
}

async function decide(review: AccountProviderIdentityReview, approve: boolean): Promise<void> {
  if (!state.value) return
  const reason = decisionReasons[review.id]?.trim() ?? ''
  if (!reason) return
  const scope = `${approve ? 'approve' : 'reject'}:${review.id}`
  const accountID = props.account.id
  const version = state.value.identity_version
  const key = operationKey(scope)
  busy.value = true
  try {
    const result = await stepUp.run(() => adminAPI.accounts.decideProviderIdentity(
      accountID,
      review.id,
      version,
      approve,
      reason,
      key
    ))
    clearOperationKey(scope)
    delete decisionReasons[review.id]
    state.value = result.state
    emit('changed', result.state)
    appStore.showSuccess(t(approve ? 'admin.accounts.providerIdentity.approved' : 'admin.accounts.providerIdentity.rejected'))
  } catch (error: any) {
    if (isStepUpCancelled(error)) return
    appStore.showError(error?.message || t('admin.accounts.providerIdentity.operationFailed'))
  } finally {
    busy.value = false
  }
}

async function revoke(): Promise<void> {
  if (!state.value || !canRevoke.value) return
  const scope = 'revoke'
  const accountID = props.account.id
  const payload = { ...revocation }
  const key = operationKey(scope)
  busy.value = true
  try {
    const result = await stepUp.run(() => adminAPI.accounts.revokeProviderIdentity(
      accountID,
      payload,
      key
    ))
    clearOperationKey(scope)
    state.value = result.state
    emit('changed', result.state)
    appStore.showSuccess(t('admin.accounts.providerIdentity.revoked'))
  } catch (error: any) {
    if (isStepUpCancelled(error)) return
    appStore.showError(error?.message || t('admin.accounts.providerIdentity.operationFailed'))
  } finally {
    busy.value = false
  }
}

onMounted(loadState)
watch(() => props.account.id, loadState)
</script>
