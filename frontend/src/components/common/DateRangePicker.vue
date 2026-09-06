<template>
  <div class="relative" ref="containerRef">
    <button
      type="button"
      @click="toggle"
      :class="['date-picker-trigger', isOpen && 'date-picker-trigger-open']"
    >
      <span class="date-picker-icon">
        <Icon name="calendar" size="sm" />
      </span>
      <span class="date-picker-value">
        {{ displayValue }}
      </span>
      <span class="date-picker-chevron">
        <Icon
          name="chevronDown"
          size="sm"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <Transition name="date-picker-dropdown">
      <div v-if="isOpen" class="date-picker-dropdown">
        <!-- Quick presets -->
        <div class="date-picker-presets">
          <button
            v-for="preset in presets"
            :key="preset.value"
            @click="selectPreset(preset)"
            :class="['date-picker-preset', isPresetActive(preset) && 'date-picker-preset-active']"
          >
            {{ t(preset.labelKey) }}
          </button>
        </div>

        <div class="date-picker-divider"></div>

        <!-- Custom date range inputs -->
        <div class="date-picker-custom">
          <div class="date-picker-field">
            <label class="date-picker-label">{{ t('dates.startDate') }}</label>
            <input
              type="date"
              v-model="localStartDate"
              :max="localEndDate || tomorrow"
              class="date-picker-input"
              @change="onDateChange"
            />
          </div>
          <div class="date-picker-separator">
            <Icon name="arrowRight" size="sm" class="text-gray-400" />
          </div>
          <div class="date-picker-field">
            <label class="date-picker-label">{{ t('dates.endDate') }}</label>
            <input
              type="date"
              v-model="localEndDate"
              :min="localStartDate"
              :max="tomorrow"
              class="date-picker-input"
              @change="onDateChange"
            />
          </div>
        </div>

        <!-- Apply button -->
        <div class="date-picker-actions">
          <button @click="apply" class="date-picker-apply">
            {{ t('dates.apply') }}
          </button>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { formatLocalDate, getLast24HoursRange, type DateRangeSelection } from '@/utils/dateRange'

interface DatePreset {
  labelKey: string
  value: string
  getRange: () => DateRangeSelection
}

interface Props {
  startDate: string
  endDate: string
}

interface Emits {
  (e: 'update:startDate', value: string): void
  (e: 'update:endDate', value: string): void
  (
    e: 'change',
    range: {
      startDate: string
      endDate: string
      // Exact bounds for rolling presets; undefined means "use the dates".
      startTime?: string
      endTime?: string
      preset: string | null
    }
  ): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t, locale } = useI18n()

const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const localStartDate = ref(props.startDate)
const localEndDate = ref(props.endDate)
// Exact bounds of the current selection. Only rolling presets set them; a
// manual date edit clears them so the query falls back to whole calendar days.
const localStartTime = ref<string | undefined>(undefined)
const localEndTime = ref<string | undefined>(undefined)
// Resolved from the initial dates by detectPreset() below, so the label never
// flashes a preset the parent did not actually select.
const activePreset = ref<string | null>(null)

// Use local timezone to avoid UTC timezone issues
const today = computed(() => formatLocalDate(new Date()))

// Tomorrow's date - used for max date to handle timezone differences
// When user is in a timezone behind the server, "today" on server might be "tomorrow" locally
const tomorrow = computed(() => {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return formatLocalDate(d)
})

