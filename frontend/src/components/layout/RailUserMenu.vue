<template>
  <div v-if="user" ref="rootRef" class="relative">
    <button type="button" class="zt-rail-user" :aria-label="t('common.userMenu')" @click="toggleMenu">
      <span class="zt-avatar">
        <img v-if="avatarUrl" :src="avatarUrl" :alt="displayName" />
        <span v-else>{{ userInitials }}</span>
      </span>
      <span class="zt-rail-user-text">
        <b>{{ displayName }}</b>
        <small>{{ formatMoney(availableBalance) }}<template v-if="frozenBalance > 0"> · {{ t('nav.rail.frozen') }} {{ formatMoney(frozenBalance) }}</template></small>
      </span>
      <Icon name="chevronUp" size="sm" />
    </button>

    <transition name="dropdown">
      <div v-if="open" class="dropdown zt-rail-menu">
        <div class="border-b px-4 py-3" style="border-color: var(--zt-border)">
          <div class="text-sm font-medium" style="color: var(--zt-ink)">{{ displayName }}</div>
          <div class="text-xs" style="color: var(--zt-ink-3)">{{ user.email }}</div>
          <div class="mt-1 text-xs" style="color: var(--zt-ink-3)">{{ t('admin.users.roles.' + user.role) }}</div>
        </div>

        <div class="border-b px-4 py-2.5 text-xs" style="border-color: var(--zt-border)">
          <div class="flex items-center justify-between">
            <span style="color: var(--zt-ink-3)">{{ balanceAvailableText }}</span>
            <span class="num font-medium" style="color: var(--zt-ink)">{{ formatMoney(availableBalance) }}</span>
          </div>
          <div v-if="frozenBalance > 0" class="mt-1.5 flex items-center justify-between">
            <span style="color: var(--zt-ink-3)">{{ balanceFrozenText }}</span>
            <span class="num font-medium" style="color: var(--zt-warn-ink)">{{ formatMoney(frozenBalance) }}</span>
          </div>
          <div v-if="frozenBalance > 0" class="mt-1.5 flex items-center justify-between border-t pt-1.5" style="border-color: var(--zt-border)">
            <span style="color: var(--zt-ink-3)">{{ balanceTotalText }}</span>
            <span class="num font-semibold" style="color: var(--zt-ink)">{{ formatMoney(totalBalance) }}</span>
          </div>
        </div>

        <div class="py-1">
          <router-link to="/profile" class="dropdown-item" @click="closeMenu">
            <Icon name="user" size="sm" />
            {{ t('nav.profile') }}
          </router-link>
          <router-link to="/keys" class="dropdown-item" @click="closeMenu">
            <Icon name="key" size="sm" />
            {{ t('nav.apiKeys') }}
          </router-link>
        </div>

        <!-- Contact Support (only show if configured) -->
        <div v-if="hasSupportContact" class="border-t px-4 py-2.5" style="border-color: var(--zt-border)">
          <button
            v-if="contactQRCodeEnabled"
            type="button"
            class="flex w-full items-center gap-2 rounded-md text-left text-xs transition-colors hover:text-primary-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 dark:hover:text-primary-400"
            style="color: var(--zt-ink-3)"
            @click="openSupportDialog"
          >
            <Icon name="chatBubble" size="xs" class="flex-shrink-0" />
            <span>{{ t('common.contactSupport') }}:</span>
            <span class="min-w-0 flex-1 truncate font-medium" style="color: var(--zt-ink)">
              {{ contactInfo || t('common.viewSupportQrCode') }}
            </span>
            <Icon name="chevronRight" size="xs" class="flex-shrink-0" />
          </button>
          <div v-else class="flex items-center gap-2 text-xs" style="color: var(--zt-ink-3)">
            <Icon name="chatBubble" size="xs" class="flex-shrink-0" />
            <span>{{ t('common.contactSupport') }}:</span>
            <span class="font-medium" style="color: var(--zt-ink)">{{ contactInfo }}</span>
          </div>
        </div>

        <div v-if="showOnboardingButton" class="border-t py-1" style="border-color: var(--zt-border)">
          <button type="button" class="dropdown-item w-full" @click="handleReplayGuide">
            <Icon name="questionCircle" size="sm" />
            {{ t('onboarding.restartTour') }}
          </button>
        </div>

        <div class="border-t py-1" style="border-color: var(--zt-border)">
          <button
            type="button"
            class="dropdown-item w-full text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
            @click="handleLogout"
          >
            <Icon name="login" size="sm" class="rotate-180" />
            {{ t('nav.logout') }}
          </button>
        </div>
      </div>
    </transition>

    <CustomerSupportDialog :show="supportDialogOpen" :contact-info="contactInfo" @close="supportDialogOpen = false" />
  </div>
