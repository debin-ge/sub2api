<template>
  <div v-if="groups && groups.length > 0" class="relative max-w-56">
    <!--
      分组容器：固定最大宽度，最多显示2行。
      名称被截断或还有折叠分组时，整块区域可点击/回车展开 popover 查看完整名称。
    -->
    <div
      ref="triggerRef"
      class="flex flex-wrap gap-1 max-h-14 overflow-hidden"
      :class="canExpand ? 'cursor-pointer' : null"
      :role="canExpand ? 'button' : undefined"
      :tabindex="canExpand ? 0 : undefined"
      :aria-expanded="canExpand ? showPopover : undefined"
      :aria-label="canExpand ? t('admin.accounts.viewAllGroups') : undefined"
      @click.stop="handleTriggerActivate"
      @keydown.enter.prevent="handleTriggerActivate"
      @keydown.space.prevent="handleTriggerActivate"
    >
      <GroupBadge
        v-for="group in displayGroups"
        :key="group.id"
        :name="group.name"
        :platform="group.platform"
        :subscription-type="group.subscription_type"
        :rate-multiplier="group.rate_multiplier"
        :show-rate="false"
        :title="group.name"
        :class="badgeWidthClass"
      />
      <!-- 更多数量徽章 -->
      <span
        v-if="hiddenCount > 0"
        class="inline-flex items-center gap-0.5 rounded-md px-1.5 py-0.5 text-xs font-medium bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-dark-500 transition-colors whitespace-nowrap"
      >
        +{{ hiddenCount }}
      </span>
    </div>

    <!-- Popover 显示完整列表（名称换行，不再截断） -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-150 ease-out"
        enter-from-class="opacity-0 scale-95"
        enter-to-class="opacity-100 scale-100"
        leave-active-class="transition duration-100 ease-in"
        leave-from-class="opacity-100 scale-100"
        leave-to-class="opacity-0 scale-95"
      >
        <div
          v-if="showPopover"
          ref="popoverRef"
          class="fixed z-50 min-w-48 max-w-96 rounded-lg border border-gray-200 bg-white p-3 shadow-lg dark:border-dark-600 dark:bg-dark-800"
          :style="popoverStyle"
        >
          <div class="mb-2 flex items-center justify-between">
            <span class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.groupCountTotal', { count: groups.length }) }}
            </span>
            <button
              @click="showPopover = false"
              class="rounded p-0.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-300"
            >
              <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
          <div class="flex flex-wrap items-start gap-1.5 max-h-64 overflow-y-auto">
            <GroupBadge
              v-for="group in groups"
              :key="group.id"
              :name="group.name"
              :platform="group.platform"
              :subscription-type="group.subscription_type"
              :rate-multiplier="group.rate_multiplier"
              :show-rate="false"
              wrap-name
              class="max-w-full"
            />
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- 点击外部关闭 popover -->
    <div
      v-if="showPopover"
      class="fixed inset-0 z-40"
      @click="showPopover = false"
    />
  </div>
  <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from '@/components/common/GroupBadge.vue'
import type { Group } from '@/types'

interface Props {
  groups: Group[] | null | undefined
  maxDisplay?: number
}

const props = withDefaults(defineProps<Props>(), {
  maxDisplay: 4
})

const { t } = useI18n()

// popover 定位用的估算尺寸，与模板上的 max-w-96 / max-h-64 对应
const POPOVER_WIDTH = 384
const POPOVER_HEIGHT = 280

const triggerRef = ref<HTMLElement | null>(null)
const popoverRef = ref<HTMLElement | null>(null)
const showPopover = ref(false)
const popoverStyle = ref<Record<string, string>>({})
// 名称被截断、或徽章被 max-h-14 裁掉（内容放不下时才提供展开入口）
const hasClippedContent = ref(false)

// 显示的分组（最多显示 maxDisplay 个）
const displayGroups = computed(() => {
  if (!props.groups) return []
  if (props.groups.length <= props.maxDisplay) {
    return props.groups
  }
  // 留一个位置给 +N 按钮
  return props.groups.slice(0, props.maxDisplay - 1)
})

// 隐藏的数量
const hiddenCount = computed(() => {
  if (!props.groups) return 0
  if (props.groups.length <= props.maxDisplay) return 0
  return props.groups.length - (props.maxDisplay - 1)
})

// 只有一个分组时放宽到 max-w-48：列宽上限仍由两个 max-w-24 徽章决定，表格不会变宽
const badgeWidthClass = computed(() =>
  displayGroups.value.length === 1 && hiddenCount.value === 0 ? 'max-w-48' : 'max-w-24'
)

// 有内容被裁掉，或还有没显示出来的分组时，才允许展开
const canExpand = computed(() => hiddenCount.value > 0 || hasClippedContent.value)

// 量测两种裁剪：名称横向被 truncate 掉，以及徽章整行被容器高度裁掉
const measureClipping = () => {
  const el = triggerRef.value
  if (!el) {
    hasClippedContent.value = false
    return
  }
  if (el.scrollHeight - el.clientHeight > 1) {
    hasClippedContent.value = true
    return
  }
  hasClippedContent.value = Array.from(el.querySelectorAll<HTMLElement>('span')).some(
    node => node.scrollWidth - node.clientWidth > 1
  )
}

let resizeObserver: ResizeObserver | null = null

const observeTrigger = () => {
  if (!resizeObserver) return
  resizeObserver.disconnect()
  if (triggerRef.value) resizeObserver.observe(triggerRef.value)
}

// 打开时按触发区域实时定位（不能用 computed 缓存，滚动后 rect 会失效）
const updatePopoverPosition = () => {
  const rect = triggerRef.value?.getBoundingClientRect()
  if (!rect) return

  let top = rect.bottom + 8
  let left = rect.left

  // 如果下方空间不足，显示在上方
  if (top + POPOVER_HEIGHT > window.innerHeight) {
    top = Math.max(8, rect.top - POPOVER_HEIGHT)
  }

  // 如果右侧空间不足，向左偏移
  if (left + POPOVER_WIDTH > window.innerWidth) {
    left = Math.max(8, window.innerWidth - POPOVER_WIDTH - 8)
  }

  popoverStyle.value = {
    top: `${top}px`,
    left: `${left}px`
  }
}

const handleTriggerActivate = () => {
  if (!canExpand.value) return
  if (showPopover.value) {
    showPopover.value = false
    return
  }
  updatePopoverPosition()
  showPopover.value = true
}

// 关闭 popover 的键盘事件
const handleKeydown = (e: KeyboardEvent) => {
  if (e.key === 'Escape') {
    showPopover.value = false
  }
}

watch(triggerRef, () => {
  observeTrigger()
  measureClipping()
})

watch(
  () => props.groups,
  () => {
    nextTick(measureClipping)
  }
)

onMounted(() => {
  window.addEventListener('keydown', handleKeydown)
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => measureClipping())
  }
  observeTrigger()
  nextTick(measureClipping)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
  resizeObserver?.disconnect()
  resizeObserver = null
})
</script>
