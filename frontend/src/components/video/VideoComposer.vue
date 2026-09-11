<template>
  <div
    class="rounded-2xl border border-gray-200 bg-white p-3 shadow-lg dark:border-gray-700 dark:bg-gray-800"
  >
    <Transition name="fade">
      <div
        v-if="open"
        class="mb-3 max-h-80 overflow-y-auto rounded-xl bg-gray-50 p-3 dark:bg-gray-900/40"
      >
        <slot name="settings" />
      </div>
    </Transition>

    <div class="flex items-end gap-2">
      <button
        type="button"
        class="btn btn-ghost btn-icon shrink-0"
        :title="t('videoPlayground.composer.settings')"
        :aria-expanded="open"
        @click="emit('update:open', !open)"
      >
        <Icon name="cog" size="md" />
      </button>

      <textarea
        ref="textareaRef"
        :value="prompt"
        rows="1"
        class="input max-h-40 flex-1 resize-none"
        :placeholder="placeholder"
        :disabled="disabled"
        @input="onInput"
        @keydown.enter.exact.prevent="emit('submit')"
      />

      <button
        type="button"
        class="btn btn-primary btn-icon shrink-0"
        :disabled="disabled || submitting || !prompt.trim()"
        :title="t('videoPlayground.composer.submit')"
        @click="emit('submit')"
      >
        <Icon :name="submitting ? 'refresh' : 'arrowUp'" size="md" :class="submitting ? 'animate-spin' : ''" />
      </button>
    </div>

    <p v-if="hint" class="mt-2 px-1 text-xs text-gray-500 dark:text-gray-400">{{ hint }}</p>
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon } from '@/components/icons'

const props = defineProps<{
  prompt: string
  open: boolean
  submitting: boolean
  disabled: boolean
  placeholder: string
  hint?: string
}>()

const emit = defineEmits<{
  (e: 'update:prompt', value: string): void
  (e: 'update:open', value: boolean): void
  (e: 'submit'): void
}>()

const { t } = useI18n()
const textareaRef = ref<HTMLTextAreaElement | null>(null)

function resize(): void {
  const element = textareaRef.value
  if (!element) return
  element.style.height = 'auto'
  element.style.height = `${element.scrollHeight}px`
}

function onInput(event: Event): void {
  emit('update:prompt', (event.target as HTMLTextAreaElement).value)
  resize()
}

// 外部改写 prompt（复用上一条、提交后清空）时也要跟着收放。
watch(
  () => props.prompt,
  () => {
    void nextTick(resize)
  },
)
</script>
