<template>
  <div class="space-y-4">
    <!-- 模型 -->
    <div>
      <label class="mb-1 block text-xs font-medium zt-ink-2">
        {{ t('videoPlayground.settings.model') }}
      </label>
      <Select
        :model-value="modelId"
        :options="modelOptions"
        :disabled="loading"
        :loading="loading"
        :placeholder="t('videoPlayground.settings.modelPlaceholder')"
        :empty-text="t('videoPlayground.settings.noModels')"
        @update:model-value="(value) => emit('update:modelId', String(value ?? ''))"
      />
    </div>

    <!-- 时长有三种形态，全部由目录决定：
         · options 非空 → 只能挑这几档（Seedance 的 5/10，或运营在价目里收窄过的档位）；
         · range 非空 → 区间内任意整数秒，给滑杆；
         · 两者都没有 → 自由输入。都没有并不代表不能选时长——按秒计价的价目本就不必逐条
           列档位。整块藏掉只会让用户被迫接受上游默认值。
         allowSmart 时再加一个「智能时长」档（duration = -1），由模型按内容决定长度。 -->
    <div v-if="hasModel">
      <label class="mb-1 flex items-center justify-between gap-2 text-xs font-medium zt-ink-2">
        <span>{{ t('videoPlayground.settings.duration') }}</span>
        <span v-if="durationChoices.range && !smartActive" class="tabular-nums font-semibold zt-accent-text">
          {{ t('videoPlayground.card.duration', { duration: sliderValue }) }}
        </span>
      </label>
      <div v-if="durationChoices.options.length > 0 || durationChoices.allowSmart" class="flex flex-wrap gap-2">
        <button
          v-for="value in durationChoices.options"
          :key="value"
          type="button"
          :class="chipClass(duration === value, false)"
          @click="emit('update:duration', value)"
        >
          {{ t('videoPlayground.card.duration', { duration: value }) }}
        </button>
        <button
          v-if="durationChoices.allowSmart"
          type="button"
          :class="chipClass(smartActive, false)"
          :title="t('videoPlayground.settings.smartDurationHint')"
          @click="toggleSmartDuration"
        >
          {{ t('videoPlayground.settings.smartDuration') }}
        </button>
      </div>
      <template v-if="durationChoices.options.length === 0 && durationChoices.range">
        <input
          class="duration-slider"
          :class="durationChoices.allowSmart ? 'mt-2' : ''"
          type="range"
          :min="durationChoices.range.min"
          :max="durationChoices.range.max"
          step="1"
          :value="sliderValue"
          :disabled="smartActive"
          :aria-label="t('videoPlayground.settings.duration')"
          @input="onSliderInput"
        />
        <div class="mt-1 flex justify-between text-[11px] tabular-nums zt-ink-3">
          <span>{{ t('videoPlayground.card.duration', { duration: durationChoices.range.min }) }}</span>
          <span>{{ t('videoPlayground.card.duration', { duration: durationChoices.range.max }) }}</span>
        </div>
      </template>
      <template v-else-if="durationChoices.options.length === 0">
        <input
          :value="smartActive ? '' : (duration ?? '')"
          type="number"
          min="1"
          step="1"
          class="input"
          :class="durationChoices.allowSmart ? 'mt-2' : ''"
          :disabled="smartActive"
          :placeholder="t('videoPlayground.settings.durationPlaceholder')"
          @input="onDurationInput"
        />
        <p class="mt-1 text-xs zt-ink-3">
          {{ t('videoPlayground.settings.durationFreeHint') }}
        </p>
      </template>
    </div>

    <!-- 分辨率：同一个值再点一次即取消，让"不指定"始终可达。 -->
    <div v-if="resolutionOptions.length > 0">
      <label class="mb-1 block text-xs font-medium zt-ink-2">
        {{ t('videoPlayground.settings.resolution') }}
      </label>
      <div class="flex flex-wrap gap-2">
        <button
          v-for="value in resolutionOptions"
          :key="value"
          type="button"
          :class="chipClass(resolution === value, false)"
          @click="emit('update:resolution', resolution === value ? '' : value)"
        >
          {{ value }}
        </button>
      </div>
    </div>

    <!-- 比例：adaptive 要有首帧才能选，上游对没有首帧的 adaptive 是硬 400。 -->
    <div v-if="ratioControl.values.length > 0">
      <label class="mb-1 flex flex-wrap items-center gap-2 text-xs font-medium zt-ink-2">
        <span>{{ t('videoPlayground.settings.ratio') }}</span>
        <span
          v-if="ratioControl.values.includes(VIDEO_RATIO_ADAPTIVE) && ratioControl.adaptiveDisabled"
          class="font-normal zt-ink-3"
        >
          {{ t('videoPlayground.settings.ratioAdaptiveHint') }}
        </span>
      </label>
      <div class="flex flex-wrap gap-2">
        <button
          v-for="value in ratioControl.values"
          :key="value"
          type="button"
          :disabled="value === VIDEO_RATIO_ADAPTIVE && ratioControl.adaptiveDisabled"
          :class="chipClass(ratio === value, value === VIDEO_RATIO_ADAPTIVE && ratioControl.adaptiveDisabled)"
          @click="emit('update:ratio', ratio === value ? '' : value)"
        >
          {{ value === VIDEO_RATIO_ADAPTIVE ? t('videoPlayground.settings.ratioAdaptive') : value }}
        </button>
      </div>
    </div>

    <!-- 开关与种子：只摆目录放行的。未勾选即不进请求体，让上游用自己的默认值。 -->
    <div v-if="flagControls.length > 0 || seedEnabled">
      <label class="mb-1 block text-xs font-medium zt-ink-2">
        {{ t('videoPlayground.settings.flags') }}
      </label>
      <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
        <label
          v-for="flag in flagControls"
          :key="flag"
          class="inline-flex cursor-pointer items-center gap-1.5 text-xs zt-ink-2"
        >
          <input
            type="checkbox"
            class="h-3.5 w-3.5 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-gray-600"
            :checked="flags[flag] === true"
            @change="emit('update:flag', flag, ($event.target as HTMLInputElement).checked)"
          />
          {{ t(`videoPlayground.flags.${flag}`) }}
        </label>
      </div>
      <div v-if="seedEnabled" class="mt-2 flex items-center gap-2">
        <span class="text-xs zt-ink-2">{{ t('videoPlayground.settings.seed') }}</span>
        <input
          :value="seed ?? ''"
          type="number"
          min="-1"
          step="1"
          class="input w-40"
          :placeholder="t('videoPlayground.settings.seedPlaceholder')"
          @input="onSeedInput"
        />
      </div>
    </div>

    <!-- 参考素材：按角色分桶，只填 URL 或 data: URI，不提供任何上传入口 -->
    <div v-if="contentSlots.length > 0">
      <button
        type="button"
        class="flex items-center gap-1 text-xs font-medium zt-ink-2"
        @click="showContent = !showContent"
      >
        <Icon :name="showContent ? 'chevronDown' : 'chevronRight'" size="sm" />
        {{ t('videoPlayground.settings.content') }}
      </button>
      <div v-if="showContent" class="mt-2 space-y-3">
        <div v-for="slot in contentSlots" :key="slot.role">
          <label class="mb-1 flex flex-wrap items-center gap-2 text-xs zt-ink-2">
            <span class="font-medium">{{ roleLabel(slot.role) }}</span>
            <span class="zt-ink-3">{{ t('videoPlayground.settings.slotMax', { max: slot.max }) }}</span>
            <span v-if="slot.missingPrerequisite" class="zt-warn-text">
              {{ t('videoPlayground.settings.requiresHint', { roles: slot.requires.map(roleLabel).join(' / ') }) }}
            </span>
          </label>
          <!-- 五个角色光看名字分不出用途：首帧、尾帧、风格参考、运镜参考、配乐参考是五件不同的事，
               混填的后果是上游一个笼统的 400。用途就地写在槽位上，不靠用户去翻文档。 -->
          <p v-if="roleHint(slot.role)" class="mb-1 text-[11px] leading-relaxed zt-ink-3">
            {{ roleHint(slot.role) }}
          </p>
          <div class="space-y-1">
            <div v-for="index in slotRowCount(slot)" :key="index" class="flex items-center gap-1">
              <input
                :value="(content[slot.role] || [])[index - 1] || ''"
                type="text"
                inputmode="url"
                class="input flex-1"
                :disabled="slot.missingPrerequisite"
                :placeholder="slotPlaceholder(slot)"
                @input="setContent(slot, index - 1, ($event.target as HTMLInputElement).value)"
              />
              <button
                v-if="slot.max > 1"
                type="button"
                class="btn btn-ghost btn-sm"
                :title="t('videoPlayground.settings.removeUrl')"
                @click="removeContent(slot, index - 1)"
              >
                <Icon name="x" size="sm" />
              </button>
            </div>
          </div>
          <button
            v-if="slot.max > 1 && slotRowCount(slot) < slot.max"
            type="button"
            class="mt-1 text-xs zt-accent-text hover:underline disabled:opacity-50"
            :disabled="slot.missingPrerequisite"
            @click="addContent(slot)"
          >
            + {{ t('videoPlayground.settings.addUrl') }}
          </button>
        </div>
        <p class="text-xs zt-ink-3">
          {{ t('videoPlayground.settings.contentHint') }}
        </p>
      </div>
    </div>

    <!-- 厂商特有参数：只有目录列出了才渲染；键名原样显示，这是厂商自己的词表。 -->
    <div v-if="providerOptionControls.length > 0">
      <label class="mb-1 block text-xs font-medium zt-ink-2">
        {{ t('videoPlayground.settings.providerOptions') }}
      </label>
      <div class="space-y-2">
        <div v-for="control in providerOptionControls" :key="control.name">
          <span class="mb-1 block font-mono text-[11px] zt-ink-3">{{ control.name }}</span>
          <div v-if="control.values.length > 0" class="flex flex-wrap gap-2">
            <button
              v-for="value in control.values"
              :key="value"
              type="button"
              :class="chipClass(providerOptions[control.name] === value, false)"
              @click="emit('update:providerOption', control.name, providerOptions[control.name] === value ? '' : value)"
            >
              {{ value }}
            </button>
          </div>
          <input
            v-else
            :value="providerOptions[control.name] || ''"
            type="text"
            class="input"
            :placeholder="t('videoPlayground.settings.providerOptionPlaceholder')"
            @input="emit('update:providerOption', control.name, ($event.target as HTMLInputElement).value)"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { Icon } from '@/components/icons'
