<template>
  <div
    data-testid="plaza-model-list"
    class="overflow-x-auto rounded-xl border border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900"
  >
    <table class="w-full min-w-[760px] text-left text-sm">
      <thead class="bg-gray-50 text-[11px] font-semibold uppercase tracking-wide text-gray-400 dark:bg-dark-800/50 dark:text-gray-500">
        <tr>
          <th scope="col" class="px-4 py-2.5">{{ t('plaza.list.model') }}</th>
          <th scope="col" class="px-3 py-2.5">{{ t('plaza.list.kind') }}</th>
          <th scope="col" class="px-3 py-2.5 text-gray-600 dark:text-gray-300">{{ t('plaza.list.standard') }}</th>
          <th v-if="showVipColumn" scope="col" class="px-3 py-2.5 text-orange-600 dark:text-orange-400">
            {{ t('plaza.list.vip') }}
          </th>
          <th scope="col" class="px-4 py-2.5">{{ t('plaza.list.groups') }}</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-gray-100 dark:divide-dark-800/70">
        <tr
          v-for="entry in entries"
          :key="`${entry.model.platform}::${entry.model.model}`"
          :data-billing-kind="entry.model.billingKind"
          class="group cursor-pointer align-top transition hover:bg-primary-50/40 dark:hover:bg-primary-500/5"
          @click="$emit('open-detail', entry.model)"
        >
          <td class="px-4 py-3">
            <div class="flex items-center gap-2.5">
              <span
                :class="['flex h-7 w-7 shrink-0 items-center justify-center rounded-lg', platformBadgeLightClass(entry.model.platform)]"
                aria-hidden="true"
              >
                <PlatformIcon :platform="entry.model.platform as GroupPlatform" size="sm" />
              </span>
              <div class="min-w-0">
                <button
                  type="button"
                  class="block max-w-[16rem] truncate text-left font-semibold text-gray-900 outline-none group-hover:text-primary-600 focus-visible:underline dark:text-white dark:group-hover:text-primary-400"
                  @click.stop="$emit('open-detail', entry.model)"
                >
                  {{ entry.model.displayName }}
                </button>
                <p class="mt-0.5 text-[11px] text-gray-500 dark:text-gray-400">
                  {{ platformLabel(entry.model.platform) }}
                  <span v-if="entry.model.recentCalls > 0" class="tabular-nums text-gray-400">
                    · {{ t('plaza.list.recentCalls') }} {{ formatCallCount(entry.model.recentCalls) }}
                  </span>
                </p>
              </div>
            </div>
          </td>
          <td class="px-3 py-3">
            <span :class="['inline-flex items-center gap-1 whitespace-nowrap rounded-full px-1.5 py-px text-[11px] font-medium', kindStyle(entry.model).badge]">
              <span :class="['h-1 w-1 rounded-full', kindStyle(entry.model).dot]" aria-hidden="true"></span>
              {{ t(kindStyle(entry.model).labelKey) }}
            </span>
          </td>
          <td class="px-3 py-3">
            <PriceLines v-if="entry.model.standardPricing" :lines="entry.standard" />
            <span v-else class="text-gray-300 dark:text-gray-600">-</span>
          </td>
          <td v-if="showVipColumn" class="px-3 py-3">
            <PriceLines v-if="entry.model.vipPricing" :lines="entry.vip" vip />
            <span v-else class="text-gray-300 dark:text-gray-600">-</span>
          </td>
          <td class="max-w-[16rem] px-4 py-3">
            <PlazaGroupTags
              :groups="entry.model.supportedGroups"
              :billing-kind="entry.model.billingKind"
              :server-utc-offset="serverUtcOffset"
              size="sm"
            />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlazaGroupTags from './PlazaGroupTags.vue'
import { listRowsFor, plazaUnitLabel, rowLabel, unitScale } from './plazaPricingRows'
import { PLAZA_BILLING_KIND_STYLES, formatCallCount } from './plazaBillingKind'
import { platformBadgeLightClass, platformLabel } from '@/utils/platformColors'
import { formatCNYEffective, scheduledScaledPrice } from '@/utils/pricing'
import type { AggregatedModel, PlazaPricingSummary } from '@/composables/useModelAggregation'
import type { GroupPlatform } from '@/types'

interface PriceLine {
  key: string
  label: string
  value: string
  unit: string
  title?: string
}

const props = defineProps<{
  models: AggregatedModel[]
  serverUtcOffset?: string
}>()

defineEmits<{
  'open-detail': [model: AggregatedModel]
}>()

const { t } = useI18n()

const kindStyle = (model: AggregatedModel) => PLAZA_BILLING_KIND_STYLES[model.billingKind]

const showVipColumn = computed(() => props.models.some((model) => model.vipPricing != null))

function priceLines(model: AggregatedModel, summary: PlazaPricingSummary | null): PriceLine[] {
  if (!summary) return []
  const schedule = model.timeSchedule
  const scheduled =
    typeof schedule?.peak_multiplier === 'number' && typeof schedule?.off_peak_multiplier === 'number'
  return listRowsFor(model)
    .filter((item) => summary.minPricing[item.key] != null)
    .map((item) => {
      const raw = summary.minPricing[item.key] ?? null
      const scale = unitScale(item.unit)
      const rate = summary.minPricingRateMultipliers[item.key]
      const format = (value: number | null) => formatCNYEffective(value, scale, rate)
      // 分时价与卡片口径一致：高峰 / 空闲两档都由倍率换算。
      const value =
        scheduled && item.unit === 'million'
          ? `${format(scheduledScaledPrice(raw, schedule?.peak_multiplier))} / ${format(scheduledScaledPrice(raw, schedule?.off_peak_multiplier))}`
          : format(raw)
      return {
        key: item.key,
        label: rowLabel(t, item),
        value,
        unit: plazaUnitLabel(t, item.unit),
        title: scheduled && item.unit === 'million' ? t('plaza.list.peakOffPeak') : undefined
      }
    })
}

const entries = computed(() =>
  props.models.map((model) => ({
    model,
    standard: priceLines(model, model.standardPricing),
    vip: priceLines(model, model.vipPricing)
  }))
)

const PriceLines = defineComponent({
  props: {
    lines: { type: Array as PropType<PriceLine[]>, required: true },
    vip: { type: Boolean, default: false }
  },
  setup(lineProps) {
    return () =>
      h(
        'dl',
        { class: 'grid grid-cols-[auto_auto_auto] items-baseline justify-start gap-x-2 gap-y-0.5' },
        lineProps.lines.flatMap((line) => [
          h('dt', { key: `${line.key}-label`, class: 'whitespace-nowrap text-[11px] text-gray-500 dark:text-gray-400' }, line.label),
          h(
            'dd',
            {
              key: `${line.key}-value`,
              title: line.title,
              class: [
                'whitespace-nowrap text-right font-mono text-[13px] font-semibold tabular-nums',
                lineProps.vip ? 'text-orange-600 dark:text-orange-400' : 'text-gray-900 dark:text-white'
              ]
            },
            line.value
          ),
          h(
            'dd',
            {
              key: `${line.key}-unit`,
              class: ['whitespace-nowrap font-mono text-[10px]', lineProps.vip ? 'text-orange-400' : 'text-gray-400']
            },
            line.unit
          )
        ])
      )
  }
})
</script>
