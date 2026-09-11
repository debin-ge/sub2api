<template>
  <AppLayout>
    <div class="mx-auto flex h-full max-w-4xl flex-col gap-4 px-4 py-4">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
            {{ t('videoPlayground.title') }}
          </h1>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('videoPlayground.subtitle') }}</p>
        </div>
        <div class="flex items-center gap-2">
          <Select
            v-if="videoApiKeys.length > 0"
            :model-value="selectedApiKeyId"
            :options="apiKeyOptions"
            class="w-56"
            :aria-label="t('videoPlayground.apiKey')"
            @update:modelValue="(value) => selectApiKey(Number(value ?? 0))"
          />
          <button
            type="button"
            class="btn btn-secondary btn-icon"
            :disabled="refreshing"
            :title="t('common.refresh')"
            @click="refreshAll"
          >
            <Icon name="refresh" size="md" :class="refreshing ? 'animate-spin' : ''" />
          </button>
        </div>
      </header>

      <!-- 无可用 Key：明确引导，不留白也不报错 -->
      <div
        v-if="!loadingKeys && videoApiKeys.length === 0"
        class="rounded-xl border border-dashed border-gray-300 p-8 text-center dark:border-gray-600"
      >
        <Icon name="key" size="lg" class="mx-auto text-gray-400" />
        <p class="mt-2 text-sm font-medium text-gray-700 dark:text-gray-200">
          {{ t('videoPlayground.noKey.title') }}
        </p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('videoPlayground.noKey.description') }}</p>
        <RouterLink to="/keys" class="btn btn-primary mt-3 inline-flex">
          {{ t('videoPlayground.noKey.action') }}
        </RouterLink>
      </div>

      <template v-else>
        <div ref="streamRef" class="flex-1 space-y-4 overflow-y-auto pb-2">
          <!-- 历史在最上方：往上翻是"更早"，与对话流的方向一致 -->
          <p
            v-if="loadingHistory && !historyLoaded"
            class="flex items-center justify-center gap-1 text-xs text-gray-400 dark:text-gray-500"
          >
            <Icon name="refresh" size="sm" class="animate-spin" />
            {{ t('videoPlayground.history.loading') }}
          </p>
          <div v-else-if="historyHasMore" class="flex justify-center">
            <button
              type="button"
              class="btn btn-ghost btn-sm inline-flex items-center gap-1"
              :disabled="loadingHistory || !selectedApiKey"
              @click="loadMoreHistory"
            >
              <Icon name="refresh" size="sm" :class="loadingHistory ? 'animate-spin' : ''" />
              {{ loadingHistory ? t('videoPlayground.history.loading') : t('videoPlayground.history.loadMore') }}
            </button>
          </div>
          <p
            v-else-if="historyLoaded && cards.length > 0"
            class="text-center text-xs text-gray-400 dark:text-gray-500"
          >
            {{ t('videoPlayground.history.end') }}
          </p>
          <div
            v-if="cards.length === 0 && !loadingHistory"
            class="rounded-xl border border-dashed border-gray-300 p-10 text-center text-sm text-gray-500 dark:border-gray-600 dark:text-gray-400"
          >
            {{ t('videoPlayground.empty') }}
          </div>
          <VideoResultCard
            v-for="card in cards"
            :key="card.localId"
            :card="card"
            :now="now"
            :api-key="selectedApiKey?.key || ''"
            @refresh="applyRefreshedTask"
          />
        </div>

        <VideoComposer
          v-model:prompt="form.prompt"
          v-model:open="settingsOpen"
          :submitting="submitting"
          :disabled="!selectedApiKey"
          :placeholder="t('videoPlayground.composer.placeholder')"
          :hint="composerHint"
          @submit="submit"
        >
          <template #settings>
            <VideoSettingsPanel
              :models="models"
              :model-id="form.model"
              :seconds="form.seconds"
              :seconds-options="secondsOptions"
              :seconds-range="secondsRange"
              :framing="form.framing"
              :framing-controls="framingControls"
              :references="form.references"
              :reference-slots="referenceSlots"
              :loading="loadingModels"
              @update:modelId="selectModel"
              @update:seconds="(value) => (form.seconds = value)"
              @update:framing="applyFraming"
              @update:reference="applyReference"
            />
          </template>
        </VideoComposer>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select from '@/components/common/Select.vue'
import { Icon } from '@/components/icons'
import VideoComposer from '@/components/video/VideoComposer.vue'
import VideoResultCard from '@/components/video/VideoResultCard.vue'
import VideoSettingsPanel from '@/components/video/VideoSettingsPanel.vue'
import { mergeVideoHistoryCards } from '@/components/video/history'
import type { VideoPlaygroundCard } from '@/components/video/types'
import { keysAPI } from '@/api/keys'
import { createVideo, getVideoTask, listVideoTasks, type VideoTask } from '@/api/video'
import { keyAllowsVideoPlayground } from '@/composables/useVideoPlaygroundAccess'
import { useVideoPlaygroundModels, type VideoFormIssue } from '@/composables/useVideoPlaygroundModels'
import { rememberVideoPrompt } from '@/utils/videoPromptMemory'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()

