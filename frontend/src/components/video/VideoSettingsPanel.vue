<template>
  <div class="space-y-4">
    <!-- 模型 -->
    <div>
      <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">
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
         · values 非空 → 只能挑这几档（Sora 的 4/8/12/16/20，或运营在价目里收窄过的档位）；
         · min/max 非空 → 区间内任意整数秒，给滑杆；
         · 两者都没有 → 自由输入。都没有并不代表不能选时长——按秒计价的价目本就不必逐条
           列 conditions.seconds。整块藏掉只会让用户被迫接受上游默认值。 -->
    <div v-if="hasModel">
      <label class="mb-1 flex items-center justify-between gap-2 text-xs font-medium text-gray-600 dark:text-gray-300">
        <span>{{ t('videoPlayground.settings.seconds') }}</span>
        <span v-if="secondsRange" class="tabular-nums font-semibold text-primary-600 dark:text-primary-400">
          {{ t('videoPlayground.card.seconds', { seconds: sliderValue }) }}
        </span>
      </label>
      <div v-if="secondsOptions.length > 0" class="flex flex-wrap gap-2">
        <button
          v-for="value in secondsOptions"
          :key="value"
          type="button"
          :class="chipClass(seconds === value, false)"
          @click="emit('update:seconds', value)"
        >
          {{ t('videoPlayground.card.seconds', { seconds: value }) }}
        </button>
      </div>
      <template v-else-if="secondsRange">
        <input
          class="seconds-slider"
          type="range"
          :min="secondsRange.min"
          :max="secondsRange.max"
          step="1"
          :value="sliderValue"
          :aria-label="t('videoPlayground.settings.seconds')"
          @input="onSliderInput"
        />
        <div class="mt-1 flex justify-between text-[11px] tabular-nums text-gray-400 dark:text-gray-500">
          <span>{{ t('videoPlayground.card.seconds', { seconds: secondsRange.min }) }}</span>
          <span>{{ t('videoPlayground.card.seconds', { seconds: secondsRange.max }) }}</span>
        </div>
      </template>
      <template v-else>
        <input
          :value="seconds ?? ''"
          type="number"
          min="1"
          step="1"
          class="input"
          :placeholder="t('videoPlayground.settings.secondsPlaceholder')"
          @input="onSecondsInput"
        />
        <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
          {{ t('videoPlayground.settings.secondsFreeHint') }}
        </p>
      </template>
    </div>

    <!-- 画幅：size 与 ratio/aspect_ratio/resolution 被拉平成同一种控件，
         可用性完全由目录里声明的互斥与"被素材决定"关系派生。 -->
    <div v-for="control in framingControls" :key="control.name">
      <label class="mb-1 flex flex-wrap items-center gap-2 text-xs font-medium text-gray-600 dark:text-gray-300">
        <span>{{ framingLabel(control.name) }}</span>
        <span v-if="control.disabledByConflict" class="font-normal text-amber-600 dark:text-amber-400">
          {{ t('videoPlayground.settings.conflictHint', { field: fieldLabel(control.disabledByConflict) }) }}
        </span>
        <span v-else-if="control.ignoredBy" class="font-normal text-amber-600 dark:text-amber-400">
          {{ t('videoPlayground.settings.ignoredHint', { field: fieldLabel(control.ignoredBy) }) }}
        </span>
        <!-- 还没填素材时也要先说清楚"这个控件只在文生视频下生效"。等填完素材才由上面那条
             琥珀色提示告知，用户已经白选了一次画幅。Ark 的 resolution/ratio 不声明这层关系，
             ignoredWhenPresent 为空，不会误挂这句。 -->
        <span v-else-if="control.ignoredWhenPresent.length > 0" class="font-normal text-gray-400 dark:text-gray-500">
          {{ t('videoPlayground.settings.framingTextOnlyHint') }}
        </span>
      </label>
      <div class="flex flex-wrap gap-2">
        <button
          v-for="value in control.values"
          :key="value"
          type="button"
          :disabled="!!control.disabledByConflict || !!control.ignoredBy"
          :class="chipClass(framing[control.name] === value, !!control.disabledByConflict || !!control.ignoredBy)"
          @click="toggleFraming(control.name, value)"
        >
          {{ value }}
        </button>
      </div>
    </div>

    <!-- 参考素材：只填 URL，不提供任何上传入口 -->
    <div v-if="referenceSlots.length > 0">
      <button
        type="button"
        class="flex items-center gap-1 text-xs font-medium text-gray-600 dark:text-gray-300"
        @click="showReferences = !showReferences"
      >
        <Icon :name="showReferences ? 'chevronDown' : 'chevronRight'" size="sm" />
        {{ t('videoPlayground.settings.references') }}
      </button>
      <div v-if="showReferences" class="mt-2 space-y-3">
        <div v-for="slot in referenceSlots" :key="slot.field">
          <label class="mb-1 flex flex-wrap items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
            <span class="font-medium">{{ fieldLabel(slot.field) }}</span>
            <span class="text-gray-400 dark:text-gray-500">{{ t('videoPlayground.settings.slotMax', { max: slot.max }) }}</span>
            <span v-if="slot.disabledByConflict" class="text-amber-600 dark:text-amber-400">
              {{ t('videoPlayground.settings.conflictHint', { field: fieldLabel(slot.disabledByConflict) }) }}
            </span>
            <span v-else-if="slot.missingPrerequisite" class="text-amber-600 dark:text-amber-400">
              {{ t('videoPlayground.settings.requiresHint', { fields: (slot.requires_any || []).map(fieldLabel).join(' / ') }) }}
            </span>
          </label>
          <!-- 六个槽位光看名字分不出用途：首帧、首尾帧、风格参考、运镜参考是四件不同的事，
               混填的后果是上游一个笼统的 400。用途与硬规则就地写在槽位上，不靠用户去翻文档。 -->
          <p v-if="fieldHint(slot.field)" class="mb-1 text-[11px] leading-relaxed text-gray-400 dark:text-gray-500">
            {{ fieldHint(slot.field) }}
          </p>
          <div class="space-y-1">
            <div v-for="index in slotRowCount(slot)" :key="index" class="flex items-center gap-1">
              <input
                :value="(references[slot.field] || [])[index - 1] || ''"
                type="url"
                inputmode="url"
                class="input flex-1"
                :disabled="!!slot.disabledByConflict || slot.missingPrerequisite"
                :placeholder="slotPlaceholder(slot)"
                @input="setReference(slot, index - 1, ($event.target as HTMLInputElement).value)"
              />
              <button
                v-if="slot.max > 1"
                type="button"
                class="btn btn-ghost btn-sm"
                :title="t('videoPlayground.settings.removeUrl')"
                @click="removeReference(slot, index - 1)"
              >
                <Icon name="x" size="sm" />
              </button>
            </div>
          </div>
          <button
            v-if="slot.max > 1 && slotRowCount(slot) < slot.max"
            type="button"
            class="mt-1 text-xs text-primary-600 hover:underline disabled:opacity-50 dark:text-primary-400"
            :disabled="!!slot.disabledByConflict || slot.missingPrerequisite"
            @click="addReference(slot)"
          >
            + {{ t('videoPlayground.settings.addUrl') }}
          </button>
        </div>
        <p class="text-xs text-gray-400 dark:text-gray-500">
          {{ t('videoPlayground.settings.referenceHint') }}
        </p>
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
import type { VideoFramingControl, VideoReferenceSlot } from '@/composables/useVideoPlaygroundModels'

