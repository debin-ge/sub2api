<template>
  <div class="space-y-2">
    <!-- 用户这一侧：提示词靠右，像聊天里自己发出的那条消息 -->
    <div class="flex justify-end">
      <div
        class="max-w-[85%] rounded-2xl rounded-br-sm bg-blue-600 px-4 py-2.5 text-sm text-white shadow-sm"
      >
        <p v-if="card.prompt" class="whitespace-pre-wrap break-words">{{ card.prompt }}</p>
        <!-- 本次改动之前创建的任务没留提示词。说明清楚，不要拿别的字段冒充原文。 -->
        <p v-else class="italic text-blue-200" :title="t('videoPlayground.card.promptUnavailableHint')">
          {{ t('videoPlayground.card.promptUnavailable') }}
        </p>
        <p
          v-if="requestSummary.length > 0"
          class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-blue-100"
        >
          <span v-for="item in requestSummary" :key="item">{{ item }}</span>
        </p>
      </div>
    </div>

    <!-- 生成这一侧：视频/等待/失败靠左 -->
    <div class="flex justify-start">
      <article
        class="w-full max-w-[85%] rounded-2xl rounded-bl-sm border zt-border-c bg-white p-3 shadow-sm dark:border-gray-700 dark:bg-gray-800"
      >
        <header class="mb-2 flex items-center justify-between gap-2">
          <span :class="['rounded-full px-2.5 py-1 text-xs font-medium', statusClass]">
            {{ statusLabel }}
          </span>
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs zt-ink-3">
            <span class="inline-flex items-center gap-1">
              <Icon name="clock" size="sm" />
              {{ elapsedLabel }}
            </span>
            <!-- 费用缺席意味着「尚不可知」（未结算，或分组未开放披露），不是零。 -->
            <span v-if="costLabel" class="inline-flex items-center gap-1">
              <Icon name="dollar" size="sm" />
              {{ costLabel }}
            </span>
            <span v-else-if="!isFailure" class="inline-flex items-center gap-1 opacity-70">
              <Icon name="dollar" size="sm" />
              {{ t('videoPlayground.card.costPending') }}
            </span>
          </div>
        </header>

        <!--
          没点播放之前一个字节都不取。
          内容代理有并发闸（每用户 4 / 每账号 16），一屏历史记录若各自去拉，仅仅是打开
          页面就能把闸撞满：先撞出 429「通道繁忙」，重试又落进更难解释的失败。一次只放
          一个、由用户点出来，这个闸基本就碰不到了。
        -->
        <button
          v-if="showPoster"
          type="button"
          class="group relative flex h-44 w-full items-center justify-center rounded-lg bg-gray-900 transition hover:bg-black"
          :aria-label="t('videoPlayground.card.play')"
          @click="activate"
        >
          <span
            class="flex h-16 w-16 items-center justify-center rounded-full bg-white/90 shadow-lg transition group-hover:scale-105 group-hover:bg-white"
          >
            <Icon name="play" size="lg" class="ml-0.5 zt-ink" />
          </span>
          <span class="absolute bottom-2 text-xs zt-ink-3">{{ t('videoPlayground.card.play') }}</span>
        </button>
        <div v-else-if="playableUrl">
          <video
            :key="playerNonce"
            :src="playableUrl"
            controls
            autoplay
            playsinline
            preload="metadata"
            class="max-h-96 w-full rounded-lg bg-black"
            @error="onPlaybackError"
            @loadeddata="notice = ''"
          />
          <p
            v-if="notice"
            class="mt-1 flex flex-wrap items-center gap-2 text-xs zt-warn-text"
          >
            {{ notice }}
            <button type="button" class="underline" :disabled="preparing" @click="retry">
              {{ t('videoPlayground.card.retryPlayback') }}
            </button>
          </p>
        </div>
        <!--
          内容已过期：上游只在完成后的一段有限窗口内保留成片（各家保留期不同）。窗口一过，
          直链和取回一起断，播放、重试、复制链接全是死路——所以这里把它们一并撤掉，只说
          清楚发生了什么，而不是摆一排点了也没用的按钮。
        -->
        <div
          v-else-if="expired"
          class="rounded-lg border zt-border-c zt-surface-2 p-3 text-sm dark:border-gray-700 dark:bg-gray-900/40"
        >
          <p class="flex items-center gap-1.5 font-medium zt-ink-2">
            <Icon name="clock" size="sm" />
            {{ t('videoPlayground.card.contentExpired') }}
          </p>
          <p class="mt-1 text-xs zt-ink-3">
            {{ t('videoPlayground.card.contentExpiredHint') }}
          </p>
        </div>
        <div
          v-else-if="preparing"
          class="flex items-center gap-2 rounded-lg border border-dashed zt-border-c2 p-6 text-sm zt-ink-3 dark:border-gray-600"
        >
          <Icon name="refresh" size="md" class="animate-spin" />
          <span>{{ t('videoPlayground.card.preparingPlayback') }}</span>
        </div>
        <!-- 失败 / 取消 / 过期：都已结束且没有成片，但原因不同，兜底文案按状态分开说。 -->
        <div
          v-else-if="isFailure"
          class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm dark:border-red-900/50 dark:bg-red-900/20"
        >
          <p class="font-medium text-red-700 dark:text-red-300">{{ errorMessage }}</p>
          <p v-if="errorCode" class="mt-1 font-mono text-xs text-red-500 dark:text-red-400">{{ errorCode }}</p>
        </div>
        <!--
          已完成却拿不到可播放内容：别再转那个「生成中」的钟——任务早就结束了，
          转下去只会让用户一直等一个不会到来的结果。
        -->
        <div
          v-else-if="isDone"
          class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm dark:border-amber-900/50 dark:bg-amber-900/20"
        >
          <p class="text-amber-700 dark:text-amber-300">
            {{ notice || t('videoPlayground.card.contentUnavailable') }}
          </p>
          <button
            v-if="canRefresh"
            type="button"
            class="btn btn-ghost btn-sm mt-2"
            :disabled="preparing"
            @click="retry"
          >
            {{ t('videoPlayground.card.retryPlayback') }}
          </button>
        </div>
        <!--
          生成中：视频任务本来就是分钟级的，一个不动的钟会被读成"卡住了"。这里给出
          三样在等待期唯一真实可信的信息——心跳、已等多久、以及一句随时长推进的说明，
          外加一条漂动的进度条（Ark 任务对象不回报进度，所以永远是不确定态）。
        -->
        <div
          v-else
          class="space-y-3 rounded-lg border border-dashed zt-border-c2 p-5 dark:border-gray-600"
        >
          <div class="flex items-center gap-2 text-sm zt-ink-2">
            <span class="relative flex h-2.5 w-2.5 shrink-0">
              <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-blue-400 opacity-75" />
              <span class="relative inline-flex h-2.5 w-2.5 rounded-full bg-blue-500" />
            </span>
            <span class="font-medium">{{ statusLabel }}</span>
            <span class="ml-auto shrink-0 tabular-nums text-xs zt-ink-3">
              {{ elapsedLabel }}
            </span>
          </div>
          <div class="h-1.5 w-full overflow-hidden rounded-full zt-surface-3 dark:bg-gray-700">
            <div class="progress-drift h-full w-1/3 rounded-full bg-blue-500" />
          </div>
          <p class="text-xs zt-ink-3">{{ waitingHint }}</p>
        </div>

        <!--
          只有完成的任务才有东西可拿。进行中就摆出下载/复制，点下去要么是空、要么是
          一段还没生成完的字节，用户会当成功能坏了。
        -->
        <!-- 复制的是 content.video_url 那条带签名的公开直链：不需要 Bearer 头，贴到别处也能下。
             它会过期，过期后重新 GET 一次任务就有新的。 -->
        <footer
          v-if="isDone && !expired && directPlayable"
          class="mt-2 flex flex-wrap items-center justify-end gap-2"
        >
          <button
            type="button"
            class="btn btn-ghost btn-sm inline-flex items-center gap-1"
            :title="directUrl"
            @click="copyUrl"
          >
            <Icon name="link" size="sm" />
            {{ t('videoPlayground.card.copyUrl') }}
          </button>
        </footer>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon } from '@/components/icons'