const apiKeys = ref<ApiKey[]>([])
const loadingKeys = ref(false)
const selectedApiKeyId = ref(0)
const cards = ref<VideoPlaygroundCard[]>([])
const submitting = ref(false)
const refreshing = ref(false)
/** 生成参数默认展开：模型、时长、画幅都在里面，藏起来等于让用户先猜一遍默认值。 */
const settingsOpen = ref(true)
const streamRef = ref<HTMLElement | null>(null)
/** 秒级"现在"，供卡片计算进行中任务的耗时；一个定时器供全部卡片共用。 */
const now = ref(Math.floor(Date.now() / 1000))

/**
 * 首屏只回放最近 5 条：这是一条对话流，输入框在最下方，历史铺太长会把它顶出视野；
 * 真要往回翻的人会点"加载更多"，那时候一次给 10 条才不至于点个没完。
 */
const HISTORY_INITIAL_LIMIT = 5
const HISTORY_PAGE_LIMIT = 10

const loadingHistory = ref(false)
const historyLoaded = ref(false)
/** 下一页游标。后端只在 has_more 为真时才给值，空串即"没有更早的了"。 */
const historyCursor = ref('')
const historyHasMore = ref(false)

/** 轮询间隔下限 10s；命中 Retry-After 时按其指示退避。 */
const POLL_INTERVAL_MS = 10_000
let pollTimer: ReturnType<typeof setTimeout> | null = null
/** 轮询是否正在跑。定时器句柄不能兼任这面闸：执行期间它已经被置空。 */
let polling = false
let clockTimer: ReturnType<typeof setInterval> | null = null
let nextPollAt = 0

const videoApiKeys = computed(() => apiKeys.value.filter(keyAllowsVideoPlayground))
const selectedApiKey = computed(
  () => videoApiKeys.value.find((key) => key.id === selectedApiKeyId.value) || null,
)
const apiKeyOptions = computed(() =>
  videoApiKeys.value.map((key) => ({
    value: key.id,
    label: key.group?.name ? `${key.name} · ${key.group.name}` : key.name,
  })),
)

const {
  models,
  loading: loadingModels,
  loadError: modelsError,
  form,
  secondsOptions,
  secondsRange,
  framingControls,
  referenceSlots,
  loadModels,
  selectModel,
  pruneUnavailableSelections,
  validate,
  buildRequest,
} = useVideoPlaygroundModels(() => selectedApiKey.value?.key || '')

const composerHint = computed(() => {
  if (modelsError.value) {
    return videoErrorMessage(modelsError.value, t('videoPlayground.errors.loadModelsFailed'))
  }
  if (!loadingModels.value && models.value.length === 0 && selectedApiKey.value) {
    return t('videoPlayground.errors.noModels')
  }
  return ''
})

/**
 * 把网关错误翻译成人话。code 在网关侧已被小写化，直接按小写比对。
 * video_pricing_missing 必须有专属文案——它是"能力目录直出、不做定价探测"这个
 * 取舍的直接后果，落进通用兜底会让用户完全无从下手。
 */
function videoErrorMessage(error: any, fallback: string): string {
  const code = String(error?.code || '').trim().toLowerCase()
  const key = `videoPlayground.errors.${code}`
  const translated = t(key)
  if (translated !== key) return translated
  const message = String(error?.message || '').trim()
  return message || fallback
}

/**
 * 换密钥只换模型目录，不清空对话——任务按 user 维度归属，同一用户的任何密钥都能继续
 * 轮询和取回本页已经发起的那些任务。
 */
function selectApiKey(id: number): void {
  if (id === selectedApiKeyId.value) return
  selectedApiKeyId.value = id
  // 模型目录不用在这里重取：composable 自己 watch 着密钥，改完这一行它就会去取。
  // 两边都调的话每次换密钥都发两次 GET /v1/videos/models（结果正确，纯属白跑）。
  // 历史按 user 维度归属，换密钥不必重拉；只补上"挂载时还没有密钥可用"那种情况。
  if (!historyLoaded.value) void loadHistory(true)
}

function applyFraming(name: string, value: string): void {
  if (value) form.value.framing[name] = value
  else delete form.value.framing[name]
  reportPrunedReferences(pruneUnavailableSelections())
}

function applyReference(field: string, values: string[]): void {
  form.value.references[field] = values
  reportPrunedReferences(pruneUnavailableSelections())
}

