<template>
  <Teleport to="body">
    <div v-if="open && model" class="fixed inset-0 z-50">
      <button
        class="absolute inset-0 h-full w-full bg-black/50"
        type="button"
        aria-label="关闭"
        @click="$emit('close')"
      />
      <section
        ref="dialogRef"
        class="absolute inset-x-0 bottom-0 max-h-[92vh] overflow-y-auto rounded-t-lg bg-white p-5 shadow-xl dark:bg-dark-900 md:inset-y-8 md:left-1/2 md:w-[720px] md:-translate-x-1/2 md:rounded-lg"
        role="dialog"
        aria-modal="true"
        aria-labelledby="plaza-modal-title"
        tabindex="-1"
      >
        <header class="mb-5 flex items-start justify-between gap-4">
          <div class="min-w-0">
            <h2 id="plaza-modal-title" class="truncate text-xl font-semibold text-gray-900 dark:text-white">
              {{ model.displayName }}
            </h2>
            <p class="truncate text-sm text-gray-500 dark:text-gray-400">{{ model.model }}</p>
          </div>
          <button
            ref="closeButtonRef"
            data-testid="plaza-modal-close"
            class="shrink-0 rounded-md border border-gray-200 px-3 py-1.5 text-sm font-medium text-gray-700 transition hover:bg-gray-50 dark:border-dark-700 dark:text-gray-200 dark:hover:bg-dark-800"
            type="button"
            @click="$emit('close')"
          >
            关闭
          </button>
        </header>

        <div class="space-y-6">
          <section>
            <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">完整定价</h3>
            <div class="grid gap-3 md:grid-cols-2">
              <PriceLine
                label="输入"
                :value="model.minPricing.input"
                :scale="PER_MILLION_TOKEN_SCALE"
                :multiplier="multiplier"
                :rate="rate"
              />
              <PriceLine
                label="输出"
                :value="model.minPricing.output"
                :scale="PER_MILLION_TOKEN_SCALE"
                :multiplier="multiplier"
                :rate="rate"
              />
              <PriceLine
                label="缓存写入"
                :value="model.minPricing.cacheWrite"
                :scale="PER_MILLION_TOKEN_SCALE"
                :multiplier="multiplier"
                :rate="rate"
              />
              <PriceLine
                label="缓存读取"
                :value="model.minPricing.cacheRead"
                :scale="PER_MILLION_TOKEN_SCALE"
                :multiplier="multiplier"
                :rate="rate"
              />
              <PriceLine
                label="图像输出"
                :value="model.minPricing.imageOutput"
                :scale="PER_MILLION_TOKEN_SCALE"
                :multiplier="multiplier"
                :rate="rate"
              />
              <PriceLine
                label="按次计费"
                :value="model.minPricing.perRequest"
                :scale="PER_REQUEST_SCALE"
                :multiplier="multiplier"
                :rate="rate"
              />
            </div>
          </section>

          <section>
            <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">支持该模型的渠道</h3>
            <div class="space-y-2">
              <div
                v-for="item in sortedGroups"
                :key="`${item.channelName}-${item.group.id}`"
                class="rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-700"
              >
                <div class="font-medium text-gray-900 dark:text-white">{{ item.channelName }}</div>
                <div v-if="item.channelDescription" class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                  {{ item.channelDescription }}
                </div>
                <div class="mt-1 text-gray-500 dark:text-gray-400">
                  {{ item.group.name }} · rate {{ item.group.rate_multiplier || 1 }}
                </div>
              </div>
            </div>
          </section>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import PriceLine from './PriceLine.vue'
import { PER_MILLION_TOKEN_SCALE, PER_REQUEST_SCALE } from '@/utils/pricing'
import type { AggregatedModel } from '@/composables/useModelAggregation'

const props = defineProps<{
  open: boolean
  model: AggregatedModel | null
  multiplier: number
  rate: number
}>()

const emit = defineEmits<{ close: [] }>()

const dialogRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
const previousFocus = ref<HTMLElement | null>(null)

const sortedGroups = computed(() =>
  [...(props.model?.supportedGroups ?? [])].sort(
    (a, b) => (a.group.rate_multiplier || 1) - (b.group.rate_multiplier || 1)
  )
)

function restorePreviousFocus() {
  if (previousFocus.value?.isConnected) {
    previousFocus.value.focus()
  }
  previousFocus.value = null
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    emit('close')
  }
}

watch(
  () => props.open && !!props.model,
  async (isOpen, wasOpen) => {
    if (isOpen) {
      previousFocus.value = document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null
      document.addEventListener('keydown', handleKeydown)
      await nextTick()
      ;(closeButtonRef.value ?? dialogRef.value)?.focus()
      return
    }

    if (wasOpen) {
      document.removeEventListener('keydown', handleKeydown)
      restorePreviousFocus()
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleKeydown)
  restorePreviousFocus()
})
</script>