const presets: DatePreset[] = [
  {
    labelKey: 'dates.today',
    value: 'today',
    getRange: () => {
      const t = today.value
      return { start: t, end: t }
    }
  },
  {
    labelKey: 'dates.yesterday',
    value: 'yesterday',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 1)
      const yesterday = formatLocalDate(d)
      return { start: yesterday, end: yesterday }
    }
  },
  {
    labelKey: 'dates.last24Hours',
    value: 'last24Hours',
    // The only rolling preset: it carries exact instants because the two dates
    // it spans would otherwise be read as a 48-hour window.
    getRange: () => getLast24HoursRange()
  },
  {
    labelKey: 'dates.last7Days',
    value: '7days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 6)
      const start = formatLocalDate(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last14Days',
    value: '14days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 13)
      const start = formatLocalDate(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last30Days',
    value: '30days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 29)
      const start = formatLocalDate(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.thisMonth',
    value: 'thisMonth',
    getRange: () => {
      const now = new Date()
      const start = formatLocalDate(new Date(now.getFullYear(), now.getMonth(), 1))
      return { start, end: today.value }
    }
  },
  {
    labelKey: 'dates.lastMonth',
    value: 'lastMonth',
    getRange: () => {
      const now = new Date()
      const start = formatLocalDate(new Date(now.getFullYear(), now.getMonth() - 1, 1))
      const end = formatLocalDate(new Date(now.getFullYear(), now.getMonth(), 0))
      return { start, end }
    }
  }
]

const displayValue = computed(() => {
  if (activePreset.value) {
    const preset = presets.find((p) => p.value === activePreset.value)
    if (preset) return t(preset.labelKey)
  }

  if (localStartDate.value && localEndDate.value) {
    if (localStartDate.value === localEndDate.value) {
      return formatDate(localStartDate.value)
    }
    return `${formatDate(localStartDate.value)} - ${formatDate(localEndDate.value)}`
  }

  return t('dates.selectDateRange')
})

const formatDate = (dateStr: string): string => {
  const date = new Date(dateStr + 'T00:00:00')
  const dateLocale = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return date.toLocaleDateString(dateLocale, { month: 'short', day: 'numeric' })
}

const isPresetActive = (preset: DatePreset): boolean => {
  return activePreset.value === preset.value
}

const selectPreset = (preset: DatePreset) => {
  const range = preset.getRange()
  localStartDate.value = range.start
  localEndDate.value = range.end
  localStartTime.value = range.startTime
  localEndTime.value = range.endTime
  activePreset.value = preset.value
}

// Label the current dates with whatever preset produces them, if any.
// `adoptExactWindow` decides whether a matched rolling preset also hands over
// its instants: yes when the dates came from outside (mount, parent update),
// no when the user typed them — two dates typed by hand mean two whole days,
// even when they happen to coincide with the last-24-hours span.
const detectPreset = (adoptExactWindow: boolean) => {
  activePreset.value = null
  for (const preset of presets) {
    const range = preset.getRange()
    if (range.start === localStartDate.value && range.end === localEndDate.value) {
      activePreset.value = preset.value
      if (adoptExactWindow) {
        localStartTime.value = range.startTime
        localEndTime.value = range.endTime
      }
      break
    }
  }
}

const onDateChange = () => {
  localStartTime.value = undefined
  localEndTime.value = undefined
  detectPreset(false)
}

const toggle = () => {
  isOpen.value = !isOpen.value
}

const apply = () => {
  emit('update:startDate', localStartDate.value)
  emit('update:endDate', localEndDate.value)
  emit('change', {
    startDate: localStartDate.value,
    endDate: localEndDate.value,
    startTime: localStartTime.value,
    endTime: localEndTime.value,
    preset: activePreset.value
  })
  isOpen.value = false
}

const handleClickOutside = (event: MouseEvent) => {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    isOpen.value = false
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && isOpen.value) {
    isOpen.value = false
  }
}

// Sync local state with props. A prop that already matches is the parent
// echoing back our own apply() — re-running detection there would discard the
// instants we just emitted.
watch(
  () => props.startDate,
  (val) => {
    if (val === localStartDate.value) return
    localStartDate.value = val
    detectPreset(true)
  }
)

watch(
  () => props.endDate,
  (val) => {
    if (val === localEndDate.value) return
    localEndDate.value = val
    detectPreset(true)
  }
)

// Label the initial dates before the first render rather than in onMounted, so
// the trigger shows the right preset from the very first paint.
detectPreset(true)

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleEscape)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleEscape)
})
</script>

<style scoped>
.date-picker-trigger {
  @apply flex items-center gap-2;
  @apply rounded-lg px-3 py-2 text-sm;
  @apply bg-white dark:bg-dark-800;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-700 dark:text-gray-300;
  @apply transition-all duration-200;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
  @apply hover:border-gray-300 dark:hover:border-dark-500;
  @apply cursor-pointer;
}

.date-picker-trigger-open {
  @apply border-primary-500 ring-2 ring-primary-500/30;
}

.date-picker-icon {
  @apply text-gray-400 dark:text-dark-400;
}

.date-picker-value {
  @apply font-medium;
}

.date-picker-chevron {
  @apply text-gray-400 dark:text-dark-400;
}

.date-picker-dropdown {
  @apply absolute left-0 z-[100] mt-2;
  @apply bg-white dark:bg-dark-800;
  @apply rounded-xl;
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  @apply overflow-hidden;
  @apply min-w-[320px];
}

.date-picker-presets {
  @apply grid grid-cols-2 gap-1 p-2;
}

.date-picker-preset {
  @apply rounded-md px-3 py-1.5 text-xs font-medium;
  @apply text-gray-600 dark:text-gray-400;
  @apply hover:bg-gray-100 dark:hover:bg-dark-700;
  @apply transition-colors duration-150;
}

.date-picker-preset-active {
  @apply bg-primary-100 dark:bg-primary-900/30;
  @apply text-primary-700 dark:text-primary-300;
}

.date-picker-divider {
  @apply border-t border-gray-100 dark:border-dark-700;
}

.date-picker-custom {
  @apply flex items-end gap-2 p-3;
}

.date-picker-field {
  @apply flex-1;
}

.date-picker-label {
  @apply mb-1 block text-xs font-medium text-gray-500 dark:text-gray-400;
}

.date-picker-input {
  @apply w-full rounded-md px-2 py-1.5 text-sm;
  @apply bg-gray-50 dark:bg-dark-700;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-900 dark:text-gray-100;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
}

.date-picker-input::-webkit-calendar-picker-indicator {
  @apply cursor-pointer opacity-60 hover:opacity-100;
  filter: invert(0.5);
}

.dark .date-picker-input::-webkit-calendar-picker-indicator {
  filter: none;
}

.date-picker-separator {
  @apply flex items-center justify-center pb-1;
}

.date-picker-actions {
  @apply flex justify-end p-2 pt-0;
}

.date-picker-apply {
  @apply rounded-lg px-4 py-1.5 text-sm font-medium;
  @apply bg-primary-600 text-white;
  @apply hover:bg-primary-700;
  @apply transition-colors duration-150;
}

/* Dropdown animation */
.date-picker-dropdown-enter-active,
.date-picker-dropdown-leave-active {
  transition: all 0.2s ease;
}

.date-picker-dropdown-enter-from,
.date-picker-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