/**
 * 刚才这一改把哪几个槽位里手敲的 URL 清掉了，说一句。
 *
 * 典型场景是删掉最后一张参考图——参考音频的前置随之不成立，已经填好的音频地址被
 * 一并清空。不说的话界面上没有任何交代，用户只会觉得输入框自己把内容吃了。
 */
function reportPrunedReferences(fields: string[]): void {
  if (fields.length === 0) return
  appStore.showError(
    t('videoPlayground.errors.referencesCleared', { fields: fields.map(fieldLabel).join(' / ') }),
  )
}

/** 目录里的字段名对用户没有意义，有对应文案就用文案，没有就原样显示。 */
function fieldLabel(field: string): string {
  const key = `videoPlayground.fields.${field}`
  const label = t(key)
  return label === key ? field : label
}

/**
 * 把一条校验问题翻译成人话。
 *
 * detail 的含义随 code 而变：互斥/前置两条装的是**目录里的字段名**，必须先过
 * fieldLabel 才能给用户看，否则提示里会冒出 first_image_url 这种东西；其余几条装的
 * 是数量、字节数、协议名，原样插值即可。
 */
const FIELD_NAME_DETAIL_ISSUES = new Set(['reference_conflict', 'reference_requires_any'])

function issueMessage(issue: VideoFormIssue): string {
  const detail = issue.detail ?? ''
  return t(`videoPlayground.issues.${issue.code}`, {
    field: issue.field ? fieldLabel(issue.field) : '',
    detail: FIELD_NAME_DETAIL_ISSUES.has(issue.code)
      ? String(detail)
          .split(',')
          .map((name) => fieldLabel(name.trim()))
          .filter(Boolean)
          .join(' / ')
      : detail,
  })
}

async function loadApiKeys(): Promise<void> {
  loadingKeys.value = true
  try {
    const response = await keysAPI.list(1, 100, {
      status: 'active',
      sort_by: 'created_at',
      sort_order: 'desc',
    })
    apiKeys.value = response.items || []
    if (!selectedApiKey.value && videoApiKeys.value.length > 0) {
      selectedApiKeyId.value = videoApiKeys.value[0].id
    }
  } catch (error) {
    appStore.showError(videoErrorMessage(error, t('videoPlayground.errors.loadKeysFailed')))
  } finally {
    loadingKeys.value = false
  }
}

async function refreshAll(): Promise<void> {
  refreshing.value = true
  try {
    await loadApiKeys()
    await loadModels()
    // 只重取最新的一页：游标保持不动，已经翻出来的历史不会被清掉。代价是别处新建的
    // 任务若超过这一页，中间那些要等下次翻页才补得上——刷新按钮不值得为此重置全部状态。
    await loadHistory(true)
    schedulePoll(0)
  } finally {
    refreshing.value = false
  }
}

async function submit(): Promise<void> {
  const key = selectedApiKey.value
  if (!key) {
    appStore.showError(t('videoPlayground.errors.selectApiKey'))
    return
  }
  if (submitting.value) return
  const issues = validate()
  if (issues.length > 0) {
    appStore.showError(issueMessage(issues[0]))
    return
  }

  const payload = buildRequest()
  // 幂等键每次提交生成一次：它既是创建开关的准入条件（不带该头直接 403），
  // 也是连点不重复扣费的唯一保证。
  const idempotencyKey =
    globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
  cards.value = [
    ...cards.value,
    {
      localId: idempotencyKey,
      prompt: payload.prompt,
      model: payload.model,
      status: 'queued',
      submittedAt: Math.floor(Date.now() / 1000),
      task: null,
    },
  ]
  // 取回响应式代理再改：直接改推进去的那个裸对象不会触发更新。
  const card = cards.value[cards.value.length - 1]
  void scrollToLatest()

  submitting.value = true
  try {
    const task = await createVideo(key.key, payload, idempotencyKey)
    rememberVideoPrompt(task.id, payload.prompt)
    card.task = task
    card.status = task.status
    card.model = task.model || card.model
    form.value.prompt = ''
    schedulePoll(0)
  } catch (error: any) {
    card.status = 'failed'
    card.submitError = {
      code: String(error?.code || ''),
      message: videoErrorMessage(error, t('videoPlayground.errors.submitFailed')),
    }
  } finally {
    submitting.value = false
  }
}

/**
 * 拉一页历史。newest 为真取最新一页（挂载与刷新走这条），为假顺着游标往更早翻。
 */