const props = defineProps<{
  models: VideoModel[]
  modelId: string
  seconds: number | null
  secondsOptions: number[]
  secondsRange: { min: number; max: number } | null
  framing: Record<string, string>
  framingControls: VideoFramingControl[]
  references: Record<string, string[]>
  referenceSlots: VideoReferenceSlot[]
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelId', value: string): void
  (e: 'update:seconds', value: number | null): void
  (e: 'update:framing', name: string, value: string): void
  (e: 'update:reference', field: string, values: string[]): void
}>()

const { t } = useI18n()
const showReferences = ref(false)

const hasModel = computed(() => !!props.modelId)

/** 滑杆必须始终有一个落在区间内的位置：没选过就停在目录给的下界。 */
const sliderValue = computed(() => {
  const range = props.secondsRange
  if (!range) return props.seconds ?? 0
  const current = props.seconds ?? range.min
  return Math.min(range.max, Math.max(range.min, current))
})

function onSliderInput(event: Event): void {
  const value = Number.parseInt((event.target as HTMLInputElement).value, 10)
  if (Number.isFinite(value)) emit('update:seconds', value)
}

/** 清空输入等于"不指定时长"，交 null 让 buildRequest 整个略掉 seconds 字段。 */
function onSecondsInput(event: Event): void {
  const raw = (event.target as HTMLInputElement).value.trim()
  if (!raw) {
    emit('update:seconds', null)
    return
  }
  const value = Number.parseInt(raw, 10)
  emit('update:seconds', Number.isFinite(value) && value > 0 ? value : null)
}