import { fetchVideoContent, getVideoTask, isVideoTaskFailure, type VideoTask } from '@/api/video'
import { VIDEO_SMART_DURATION } from '@/composables/useVideoPlaygroundModels'
import { useClipboard } from '@/composables/useClipboard'
import { classifyContentError, isPlayableBlob, playableBlob, type VideoContentFailure } from './content'
import type { VideoPlaygroundCard } from './types'

const props = defineProps<{ card: VideoPlaygroundCard; now: number; apiKey: string }>()
/**
 * 直连播不出来时本组件会重新拉一次任务拿新的签名直链，拿到的新快照必须回流给父级——
 * 否则父级手上仍是那条失效的链接，下一次渲染又把它塞回来，重试白做。
 */
const emit = defineEmits<{ (event: 'refresh', task: VideoTask): void }>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const playerNonce = ref(0)
const notice = ref('')
const preparing = ref(false)
const blobUrl = ref('')
/** 用户点过播放没有。在此之前本卡片不碰任何内容请求。 */
const activated = ref(false)
/**
 * 播放器是否已经切到 blob。这与"blob 是否已取回"是两件事：下载会取回内容但不该动播放器。
 * 只有直连确实播不出来时才置位，因为 blob 播放是把整段视频钉在内存里、且失去 Range
 * 边下边播与拖动进度条的降级方案。
 */