async function loadHistory(newest: boolean): Promise<void> {
  const key = selectedApiKey.value
  if (!key || loadingHistory.value) return
  if (!newest && !historyCursor.value) return
  const first = !historyLoaded.value
  loadingHistory.value = true
  // 往上插内容会把正在看的位置整个推下去。记住"距底部多远"，插完再按这个距离回填，
  // 视口就停在原处；首屏则相反，要落到最下方的最新一条。
  const element = streamRef.value
  const distanceToBottom = element ? element.scrollHeight - element.scrollTop : 0
  try {
    const response = await listVideoTasks(key.key, {
      limit: newest ? HISTORY_INITIAL_LIMIT : HISTORY_PAGE_LIMIT,
      after: newest ? '' : historyCursor.value,
    })
    cards.value = mergeVideoHistoryCards(cards.value, response.data || [])
    // 刷新（已翻过页的 newest 请求）不许动游标：把它退回第一页会让"加载更多"重走
    // 已经展示过的那几页，去重之后一条不增，用户看到的就是"这个按钮坏了"。
    if (first || !newest) {
      historyCursor.value = response.has_more ? response.after || '' : ''
      historyHasMore.value = !!historyCursor.value
    }
    historyLoaded.value = true
    await nextTick()
    if (first) void scrollToLatest()
    else if (element) element.scrollTop = element.scrollHeight - distanceToBottom
    if (cards.value.some(isPending)) schedulePoll(0)
  } catch (error) {
    // 首屏失败时把按钮留着，用户点一下就是重试；不要静默吞掉一整段历史。
    if (first) historyHasMore.value = true
    appStore.showError(videoErrorMessage(error, t('videoPlayground.errors.loadHistoryFailed')))
  } finally {
    loadingHistory.value = false
  }
}

function loadMoreHistory(): void {
  void loadHistory(!historyLoaded.value)
}

function isPending(card: VideoPlaygroundCard): boolean {
  return !!card.task && (card.status === 'queued' || card.status === 'in_progress')
}

function schedulePoll(delayMs: number): void {
  const at = Date.now() + delayMs
  if (pollTimer && nextPollAt <= at) return
  if (pollTimer) clearTimeout(pollTimer)
  nextPollAt = at
  pollTimer = setTimeout(() => {
    pollTimer = null
    void pollPending()
  }, delayMs)
}

async function pollPending(): Promise<void> {
  const key = selectedApiKey.value
  const pending = cards.value.filter(isPending)
  // 执行期间 pollTimer 是空的，所以此时任何一次 schedulePoll(0)（提交新任务、翻历史）
  // 都会立刻再起一轮，两轮并排对着同一批任务发 GET。结果不会串——两轮写的是同一份
  // 快照——但请求白翻一倍，正好撞在内容代理的并发闸上。
  if (polling || !key || pending.length === 0) return
  polling = true

  let backoffMs = POLL_INTERVAL_MS
  try {
    for (const card of pending) {
      if (!card.task) continue
      try {
        const task = await getVideoTask(key.key, card.task.id)
        card.task = task
        card.status = task.status
        card.model = task.model || card.model
      } catch (error: any) {
        const status = Number(error?.status || 0)
        const retryAfter = Number(error?.retryAfter || 0)
        if (retryAfter > 0) backoffMs = Math.max(backoffMs, retryAfter * 1000)
        // 429 是限流闸，不是任务失败——继续等，不要把卡片打成失败。
        if (status === 429) {
          backoffMs = Math.max(backoffMs, POLL_INTERVAL_MS * 2)
          continue
        }
        if (status === 404) {
          card.status = 'failed'
          card.submitError = {
            code: 'video_task_not_found',
            message: videoErrorMessage(error, t('videoPlayground.errors.taskGone')),
          }
        }
      }
    }
  } finally {
    // 必须走 finally：这面闸一旦漏关，整页轮询就永久停摆，而卡片会一直停在"生成中"。
    polling = false
  }
  if (cards.value.some(isPending)) schedulePoll(backoffMs)
}

/**
 * 卡片自己重新拉回来的任务快照。
 *
 * 直链失效时卡片会去换一条新的，拿到的新快照必须写回这里——否则父级手上仍是那条
 * 失效的 url，下一次渲染又把它塞回去，重试等于白做。只更新 task 与 status，
 * prompt 之类的本地字段不动：那是本次会话的东西，服务端快照里没有。
 */
function applyRefreshedTask(task: VideoTask): void {
  const card = cards.value.find((item) => item.task?.id === task.id)
  if (!card) return
  card.task = task
  card.status = task.status
  card.model = task.model || card.model
}

async function scrollToLatest(): Promise<void> {
  await nextTick()
  const element = streamRef.value
  if (element) element.scrollTop = element.scrollHeight
}

onMounted(async () => {
  clockTimer = setInterval(() => {
    now.value = Math.floor(Date.now() / 1000)
  }, 1000)
  await loadApiKeys()
  // loadApiKeys 选中第一个密钥时 composable 的 watch 已经去取模型目录了，这里只补历史。
  if (selectedApiKey.value) await loadHistory(true)
})

onBeforeUnmount(() => {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = null
  if (clockTimer) clearInterval(clockTimer)
  clockTimer = null
})
</script>
