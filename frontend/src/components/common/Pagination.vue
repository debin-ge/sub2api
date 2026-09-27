<template>
  <div class="zt-pagination" :class="{ 'is-compact': compact }">
    <!-- 紧凑模式：单行「‹ 第 x / y 页 · 每页 ›」 -->
    <div v-if="compact" class="zt-pagination-mobile">
      <button type="button" class="zt-page-nav" :disabled="page === 1" :aria-label="t('pagination.previous')" @click="goToPage(page - 1)">‹</button>
      <div class="zt-pagination-mid">
        <span class="text-sm zt-ink-2">
          {{ t('pagination.pageOf', { page, total: totalPages }) }}
        </span>
        <div v-if="showPageSizeSelector" class="page-size-select w-20">
          <Select
            :model-value="pageSize"
            :options="pageSizeSelectOptions"
            @update:model-value="handlePageSizeChange"
          />
        </div>
      </div>
      <button type="button" class="zt-page-nav" :disabled="page === totalPages" :aria-label="t('pagination.next')" @click="goToPage(page + 1)">›</button>
    </div>

    <!-- Mobile pagination -->
    <div v-if="!compact" class="zt-pagination-mobile sm:hidden">
      <button type="button" class="zt-page-nav" :disabled="page === 1" @click="goToPage(page - 1)">
        {{ t('pagination.previous') }}
      </button>
      <span class="text-sm zt-ink-2">
        {{ t('pagination.pageOf', { page, total: totalPages }) }}
      </span>
      <button type="button" class="zt-page-nav" :disabled="page === totalPages" @click="goToPage(page + 1)">
        {{ t('pagination.next') }}
      </button>
    </div>

    <div v-if="!compact" class="hidden sm:flex sm:flex-1 sm:items-center sm:justify-between">
      <!-- Desktop pagination info -->
      <div class="flex items-center space-x-4">
        <p class="text-sm zt-ink-2">
          {{ t('pagination.showing') }}
          <span class="font-medium">{{ fromItem }}</span>
          {{ t('pagination.to') }}
          <span class="font-medium">{{ toItem }}</span>
          {{ t('pagination.of') }}
          <span class="font-medium">{{ total }}</span>
          {{ t('pagination.results') }}
        </p>

        <!-- Page size selector -->
        <div v-if="showPageSizeSelector" class="flex items-center space-x-2">
          <span class="text-sm zt-ink-2"
            >{{ t('pagination.perPage') }}:</span
          >
          <div class="page-size-select w-20">
            <Select
              :model-value="pageSize"
              :options="pageSizeSelectOptions"
              @update:model-value="handlePageSizeChange"
            />
          </div>
        </div>

        <div v-if="showJump" class="flex items-center space-x-2">
          <span class="text-sm zt-ink-2">{{ t('pagination.jumpTo') }}</span>
          <input
            v-model="jumpPage"
            type="number"
            min="1"
            :max="totalPages"
            class="input w-20 text-sm"
            :placeholder="t('pagination.jumpPlaceholder')"
            @keyup.enter="submitJump"
          />
          <button type="button" class="btn btn-ghost btn-sm" @click="submitJump">
            {{ t('pagination.jumpAction') }}
          </button>
        </div>
      </div>

      <!-- Desktop pagination buttons -->
      <nav
        class="relative z-0 inline-flex -space-x-px rounded-md shadow-sm"
        aria-label="Pagination"
      >
        <!-- Previous button -->
        <button
          @click="goToPage(page - 1)"
          :disabled="page === 1"
          class="zt-page-btn is-first"
          :aria-label="t('pagination.previous')"
        >
          <Icon name="chevronLeft" size="md" />
        </button>

        <!-- Page numbers -->
        <button
          v-for="(pageNum, index) in visiblePages"
          :key="`${pageNum}-${index}`"
          @click="typeof pageNum === 'number' && goToPage(pageNum)"
          :disabled="typeof pageNum !== 'number'"
          :class="[
            'zt-page-btn',
            pageNum === page && 'is-current',
            typeof pageNum !== 'number' && 'cursor-default'
          ]"
          :aria-label="
            typeof pageNum === 'number' ? t('pagination.goToPage', { page: pageNum }) : undefined
          "
          :aria-current="pageNum === page ? 'page' : undefined"
        >
          {{ pageNum }}
        </button>

        <!-- Next button -->
        <button
          @click="goToPage(page + 1)"
          :disabled="page === totalPages"
          class="zt-page-btn is-last"
          :aria-label="t('pagination.next')"
        >
          <Icon name="chevronRight" size="md" />
        </button>
      </nav>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from './Select.vue'
import { getConfiguredTablePageSizeOptions, normalizeTablePageSize } from '@/utils/tablePreferences'
import { setPersistedPageSize } from '@/composables/usePersistedPageSize'

const { t } = useI18n()

