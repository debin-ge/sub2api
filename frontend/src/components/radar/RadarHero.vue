<template>
  <section class="relative overflow-hidden bg-dark-950">
    <div class="tech-grid absolute inset-0"></div>
    <div class="absolute left-1/2 top-0 h-[520px] w-[900px] -translate-x-1/2 -translate-y-1/3 rounded-full bg-primary-500/20 blur-[120px]"></div>
    <div class="absolute -right-24 top-1/3 h-80 w-80 rounded-full bg-accent-400/10 blur-3xl"></div>

    <div class="relative mx-auto grid max-w-7xl items-center gap-16 px-4 py-20 sm:px-6 lg:grid-cols-2 lg:py-28 lg:px-8">
      <!-- Copy -->
      <div class="mx-auto max-w-2xl text-center lg:mx-0 lg:max-w-xl lg:text-left">
        <p
          class="mb-5 inline-flex items-center gap-2 rounded-full border border-primary-400/30 bg-primary-500/10 px-3 py-1 font-mono text-xs font-medium text-primary-300"
        >
          <span class="relative flex h-1.5 w-1.5">
            <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75"></span>
            <span class="relative inline-flex h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
          </span>
          {{ t('radar.hero.eyebrow', 'Model Radar · AI Gateway') }}
        </p>

        <h1 class="text-4xl font-extrabold leading-tight tracking-tight text-white sm:text-5xl">
          {{ t('radar.hero.titleLead', 'One gateway,') }}
          <span class="text-gradient block sm:inline">{{ t('radar.hero.titleHighlight', 'endless AI models') }}</span>
        </h1>

        <p class="mx-auto mt-5 max-w-xl text-base leading-7 text-gray-300 sm:text-lg lg:mx-0">
          {{
            t(
              'radar.hero.description',
              'One standardized protocol connects your apps to hundreds of model endpoints, with keys and quotas centrally managed for reliable access to every AI workload.',
            )
          }}
        </p>

        <div class="mt-8 flex flex-col items-center gap-3 sm:flex-row sm:justify-center lg:justify-start">
          <router-link to="/register" class="btn btn-lg btn-primary shadow-glow">
            {{ t('radar.hero.primaryCta', 'Get started') }}
          </router-link>
          <router-link
            to="/docs"
            class="btn btn-lg border border-white/15 bg-white/5 text-white backdrop-blur hover:bg-white/10"
          >
            {{ t('radar.hero.secondaryCta', 'View integration docs') }}
          </router-link>
        </div>

        <p class="mt-6 text-xs text-gray-500" aria-live="polite">
          {{ t('radar.hero.clientUpdated', 'Page data fetched') }}:
          <time v-if="parsedDate" :datetime="dateTimeValue">{{ formattedLastFetchedAt }}</time>
          <span v-else>{{ t('radar.common.never', 'Not yet') }}</span>
        </p>
      </div>

      <!-- Gateway hub diagram -->
      <div class="relative mx-auto hidden h-[380px] w-full max-w-lg lg:block" aria-hidden="true">
        <svg class="absolute inset-0 h-full w-full" viewBox="0 0 100 100" preserveAspectRatio="none">
          <line
            v-for="(node, i) in providerNodes"
            :key="`provider-line-${node.abbr}`"
            class="flow-line"
            :x1="8"
            :y1="node.top"
            x2="50"
            y2="50"
            stroke="rgba(129, 140, 248, 0.35)"
            stroke-width="0.3"
            :style="{ animationDelay: `${i * 0.2}s` }"
          />
          <line
            v-for="(node, i) in appNodes"
            :key="`app-line-${node.abbr}`"
            class="flow-line"
            x1="50"
            y1="50"
            :x2="92"
            :y2="node.top"
            stroke="rgba(34, 211, 238, 0.4)"
            stroke-width="0.3"
            :style="{ animationDelay: `${i * 0.2 + 0.1}s` }"
          />
        </svg>

        <div
          v-for="node in providerNodes"
          :key="`provider-${node.abbr}`"
          class="absolute flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-1.5"
          :style="{ left: '8%', top: `${node.top}%` }"
        >
          <span
            class="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-primary-500 to-accent-400 text-xs font-bold text-white shadow-glow"
          >
            <ProviderIcon v-if="node.logoKey" :provider="node.logoKey" :size="20" class="text-white" />
            <template v-else>{{ node.abbr }}</template>
          </span>
          <span class="whitespace-nowrap text-[11px] font-medium text-white/60">{{ node.label }}</span>
        </div>

        <div
          v-for="node in appNodes"
          :key="`app-${node.abbr}`"
          class="absolute flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-1.5"
          :style="{ left: '92%', top: `${node.top}%` }"
        >
          <span
            class="flex h-9 w-9 items-center justify-center rounded-xl border border-accent-400/30 bg-dark-800 text-xs font-bold text-accent-300"
          >
            <ProviderIcon v-if="node.logoKey" :provider="node.logoKey" :size="20" class="text-accent-300" />
            <template v-else>{{ node.abbr }}</template>
          </span>
          <span class="whitespace-nowrap text-[11px] font-medium text-white/60">{{ node.label }}</span>
        </div>

        <!-- Hub -->
        <div class="absolute left-1/2 top-1/2 flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-2">
          <div class="relative flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-primary-500 to-accent-400 shadow-glow-lg">
            <span class="sonar-ring absolute inset-0 rounded-2xl border-2 border-primary-300/60"></span>
            <span class="sonar-ring absolute inset-0 rounded-2xl border-2 border-accent-300/50" style="animation-delay: 1.3s"></span>
            <img :src="siteLogo || '/logo.svg'" alt="" class="relative h-9 w-9 object-contain" />
          </div>
          <div class="text-center">
            <p class="text-sm font-semibold text-white">{{ siteName }} {{ t('radar.hero.gatewaySuffix', 'Gateway') }}</p>
            <p class="text-[11px] text-white/60">{{ t('radar.hero.gatewaySubtitle', 'Unified billing · Smart routing') }}</p>
          </div>
        </div>

        <div
          class="absolute -top-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-white/10 bg-dark-800/90 px-3 py-1.5 text-xs font-medium text-white shadow-lg backdrop-blur"
        >
          <Icon name="bolt" size="xs" class="text-accent-300" />
          92ms {{ t('radar.hero.badgeLatency', 'avg latency') }}
        </div>
        <div
          class="absolute -bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-white/10 bg-dark-800/90 px-3 py-1.5 text-xs font-medium text-white shadow-lg backdrop-blur"
        >
          <Icon name="check" size="xs" class="text-emerald-400" />
          99.9% {{ t('radar.hero.badgeUptime', 'uptime') }}
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const props = withDefaults(
  defineProps<{
    lastFetchedAt?: Date | string | null
  }>(),
  {
    lastFetchedAt: null,
  },
)