import type { VideoModel } from '@/api/video'
import {
  VIDEO_RATIO_ADAPTIVE,
  VIDEO_SMART_DURATION,
  type VideoBooleanFlag,
  type VideoContentSlot,
  type VideoDurationChoices,
  type VideoProviderOptionControl,
  type VideoRatioControl,
} from '@/composables/useVideoPlaygroundModels'

const props = defineProps<{
  models: VideoModel[]
  modelId: string
  duration: number | null
  durationChoices: VideoDurationChoices
  resolution: string
  resolutionOptions: string[]
  ratio: string
  ratioControl: VideoRatioControl
  flags: Partial<Record<VideoBooleanFlag, boolean>>
  flagControls: VideoBooleanFlag[]
  seed: number | null
  seedEnabled: boolean
  content: Record<string, string[]>
  contentSlots: VideoContentSlot[]
  providerOptions: Record<string, string>
  providerOptionControls: VideoProviderOptionControl[]
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelId', value: string): void
  (e: 'update:duration', value: number | null): void
  (e: 'update:resolution', value: string): void
  (e: 'update:ratio', value: string): void
  (e: 'update:flag', name: VideoBooleanFlag, value: boolean): void
  (e: 'update:seed', value: number | null): void
  (e: 'update:content', role: string, values: string[]): void
  (e: 'update:providerOption', name: string, value: string): void
}>()