</template>

<script setup lang="ts">
/**
 * 侧栏底部用户卡 + 弹层菜单：承接改版前 AppHeader 用户下拉的全部内容
 * （资料 / 密钥 / 联系客服 / 重放引导 / 退出 / 余额明细）。
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import CustomerSupportDialog from '@/components/common/CustomerSupportDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatBalanceAmount } from '@/utils/formatters'

const router = useRouter()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const onboardingStore = useOnboardingStore()

const rootRef = ref<HTMLElement | null>(null)
const open = ref(false)
const supportDialogOpen = ref(false)

const user = computed(() => authStore.user)
const avatarUrl = computed(() => user.value?.avatar_url?.trim() || '')
const contactInfo = computed(() => appStore.contactInfo)
const contactQRCodeEnabled = computed(() => appStore.contactQRCodeEnabled)
const hasSupportContact = computed(() => Boolean(contactInfo.value || contactQRCodeEnabled.value))
const availableBalance = computed(() => Number(user.value?.balance || 0))
const frozenBalance = computed(() => Number(user.value?.frozen_balance || 0))
const totalBalance = computed(() => availableBalance.value + frozenBalance.value)
const balanceAvailableText = computed(() => (t('common.availableBalance') === 'common.availableBalance' ? '可用余额' : t('common.availableBalance')))
const balanceFrozenText = computed(() => (t('common.frozenBalance') === 'common.frozenBalance' ? '冻结金额' : t('common.frozenBalance')))
const balanceTotalText = computed(() => (t('common.totalBalance') === 'common.totalBalance' ? '总余额' : t('common.totalBalance')))

// 只在标准模式的管理员下显示新手引导按钮
const showOnboardingButton = computed(() => !authStore.isSimpleMode && user.value?.role === 'admin')

const userInitials = computed(() => {
  if (!user.value) return ''
  if (user.value.username) return user.value.username.substring(0, 2).toUpperCase()
  if (user.value.email) return user.value.email.split('@')[0].substring(0, 2).toUpperCase()
  return ''
})

const displayName = computed(() => {
  if (!user.value) return ''
  return user.value.username || user.value.email?.split('@')[0] || ''
})

function formatMoney(value: number) {
  return `$${formatBalanceAmount(value)}`
}

function toggleMenu() {
  open.value = !open.value
}

function closeMenu() {
  open.value = false
}

function openSupportDialog() {
  closeMenu()
  supportDialogOpen.value = true
}

async function handleLogout() {
  closeMenu()
  try {
    await authStore.logout()
  } catch (error) {
    // Ignore logout errors - still redirect to login
    console.error('Logout error:', error)
  }
  await router.push('/login')
}

function handleReplayGuide() {
  closeMenu()
  onboardingStore.replay()
}

function handleClickOutside(event: MouseEvent) {
  if (rootRef.value && !rootRef.value.contains(event.target as Node)) {
    closeMenu()
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.zt-rail-menu {
  left: 0;
  bottom: calc(100% + 6px);
  width: 240px;
  transform-origin: bottom left;
}

.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.18s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.96) translateY(4px);
}
</style>
