<template>
  <div class="table-page-layout zt-panel" :class="{ 'mobile-mode': isMobile }">
    <!-- 工具栏：筛选在左、操作在右（插槽 API 与改版前一致） -->
    <div v-if="$slots.filters || $slots.actions" class="zt-toolbar">
      <div v-if="$slots.filters" class="layout-filters">
        <slot name="filters" />
      </div>
      <div v-if="$slots.actions" class="layout-actions">
        <slot name="actions" />
      </div>
    </div>

    <!-- 表区：桌面端限高滚动，DataTable 的虚拟滚动仍以 .table-wrapper 为滚动元素 -->
    <div class="table-scroll-container">
      <slot name="table" />
    </div>

    <!-- 分页脚 -->
    <div v-if="$slots.pagination" class="zt-pager-slot">
      <slot name="pagination" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const isMobile = ref(false)

const checkMobile = () => {
  isMobile.value = window.innerWidth < 1024
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
})
</script>

<style scoped>
.table-page-layout {
  @apply flex flex-col;
  overflow: hidden;
}

.layout-filters {
  @apply flex-1 min-w-0;
}

.layout-actions {
  @apply ml-auto flex items-center gap-2;
}

/* 表格滚动容器 - 桌面端限高，表体在容器内滚动 */
.table-scroll-container {
  @apply flex flex-col;
  min-height: 0;
}

.table-scroll-container :deep(.table-wrapper) {
  @apply flex-1 overflow-x-auto overflow-y-auto;
  max-height: calc(100vh - 320px);
  /* 确保横向滚动条显示在最底部 */
  scrollbar-gutter: stable;
}

.table-scroll-container :deep(table) {
  @apply w-full;
  min-width: max-content; /* 关键：确保表格宽度根据内容撑开，从而触发横向滚动 */
  display: table; /* 使用标准 table 布局以支持 sticky 列 */
}

.table-scroll-container :deep(thead) {
  background: var(--zt-surface-2);
}

.table-scroll-container :deep(th) {
  @apply px-4 py-2.5 text-left text-[11.5px] font-semibold;
  letter-spacing: 0.02em;
  white-space: nowrap;
  color: var(--zt-ink-2);
  border-bottom: 1px solid var(--zt-border);
}

.table-scroll-container :deep(td) {
  @apply px-4 py-3 text-[13px];
  color: var(--zt-ink);
  border-bottom: 1px solid var(--zt-border);
}

/* 分页脚：Pagination 自带上边线与内边距 */
.zt-pager-slot {
  flex: none;
}

/* 移动端：恢复整页滚动 */
.table-page-layout.mobile-mode .table-scroll-container :deep(.table-wrapper) {
  @apply overflow-x-auto;
  max-height: none;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(table) {
  @apply flex-none;
  display: table;
  min-width: 100%;
}
</style>