const { t } = useI18n()
const showContent = ref(false)

const hasModel = computed(() => !!props.modelId)
const smartActive = computed(() => props.duration === VIDEO_SMART_DURATION)

/** 滑杆必须始终有一个落在区间内的位置：没选过（或选了智能）就停在目录给的下界。 */
const sliderValue = computed(() => {
  const range = props.durationChoices.range
  if (!range) return props.duration ?? 0
  const current = props.duration === null || smartActive.value ? range.min : props.duration
  return Math.min(range.max, Math.max(range.min, current))
})

/** 退出智能时长时回到一个确定的档位，而不是 null——否则目录有档位的模型会立刻报"请选时长"。 */
function toggleSmartDuration(): void {
  if (!smartActive.value) {
    emit('update:duration', VIDEO_SMART_DURATION)
    return
  }
  const { options, range } = props.durationChoices
  emit('update:duration', options[0] ?? range?.min ?? null)
}

function onSliderInput(event: Event): void {
  const value = Number.parseInt((event.target as HTMLInputElement).value, 10)
  if (Number.isFinite(value)) emit('update:duration', value)
}

/** 清空输入等于"不指定时长"，交 null 让 buildRequest 整个略掉 duration 字段。 */
function onDurationInput(event: Event): void {
  const raw = (event.target as HTMLInputElement).value.trim()
  if (!raw) {
    emit('update:duration', null)
    return
  }
  const value = Number.parseInt(raw, 10)
  emit('update:duration', Number.isFinite(value) && value > 0 ? value : null)
}