const { t, locale } = useI18n()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'ZenTok')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))

interface GatewayNode {
  label: string
  abbr: string
  top: number
  logoKey?: string
}

const providerNodes: GatewayNode[] = [
  { label: 'OpenAI', abbr: 'O', top: 10, logoKey: 'openai' },
  { label: 'Anthropic', abbr: 'A', top: 29, logoKey: 'anthropic' },
  { label: 'Gemini', abbr: 'G', top: 50, logoKey: 'gemini' },
  { label: 'DeepSeek', abbr: 'D', top: 71, logoKey: 'deepseek' },
  { label: 'Kimi', abbr: 'K', top: 90, logoKey: 'kimi' },
]
const appNodes: GatewayNode[] = [
  { label: 'Claude Code', abbr: 'CC', top: 10, logoKey: 'anthropic' },
  { label: 'Codex', abbr: 'CX', top: 29 },
  { label: 'Cursor', abbr: 'CU', top: 50, logoKey: 'cursor' },
  { label: 'Trae', abbr: 'TR', top: 71, logoKey: 'trae' },
  { label: 'WorkBuddy', abbr: 'WB', top: 90 },
]

const parsedDate = computed(() => {
  if (!props.lastFetchedAt) return null
  const value = props.lastFetchedAt instanceof Date ? props.lastFetchedAt : new Date(props.lastFetchedAt)
  return Number.isFinite(value.getTime()) ? value : null
})
const dateTimeValue = computed(() => parsedDate.value?.toISOString())
const formattedLastFetchedAt = computed(() => {
  if (!parsedDate.value) return t('radar.common.unknownTime', 'Unknown')
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(parsedDate.value)
})
</script>