const blobPlayback = ref(false)
/** 已取回的内容本体。重试时直接复用，避免为同一个视频拉两遍。 */
const blobData = ref<Blob | null>(null)
/** 服务端明确说过内容已失效（410 / video_content_expired）。 */
const contentExpired = ref(false)
/** 直连失败后只允许刷新一次任务，免得一条坏链把播放器拖进无限重试。 */
let directRefreshed = false

/** succeeded 即「已结算」：只有它有成片。 */
const isDone = computed(() => props.card.status === 'succeeded')
const isFailure = computed(() => isVideoTaskFailure(props.card.status))

/**
 * content.video_url 是带签名的本地代理直链，仅 succeeded 且内容代理开启时出现。
 * 没有它就没有任何取内容的路——签名就是唯一的授权。
 */
const directUrl = computed(() => props.card.task?.content?.video_url || '')
/** 只有绝对地址才值得喂给 <video>；相对地址说明网关没拿到公开 host，直连必失败。 */
const directPlayable = computed(() => /^https?:\/\//i.test(directUrl.value))
/**
 * 直连优先，blob 只是降级。一旦反过来（blobUrl 一有值就顶掉直连），点一次"下载"就会把
 * 正在播放的播放器整个换源——用户什么都没对播放器做，画面却重来一遍。
 */
const playableUrl = computed(() => {
  // 没点播放就没有源：<video> 一旦拿到 src 就会去预取，懒加载的意义全在这一句。
  if (!activated.value || expired.value) return ''
  if (directPlayable.value && !blobPlayback.value) return directUrl.value
  return blobUrl.value || (directPlayable.value ? directUrl.value : '')
})

/**
 * 内容已过期：只认服务端明说的 410 / video_content_expired。签名直链自身的 exp 到点
 * 不算过期——重新 GET 一次任务就有新链接，走的是「换链接」那条路，不是这里。
 *
 * 两条前提：
 *   · 只对完成的任务成立。失败的任务本来就没有内容，把它说成"已过期"等于用一句
 *     无关的解释盖掉真正的失败原因。
 *   · 手上已经有副本就不算过期——链接死了不影响这一份字节，仍然可以播。
 */
const expired = computed(() => isDone.value && !blobData.value && contentExpired.value)
/** 能不能重新拉一次任务（拿新链接）。取内容本体不需要密钥，但查任务需要。 */
const canRefresh = computed(() => !!props.apiKey && !!props.card.task?.id)
/** 海报（那个大播放按钮）：确实有东西可放、还没被点开，才摆出来。 */
const showPoster = computed(
  () => !activated.value && isDone.value && !expired.value && (directPlayable.value || canRefresh.value),
)

const statusLabel = computed(() => {
  const key = `videoPlayground.status.${props.card.status}`
  const label = t(key)
  return label === key ? props.card.status : label
})

const statusClass = computed(() => {
  switch (props.card.status) {
    case 'succeeded':
      return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
    case 'failed':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'cancelled':
      return 'bg-gray-200 text-gray-700 dark:bg-gray-700 dark:text-gray-200'
    case 'expired':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'running':
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
    default:
      return 'zt-surface-3 zt-ink-2 dark:bg-gray-700'
  }
})

const requestSummary = computed(() => {
  const items: string[] = []
  if (props.card.model) items.push(props.card.model)
  const task = props.card.task
  const duration = task?.duration
  if (typeof duration === 'number') {
    items.push(
      duration === VIDEO_SMART_DURATION
        ? t('videoPlayground.card.smartDuration')
        : t('videoPlayground.card.duration', { duration }),
    )
  }
  if (task?.resolution) items.push(task.resolution)
  if (task?.ratio) items.push(task.ratio)
  return items
})

const errorCode = computed(() => props.card.task?.error?.code || props.card.submitError?.code || '')
const errorMessage = computed(() => {
  const message = props.card.task?.error?.message || props.card.submitError?.message
  if (message) return message
  switch (props.card.status) {
    case 'cancelled':
      return t('videoPlayground.card.cancelled')
    case 'expired':
      return t('videoPlayground.card.expired')
    default:
      return t('videoPlayground.card.failed')
  }
})

/**
 * 耗时。Ark 任务对象没有 completed_at，终态用 updated_at 收口——它在任务完成或结算
 * 时被推进，与完成时刻相差不过几秒。
 */
const elapsedSeconds = computed(() => {
  const start = props.card.task?.created_at || props.card.submittedAt
  const terminal = isDone.value || isFailure.value
  const stop = terminal ? props.card.task?.updated_at || start : props.now
  return Math.max(0, Math.round(stop - start))
})

const elapsedLabel = computed(() => {
  const total = elapsedSeconds.value
  if (total < 60) return t('videoPlayground.card.elapsedSeconds', { seconds: total })
  const minutes = Math.floor(total / 60)
  return t('videoPlayground.card.elapsedMinutes', { minutes, seconds: total % 60 })
})

/**
 * 等待期唯一确凿的信息是"已经等了多久"，分档说一句人话即可。不要把它写成倒计时或
 * 预计剩余时间——那两个数我们都没有，编出来只会在超时后显得更不可信。
 */
const waitingHint = computed(() => {
  const total = elapsedSeconds.value
  if (total < 15) return t('videoPlayground.card.waitingQueued')
  if (total < 60) return t('videoPlayground.card.waitingRendering')
  if (total < 180) return t('videoPlayground.card.waitingLong')
  return t('videoPlayground.card.waitingVeryLong')
})

const costLabel = computed(() => {
  const cost = props.card.task?.actual_cost
  if (typeof cost !== 'number') return ''
  const amount = cost.toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
  // 币种可能为空（历史行），此时只显示数字，不要编造 USD。
  const currency = props.card.task?.currency
  return currency ? `${amount} ${currency}` : amount
})

/**
 * 把取内容的失败翻译成一句人话。
 *
 * 「稍后重试」（并发闸）、「已经没了」（链接过期）、「拿回来的不是视频」这三种，用户
 * 要做的事完全不同，必须分开说。只有真的归不了类时才退回上游原文。
 */
function applyContentFailure(kind: VideoContentFailure, error?: unknown): void {
  if (kind === 'expired') contentExpired.value = true
  notice.value =
    kind === 'busy'
      ? t('videoPlayground.card.playbackBusy')
      : kind === 'expired'
        ? t('videoPlayground.card.contentExpired')
        : kind === 'notVideo'
          ? t('videoPlayground.card.contentNotVideo')
          : String((error as { message?: unknown } | undefined)?.message || '').trim() ||
            t('videoPlayground.card.playbackFailed')
}

function releaseBlob(): void {
  releaseBlobUrlOnly()
  blobPlayback.value = false
  blobData.value = null
}

function releaseBlobUrlOnly(): void {
  if (blobUrl.value) URL.revokeObjectURL(blobUrl.value)
  blobUrl.value = ''
}

/** 把已取回的内容交给播放器。只有这一处会改变播放源。 */
function promoteBlob(blob: Blob): void {
  releaseBlobUrlOnly()
  blobUrl.value = URL.createObjectURL(playableBlob(blob))
  blobPlayback.value = true
  notice.value = ''
}

/**
 * 按签名直链取回内容本体并交给播放器。只在直连不可用或已经失败时才走这条路。
 * 没有直链就没有可取的地址——调用方应先 refreshTask 拿一条。
 */
async function loadContent(): Promise<Blob | null> {
  const cached = blobData.value
  if (cached) {
    promoteBlob(cached)
    return cached
  }
  const url = directUrl.value
  if (!url) return null
  preparing.value = true
  try {
    const blob = await fetchVideoContent(url)
    // 代理认不出内容路径时会把请求放行到内嵌 SPA，于是「成功」地拿回一份 index.html。
    // 贴上 video/mp4 交给播放器只换来一句媒体错误 4，不如直说这不是视频。
    if (!isPlayableBlob(blob)) {
      applyContentFailure('notVideo')
      return null
    }
    blobData.value = blob
    promoteBlob(blob)
    return blob
  } catch (error) {
    applyContentFailure(classifyContentError(error), error)
    return null
  } finally {
    preparing.value = false
  }
}

/**
 * 重新拉一次任务，拿当前有效的那条签名直链。
 *
 * 直链的签名带过期时间，到点后代理回 404；重新取一次任务就能拿到新的 content.video_url，
 * 比把整段视频读进内存便宜得多。
 */
async function refreshTask(): Promise<string> {
  const taskId = props.card.task?.id
  if (!taskId || !props.apiKey) return ''
  try {
    const task = await getVideoTask(props.apiKey, taskId)
    emit('refresh', task)
    return task.content?.video_url || ''
  } catch (error) {
    if (classifyContentError(error) === 'expired') contentExpired.value = true
    return ''
  }
}

/** 直连播不出来时的挽救顺序：先换一条新直链，不行才退到 blob。 */
async function recoverDirectPlayback(): Promise<void> {
  if (!directRefreshed) {
    directRefreshed = true
    // emit 之后 directUrl 已经指向新值，所以要跟刷新前那条比。
    const previous = directUrl.value
    const fresh = await refreshTask()
    if (fresh && fresh !== previous) {
      playerNonce.value += 1
      return
    }
  }
  // blob 要把整段视频读进内存并失去 Range，所以放在最后。
  await loadContent()
}

/**
 * 用户点了播放。有直链就只是把 <video> 放出来，让它自己去拉（能走 Range、边下边播）；
 * 没有直链就先重新取一次任务——也许只是这份快照太旧——拿到链接再取回。
 */
function activate(): void {
  activated.value = true
  if (!directPlayable.value) void retry()
}

function onPlaybackError(event: Event): void {
  const code = (event.target as HTMLVideoElement | null)?.error?.code ?? 0
  // MEDIA_ERR_ABORTED：换源或卸载元素造成的中断，是我们自己干的，不是播放失败。
  if (code === 1) return
  // 已经在放 blob 还报错，那就是真播不了，再抓一次也是同一份数据。
  if (blobPlayback.value) {
    notice.value = code
      ? t('videoPlayground.card.playbackFailedCode', { code })
      : t('videoPlayground.card.playbackFailed')
    return
  }
  void recoverDirectPlayback()
}

async function retry(): Promise<void> {
  notice.value = ''
  // 重试是用户明说"再试一次"，之前判过的死刑一并作废：链接可能已经换新的了。
  contentExpired.value = false
  directRefreshed = false
  // 只丢缓存不动播放源：清空 blobUrl 会让播放器先弹回直连、放出首帧，再被换成 blob，
  // 白闪一次。等新内容到手再一次性换。
  blobData.value = null
  // 没有直链时先换一份新快照：直链只在 succeeded 后才出现，手上这份可能还是提交时的占位。
  if (!directPlayable.value) {
    preparing.value = true
    try {
      await refreshTask()
    } finally {
      preparing.value = false
    }
    if (!directPlayable.value) return
  }
  const blob = await loadContent()
  if (blob) playerNonce.value += 1
}

function copyUrl(): void {
  if (!directUrl.value) return
  void copyToClipboard(directUrl.value, t('videoPlayground.card.copiedUrl'))
}

onBeforeUnmount(releaseBlob)
</script>

<style scoped>
/* 不确定态进度条：来回漂，只表达"还在动"，不表达进度。 */
.progress-drift {
  animation: progress-drift 1.6s ease-in-out infinite;
}

@keyframes progress-drift {
  0% {
    transform: translateX(-100%);
  }
  100% {
    transform: translateX(300%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .progress-drift {
    animation: none;
    width: 100%;
    opacity: 0.4;
  }
}
</style>