/** 种子接受 -1（随机）到 2^32-1；清空即不指定。 */
function onSeedInput(event: Event): void {
  const raw = (event.target as HTMLInputElement).value.trim()
  if (!raw) {
    emit('update:seed', null)
    return
  }
  const value = Number.parseInt(raw, 10)
  emit('update:seed', Number.isFinite(value) && value >= -1 ? value : null)
}

const chipBase =
  'rounded-full border px-3 py-1 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50'

function chipClass(active: boolean, disabled: boolean): string {
  const tone = active
    ? 'border-primary-500 bg-primary-50 zt-accent-text dark:bg-primary-900/30'
    : 'zt-border-c2 zt-ink-2 hover:border-gray-400 dark:border-gray-600'
  return `${chipBase} ${tone}${disabled ? ' opacity-50' : ''}`
}

const modelOptions = computed(() =>
  props.models.map((model) => ({
    value: model.id,
    // composite 分组同时列出多家的模型，不标 provider 用户无从分辨。
    label: props.models.some((other) => other.provider !== model.provider)
      ? `${model.id} · ${model.provider}`
      : model.id,
  })),
)

/** 目录里的角色名对用户没有意义，有对应文案就用文案，没有就原样显示。 */
function roleLabel(role: string): string {
  const key = `videoPlayground.roles.${role}`
  const label = t(key)
  return label === key ? role : label
}

/** 角色用途说明。没写文案的角色整行不渲染，目录将来新增角色也不会冒出一行键名。 */
function roleHint(role: string): string {
  const key = `videoPlayground.roleHints.${role}`
  const hint = t(key)
  return hint === key ? '' : hint
}

function slotRowCount(slot: VideoContentSlot): number {
  const filled = (props.content[slot.role] || []).length
  return Math.min(slot.max, Math.max(1, filled))
}

function setContent(slot: VideoContentSlot, index: number, value: string): void {
  const values = [...(props.content[slot.role] || [])]
  while (values.length <= index) values.push('')
  values[index] = value
  emit('update:content', slot.role, values)
}

function addContent(slot: VideoContentSlot): void {
  const values = [...(props.content[slot.role] || [])]
  if (values.length === 0) values.push('')
  values.push('')
  emit('update:content', slot.role, values.slice(0, slot.max))
}

function removeContent(slot: VideoContentSlot, index: number): void {
  const values = [...(props.content[slot.role] || [])]
  values.splice(index, 1)
  emit('update:content', slot.role, values)
}

function slotPlaceholder(slot: VideoContentSlot): string {
  const acceptsData = slot.accepts.includes('data_uri')
  const acceptsHTTPS = slot.accepts.includes('https')
  if (acceptsData && !acceptsHTTPS) return t('videoPlayground.settings.dataUriOnly')
  return acceptsData ? t('videoPlayground.settings.urlOrDataUri') : t('videoPlayground.settings.urlOnly')
}
</script>

<style scoped>
/* 滑杆全仓只此一处，就地写样式而不进 Tailwind 配置。两套伪元素缺一不可：
   ::-webkit-slider-thumb 管 Chrome/Safari，::-moz-range-thumb 管 Firefox，
   两者不能合并成一条选择器——任一浏览器认不得的选择器会让整条规则被丢弃。 */
.duration-slider {
  @apply h-1.5 w-full cursor-pointer appearance-none rounded-full bg-gray-200 dark:bg-gray-600;
}

.duration-slider:disabled {
  @apply cursor-not-allowed opacity-50;
}

.duration-slider::-webkit-slider-thumb {
  @apply h-4 w-4 appearance-none rounded-full border-2 border-white bg-primary-600 shadow;
}

.duration-slider::-moz-range-thumb {
  @apply h-4 w-4 rounded-full border-2 border-white bg-primary-600 shadow;
}

.duration-slider:focus-visible {
  @apply outline-none ring-2 ring-primary-500 ring-offset-2 dark:ring-offset-gray-900;
}
</style>
