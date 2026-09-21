<template>
  <div class="rounded-2xl border border-dark-700 bg-dark-900 shadow-glow-lg">
    <div class="flex items-center gap-4 overflow-x-auto border-b border-dark-700 px-4 py-3 sm:px-6">
      <div class="flex shrink-0 items-center gap-1.5">
        <span class="h-3 w-3 rounded-full bg-red-500/70"></span>
        <span class="h-3 w-3 rounded-full bg-amber-500/70"></span>
        <span class="h-3 w-3 rounded-full bg-emerald-500/70"></span>
      </div>

      <div class="flex items-center gap-4 font-mono text-xs sm:text-sm" role="tablist">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab.id"
          class="whitespace-nowrap py-1 font-medium transition-colors"
          :class="activeTab === tab.id ? 'text-emerald-400' : 'text-gray-500 hover:text-gray-300'"
          @click="activeTab = tab.id"
        >
          {{ tab.label }}
        </button>
      </div>
    </div>

    <pre
      class="max-h-96 overflow-auto whitespace-pre-wrap break-words px-4 py-4 font-mono text-[12px] leading-6 text-gray-200 sm:px-6"
    ><code><span
      v-for="(segment, index) in activeSegments"
      :key="index"
      :class="segment.class"
    >{{ segment.text }}</span><span class="caret-blink ml-1 inline-block h-4 w-1.5 align-text-bottom bg-cyan-400"></span></code></pre>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'

const { t } = useI18n()
const appStore = useAppStore()

const baseUrl = computed(() => {
  const trimmed = (appStore.cachedPublicSettings?.api_base_url || appStore.apiBaseUrl || '').trim()
  const fallback = typeof window === 'undefined' ? '/' : `${window.location.origin}/`
  const base = trimmed || fallback
  return base.endsWith('/') ? base.slice(0, -1) : base
})

type TabId = 'chat' | 'responses' | 'claude'

const tabs = computed(() => [
  { id: 'chat' as TabId, label: t('radar.home.docs.tabs.chat', 'Chat Completions') },
  { id: 'responses' as TabId, label: t('radar.home.docs.tabs.responses', 'Responses') },
  { id: 'claude' as TabId, label: t('radar.home.docs.tabs.claude', 'Claude Messages') },
])

const activeTab = ref<TabId>('chat')

interface Segment {
  text: string
  class?: string
}

const COMMAND = 'text-cyan-400'
const STRING = 'text-emerald-300'
const RESPONSE = 'text-gray-500'
const HIGHLIGHT = 'text-amber-400'

function requestSegments(url: string, headers: string[], body: string): Segment[] {
  const segments: Segment[] = [
    { text: 'curl', class: COMMAND },
    { text: ` ${url} \\\n` },
  ]
  headers.forEach((header) => {
    segments.push({ text: '  -H ' })
    segments.push({ text: `"${header}"`, class: STRING })
    segments.push({ text: ' \\\n' })
  })
  segments.push({ text: '  -d ' })
  segments.push({ text: `'${body}'`, class: STRING })
  return segments
}

const segments = computed<Record<TabId, Segment[]>>(() => ({
  chat: [
    ...requestSegments(
      `${baseUrl.value}/v1/chat/completions`,
      ['Authorization: Bearer YOUR_API_KEY', 'Content-Type: application/json'],
      '{\n    "model": "gpt-4o",\n    "messages": [{ "role": "user", "content": "Hello!" }]\n  }',
    ),
    { text: '\n\n' },
    { text: '{ "id": "chatcmpl-8f2c...", "object": "chat.completion", "model": "gpt-4o", "choices": [{ "index": 0, "message": { "role": "assistant", "content": "Hello! How can I help you today?" }, "finish_reason": "stop" }], "usage": { "prompt_tokens": 9, "completion_tokens": 12, "total_tokens": ', class: RESPONSE },
    { text: '21', class: HIGHLIGHT },
    { text: ' } }', class: RESPONSE },
  ],
  responses: [
    ...requestSegments(
      `${baseUrl.value}/v1/responses`,
      ['Authorization: Bearer YOUR_API_KEY', 'Content-Type: application/json'],
      '{\n    "model": "gpt-4o",\n    "input": "Hello!"\n  }',
    ),
    { text: '\n\n' },
    { text: '{ "id": "resp_8f2c...", "object": "response", "model": "gpt-4o", "status": "completed", "output": [{ "type": "message", "role": "assistant", "content": [{ "type": "output_text", "text": "Hello! How can I help you today?" }] }], "usage": { "input_tokens": 9, "output_tokens": 12, "total_tokens": ', class: RESPONSE },
    { text: '21', class: HIGHLIGHT },
    { text: ' } }', class: RESPONSE },
  ],
  claude: [
    ...requestSegments(
      `${baseUrl.value}/v1/messages`,
      ['x-api-key: YOUR_API_KEY', 'anthropic-version: 2023-06-01', 'Content-Type: application/json'],
      '{\n    "model": "claude-sonnet-5",\n    "max_tokens": 1024,\n    "messages": [{ "role": "user", "content": "Hello!" }]\n  }',
    ),
    { text: '\n\n' },
    { text: '{ "id": "msg_8f2c...", "type": "message", "role": "assistant", "model": "claude-sonnet-5", "content": [{ "type": "text", "text": "Hello! How can I help you today?" }], "stop_reason": "end_turn", "usage": { "input_tokens": 9, "output_tokens": ', class: RESPONSE },
    { text: '12', class: HIGHLIGHT },
    { text: ' } }', class: RESPONSE },
  ],
}))

const activeSegments = computed(() => segments.value[activeTab.value])
</script>