interface Props {
  total: number
  page: number
  pageSize: number
  pageSizeOptions?: number[]
  showPageSizeSelector?: boolean
  showJump?: boolean
  /** 紧凑模式：单行「‹ 第 x / y 页 · 每页 ›」，用于窄列（如分栏视图的列表列） */
  compact?: boolean
}

interface Emits {
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', pageSize: number): void
}

const props = withDefaults(defineProps<Props>(), {
  pageSizeOptions: () => getConfiguredTablePageSizeOptions(),
  showPageSizeSelector: true,
  showJump: false,
  compact: false
})

const emit = defineEmits<Emits>()

const totalPages = computed(() => Math.ceil(props.total / props.pageSize))

const fromItem = computed(() => {
  if (props.total === 0) return 0
  return (props.page - 1) * props.pageSize + 1
})

const toItem = computed(() => {
  const to = props.page * props.pageSize
  return to > props.total ? props.total : to
})

const pageSizeSelectOptions = computed(() => {
  const options = Array.from(
    new Set([
      ...getConfiguredTablePageSizeOptions(),
      normalizeTablePageSize(props.pageSize)
    ])
  ).sort((a, b) => a - b)

  return options.map((size) => ({
    value: size,
    label: String(size)
  }))
})

const jumpPage = ref('')

const visiblePages = computed(() => {
  const pages: (number | string)[] = []
  const maxVisible = 7
  const total = totalPages.value

  if (total <= maxVisible) {
    // Show all pages if total is small
    for (let i = 1; i <= total; i++) {
      pages.push(i)
    }
  } else {
    // Always show first page
    pages.push(1)

    const start = Math.max(2, props.page - 2)
    const end = Math.min(total - 1, props.page + 2)

    // Add ellipsis before if needed
    if (start > 2) {
      pages.push('...')
    }

    // Add middle pages
    for (let i = start; i <= end; i++) {
      pages.push(i)
    }

    // Add ellipsis after if needed
    if (end < total - 1) {
      pages.push('...')
    }

    // Always show last page
    pages.push(total)
  }

  return pages
})

const goToPage = (newPage: number) => {
  if (newPage >= 1 && newPage <= totalPages.value && newPage !== props.page) {
    emit('update:page', newPage)
  }
}

const handlePageSizeChange = (value: string | number | boolean | null) => {
  if (value === null || typeof value === 'boolean') return
  const newPageSize = normalizeTablePageSize(typeof value === 'string' ? parseInt(value, 10) : value)
  setPersistedPageSize(newPageSize)
  emit('update:pageSize', newPageSize)
}

const submitJump = () => {
  const value = String(jumpPage.value).trim()
  if (!value) return
  const pageNum = Number.parseInt(value, 10)
  if (Number.isNaN(pageNum)) return
  const nextPage = Math.min(Math.max(pageNum, 1), totalPages.value)
  jumpPage.value = ''
  goToPage(nextPage)
}
</script>

<style scoped>
.page-size-select :deep(.select-trigger) {
  @apply px-3 py-1.5 text-sm;
}
</style>

<style scoped>
.zt-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 16px;
  border-top: 1px solid var(--zt-border);
  background: var(--zt-surface);
  font-size: 13px;
}
@media (min-width: 640px) {
  .zt-pagination {
    padding-inline: 24px;
  }
}
.zt-pagination.is-compact {
  padding: 8px;
  background: transparent;
}
.zt-pagination-mobile {
  display: flex;
  flex: 1;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
}
/* 非紧凑模式下，≥640px 只显示桌面分页；scoped 属性选择器会压过 Tailwind 的 sm:hidden，这里显式覆盖 */
@media (min-width: 640px) {
  .zt-pagination:not(.is-compact) .zt-pagination-mobile {
    display: none;
  }
}
.zt-pagination-mid {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex-wrap: wrap;
  justify-content: center;
}
.zt-page-nav {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  height: 28px;
  padding: 0 10px;
  border-radius: 7px;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--zt-ink);
  background: var(--zt-surface);
  border: 1px solid var(--zt-border-2);
}
.zt-page-nav:hover:not(:disabled) {
  background: var(--zt-surface-2);
}
.zt-page-nav:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.zt-page-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 32px;
  height: 30px;
  padding: 0 8px;
  font: 500 12.5px/1 var(--zt-mono);
  font-variant-numeric: tabular-nums;
  color: var(--zt-ink-2);
  background: var(--zt-surface);
  border: 1px solid var(--zt-border-2);
}
.zt-page-btn.is-first {
  border-radius: 7px 0 0 7px;
}
.zt-page-btn.is-last {
  border-radius: 0 7px 7px 0;
}
.zt-page-btn:hover:not(:disabled):not(.is-current) {
  background: var(--zt-surface-2);
  color: var(--zt-ink);
}
.zt-page-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.zt-page-btn.is-current {
  z-index: 1;
  border-color: var(--zt-accent-400);
  background: var(--zt-accent-50);
  color: var(--zt-accent);
  font-weight: 600;
}
</style>