const chipBase =
  'rounded-full border px-3 py-1 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50'

function chipClass(active: boolean, disabled: boolean): string {
  const tone = active
    ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
    : 'border-gray-300 text-gray-600 hover:border-gray-400 dark:border-gray-600 dark:text-gray-300'
  return `${chipBase} ${tone}${disabled ? ' opacity-50' : ''}`
}

const modelOptions = computed(() =>
  props.models.map((model) => ({
    value: model.id,
    // composite 分组同时列出两侧的模型，不标 provider 用户无从分辨。
    label: props.models.some((other) => other.provider !== model.provider)
      ? `${model.id} · ${model.provider}`
      : model.id,
  })),
)

/** 目录里的字段名对用户没有意义，有对应文案就用文案，没有就原样显示。 */
function fieldLabel(field: string): string {
  const key = `videoPlayground.fields.${field}`
  const label = t(key)
  return label === key ? field : label
}

function framingLabel(name: string): string {
  return fieldLabel(name)
}

/** 槽位用途说明。没写文案的字段整行不渲染，目录将来新增槽位也不会冒出一行键名。 */
function fieldHint(field: string): string {
  const key = `videoPlayground.fieldHints.${field}`
  const hint = t(key)
  return hint === key ? '' : hint
}

/** 同一个值再点一次即取消，让"不指定画幅"始终可达。 */
function toggleFraming(name: string, value: string): void {
  emit('update:framing', name, props.framing[name] === value ? '' : value)
}

function slotRowCount(slot: VideoReferenceSlot): number {
  const filled = (props.references[slot.field] || []).length
  return Math.min(slot.max, Math.max(1, filled))
}

function setReference(slot: VideoReferenceSlot, index: number, value: string): void {
  const values = [...(props.references[slot.field] || [])]
  while (values.length <= index) values.push('')
  values[index] = value
  emit('update:reference', slot.field, values)
}

function addReference(slot: VideoReferenceSlot): void {
  const values = [...(props.references[slot.field] || [])]
  if (values.length === 0) values.push('')
  values.push('')
  emit('update:reference', slot.field, values.slice(0, slot.max))
}

function removeReference(slot: VideoReferenceSlot, index: number): void {
  const values = [...(props.references[slot.field] || [])]
  values.splice(index, 1)
  emit('update:reference', slot.field, values)
}

function slotPlaceholder(slot: VideoReferenceSlot): string {
  return slot.accepts.includes('data_uri')
    ? t('videoPlayground.settings.urlOrDataUri')
    : t('videoPlayground.settings.urlOnly')
}
</script>

<style scoped>
/* 滑杆全仓只此一处，就地写样式而不进 Tailwind 配置。两套伪元素缺一不可：
   ::-webkit-slider-thumb 管 Chrome/Safari，::-moz-range-thumb 管 Firefox，
   两者不能合并成一条选择器——任一浏览器认不得的选择器会让整条规则被丢弃。 */
.seconds-slider {
  @apply h-1.5 w-full cursor-pointer appearance-none rounded-full bg-gray-200 dark:bg-gray-600;
}

.seconds-slider::-webkit-slider-thumb {
  @apply h-4 w-4 appearance-none rounded-full border-2 border-white bg-primary-600 shadow;
}

.seconds-slider::-moz-range-thumb {
  @apply h-4 w-4 rounded-full border-2 border-white bg-primary-600 shadow;
}

.seconds-slider:focus-visible {
  @apply outline-none ring-2 ring-primary-500 ring-offset-2 dark:ring-offset-gray-900;
}
</style>
