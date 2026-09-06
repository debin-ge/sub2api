<template>
  <div class="space-y-5" data-test="video-pricing-editor">
    <div class="flex flex-wrap items-center gap-2">
      <button
        v-for="option in modeOptions"
        :key="option.value"
        type="button"
        class="mode-btn"
        :class="mode === option.value ? 'mode-btn-active' : ''"
        :data-test="`video-mode-${option.value}`"
        @click="setMode(option.value)"
      >
        {{ option.label }}
      </button>
    </div>

    <p v-if="mode === 'inherit'" class="text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.modelPrices.video.inheritHint') }}
      <span v-if="inheritedValue?.enabled">{{ t('admin.modelPrices.video.inheritedSummary', { count: inheritedValue.rules?.length || 0 }) }}</span>
    </p>
    <p v-else-if="mode === 'disabled'" class="text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.modelPrices.video.disabledHint') }}
    </p>

    <template v-if="config?.enabled">
      <div v-if="validationErrors.length" data-test="video-validation" class="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-200">
        <div v-for="error in validationErrors" :key="error">{{ error }}</div>
      </div>

      <div class="flex border-b border-gray-200 dark:border-dark-600" role="tablist">
        <button
          type="button"
          class="experience-tab"
          :class="editorExperience === 'simple' ? 'experience-tab-active' : ''"
          data-test="video-editor-simple"
          @click="setEditorExperience('simple')"
        >
          {{ t('admin.modelPrices.video.simpleMode') }}
        </button>
        <button
          type="button"
          class="experience-tab"
          :class="editorExperience === 'advanced' ? 'experience-tab-active' : ''"
          data-test="video-editor-advanced"
          @click="setEditorExperience('advanced')"
        >
          {{ t('admin.modelPrices.video.advancedMode') }}
        </button>
      </div>

      <div v-if="simpleConversionPending" class="rounded border border-amber-200 bg-amber-50 px-3 py-3 text-sm text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-100">
        <p>{{ t('admin.modelPrices.video.simpleConversionWarning') }}</p>
        <div class="mt-3 flex justify-end gap-2">
          <button type="button" class="btn btn-secondary btn-sm" @click="simpleConversionPending = false">{{ t('admin.modelPrices.cancel') }}</button>
          <button type="button" class="btn btn-primary btn-sm" data-test="video-confirm-simple-conversion" @click="confirmSimpleConversion">{{ t('admin.modelPrices.video.simpleConversionConfirm') }}</button>
        </div>
      </div>

      <VideoPricingSimpleEditor
        v-if="editorExperience === 'simple' && !simpleConversionPending && config"
        :model-value="config"
        @update:model-value="emit('update:modelValue', $event)"
      />

      <template v-else-if="editorExperience === 'advanced'">
      <details class="advanced-block" data-test="video-advanced-setup">
        <summary>{{ t('admin.modelPrices.video.advancedSetup') }}</summary>
        <div class="mt-4 space-y-5">
      <section class="section-block border-t-0 pt-0">
        <h3 class="section-title">{{ t('admin.modelPrices.video.defaults') }}</h3>
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <label class="field-label">
            <span>{{ t('admin.modelPrices.video.defaultResolution') }}</span>
            <select
              class="input"
              :value="config.defaults?.resolution || ''"
              @change="updateDefaults({ resolution: inputValue($event) })"
            >
              <option value="">{{ t('admin.modelPrices.video.none') }}</option>
              <option v-for="entry in resolutionEntries" :key="entry[0]" :value="entry[0]">{{ entry[0] }}</option>
            </select>
          </label>
          <label class="field-label">
            <span>{{ t('admin.modelPrices.video.requestMode') }}</span>
            <select class="input" :value="config.defaults?.request_mode || 'standard'" @change="updateDefaults({ request_mode: inputValue($event) as 'standard' | 'batch' })">
              <option value="standard">Standard</option>
              <option value="batch">Batch</option>
            </select>
          </label>
          <label class="field-label">
            <span>{{ t('admin.modelPrices.video.inferenceMode') }}</span>
            <select class="input" :value="config.defaults?.inference_mode || 'online'" @change="updateDefaults({ inference_mode: inputValue($event) as 'online' | 'offline' })">
              <option value="online">{{ t('admin.modelPrices.video.online') }}</option>
              <option value="offline">{{ t('admin.modelPrices.video.offline') }}</option>
            </select>
          </label>
          <label class="flex min-h-10 items-center gap-2 self-end text-sm text-gray-700 dark:text-gray-200">
            <input
              type="checkbox"
              class="rounded border-gray-300"
              :checked="config.defaults?.generate_audio || false"
              @change="updateDefaults({ generate_audio: checkedValue($event) })"
            />
            {{ t('admin.modelPrices.video.generateAudio') }}
          </label>
        </div>
      </section>

      <section class="section-block">
        <div class="section-header">
          <h3 class="section-title">{{ t('admin.modelPrices.video.resolutions') }}</h3>
          <button type="button" class="btn btn-secondary btn-sm" @click="addResolution">
            <Icon name="plus" size="sm" />
            <span>{{ t('admin.modelPrices.video.addResolution') }}</span>
          </button>
        </div>
        <div v-if="resolutionEntries.length" class="divide-y divide-gray-200 dark:divide-dark-600">
          <div v-for="entry in resolutionEntries" :key="entry[0]" class="editor-row grid gap-2 sm:grid-cols-[11rem,1fr,2.5rem]">
            <input class="input" :value="entry[0]" :placeholder="t('admin.modelPrices.video.resolutionName')" @change="renameResolution(entry[0], inputValue($event))" />
            <input class="input" :value="entry[1].sizes.join(', ')" :placeholder="t('admin.modelPrices.video.sizesPlaceholder')" @input="updateResolutionSizes(entry[0], inputValue($event))" />
            <button type="button" class="icon-btn" :title="t('admin.modelPrices.video.remove')" @click="removeResolution(entry[0])">
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </div>
        <p v-else class="empty-line">{{ t('admin.modelPrices.video.noResolutions') }}</p>
      </section>

      <section class="section-block">
        <div class="section-header">
          <h3 class="section-title">{{ t('admin.modelPrices.video.estimators') }}</h3>
          <button type="button" class="btn btn-secondary btn-sm" @click="addEstimator">
            <Icon name="plus" size="sm" />
            <span>{{ t('admin.modelPrices.video.addEstimator') }}</span>
          </button>
        </div>
        <div v-if="estimatorEntries.length" class="space-y-3">
          <div v-for="entry in estimatorEntries" :key="entry[0]" class="repeated-item">
            <div class="grid gap-2 sm:grid-cols-[1fr,1fr,2.5rem]">
              <label class="field-label">
                <span>{{ t('admin.modelPrices.video.estimatorName') }}</span>
                <input class="input" :value="entry[0]" @change="renameEstimator(entry[0], inputValue($event))" />
              </label>
              <label class="field-label">
                <span>{{ t('admin.modelPrices.video.estimatorType') }}</span>
                <select class="input" :value="entry[1].type" @change="setEstimatorType(entry[0], inputValue($event) as VideoEstimatorType)">
                  <option value="pixel_frame">pixel_frame</option>
                  <option value="fixed_tokens_per_second">fixed_tokens_per_second</option>
                  <option value="fixed_max_units">fixed_max_units</option>
                </select>
              </label>
              <button type="button" class="icon-btn self-end" :title="t('admin.modelPrices.video.remove')" @click="removeEstimator(entry[0])">
                <Icon name="trash" size="sm" />
              </button>
            </div>
            <div class="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
              <label v-if="entry[1].type === 'pixel_frame'" class="field-label">
                <span>{{ t('admin.modelPrices.video.tokenScope') }}</span>
                <select class="input" :value="entry[1].token_scope || 'output_only'" @change="updateEstimator(entry[0], { token_scope: inputValue($event) as VideoTokenScope })">
                  <option value="output_only">output_only</option>
                  <option value="input_plus_output">input_plus_output</option>
                </select>
              </label>
              <NumberField v-if="entry[1].type === 'pixel_frame'" :label="t('admin.modelPrices.video.fps')" :model-value="entry[1].fps" @update:model-value="updateEstimator(entry[0], { fps: $event })" />
              <NumberField v-if="entry[1].type === 'pixel_frame'" :label="t('admin.modelPrices.video.divisor')" :model-value="entry[1].divisor" @update:model-value="updateEstimator(entry[0], { divisor: $event })" />
              <NumberField v-if="entry[1].type === 'pixel_frame' && entry[1].token_scope === 'input_plus_output'" :label="t('admin.modelPrices.video.maxInputSeconds')" :model-value="entry[1].max_input_video_seconds" @update:model-value="updateEstimator(entry[0], { max_input_video_seconds: $event })" />
              <NumberField v-if="entry[1].type === 'fixed_tokens_per_second'" :label="t('admin.modelPrices.video.tokensPerSecond')" :model-value="entry[1].tokens_per_second" @update:model-value="updateEstimator(entry[0], { tokens_per_second: $event })" />
              <NumberField v-if="entry[1].type === 'fixed_max_units'" :label="t('admin.modelPrices.video.maxUnits')" :model-value="entry[1].max_units" @update:model-value="updateEstimator(entry[0], { max_units: $event })" />
			  <NumberField :label="t('admin.modelPrices.video.minimumUnits')" :model-value="unconditionalMinimumUnits(entry[1])" @update:model-value="updateMinimumUnits(entry[0], $event)" />
            </div>
          </div>
        </div>
        <p v-else class="empty-line">{{ t('admin.modelPrices.video.noEstimators') }}</p>
      </section>
        </div>
      </details>

      <section class="section-block">
        <div class="section-header">
          <h3 class="section-title">{{ t('admin.modelPrices.video.rules') }}</h3>
          <button type="button" class="btn btn-secondary btn-sm" @click="addRule">
            <Icon name="plus" size="sm" />
            <span>{{ t('admin.modelPrices.video.addRule') }}</span>
          </button>
        </div>
        <div v-if="config.rules?.length" class="space-y-3">
          <div v-for="(rule, index) in config.rules" :key="`${index}:${rule.key}`" class="repeated-item">
            <div class="grid items-end gap-2 sm:grid-cols-2 lg:grid-cols-[1fr,1fr,1.4fr,5rem]">
              <label class="field-label">
                <span>{{ t('admin.modelPrices.video.billingUnit') }}</span>
                <select data-test="video-billing-unit" class="input" :value="rule.billing_unit" @change="setRuleBillingUnit(index, inputValue($event) as VideoBillingUnit)">
                  <option value="request">{{ t('admin.modelPrices.video.perRequestOption') }}</option>
                  <option value="second">{{ t('admin.modelPrices.video.perSecondOption') }}</option>
                  <option value="video_token">{{ t('admin.modelPrices.video.perVideoTokenOption') }}</option>
                </select>
              </label>
              <label class="field-label">
                <span>{{ priceUnitLabel(rule.billing_unit) }}</span>
                <input class="input" type="number" min="0" step="any" :value="displayRulePrice(rule)" @input="updateRulePrice(index, inputValue($event))" />
              </label>
              <div class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
                <div class="font-medium text-gray-600 dark:text-gray-300">{{ t('admin.modelPrices.video.appliesTo') }}</div>
                <div class="mt-2 truncate" :title="ruleScopeSummary(rule)">{{ ruleScopeSummary(rule) }}</div>
              </div>
              <div class="flex items-end justify-end gap-1">
                <button type="button" class="icon-btn" :title="t('admin.modelPrices.video.duplicate')" @click="duplicateRule(index)"><Icon name="copy" size="sm" /></button>
                <button type="button" class="icon-btn icon-btn-danger" :title="t('admin.modelPrices.video.remove')" @click="removeRule(index)"><Icon name="trash" size="sm" /></button>
              </div>
            </div>

            <NumberField
              v-if="rule.billing_unit === 'video_token' && ruleEstimator(rule)?.type === 'fixed_tokens_per_second'"
              class="mt-3 block max-w-sm"
              data-test="video-tokens-per-second"
              :label="t('admin.modelPrices.video.tokensPerSecondRequired')"
              :model-value="ruleEstimator(rule)?.tokens_per_second"
              @update:model-value="updateRuleTokensPerSecond(rule, $event)"
            />

            <details class="rule-details">
              <summary>{{ t('admin.modelPrices.video.advancedRule') }}</summary>
              <div class="mt-3 grid gap-2 sm:grid-cols-2">
                <label class="field-label">
                  <span>{{ t('admin.modelPrices.video.ruleKey') }}</span>
                  <input class="input" :value="rule.key" @input="updateRule(index, { key: inputValue($event) })" />
                </label>
                <NumberField :label="t('admin.modelPrices.video.priority')" :model-value="rule.priority || 0" @update:model-value="updateRule(index, { priority: $event })" />
              </div>

              <label v-if="rule.billing_unit === 'video_token'" class="field-label mt-3 block">
                <span>{{ t('admin.modelPrices.video.estimator') }}</span>
                <select class="input" :value="rule.estimator || ''" @change="updateRule(index, { estimator: inputValue($event) || undefined })">
                  <option value="">{{ t('admin.modelPrices.video.none') }}</option>
                  <option v-for="entry in estimatorEntries" :key="entry[0]" :value="entry[0]">{{ entry[0] }}</option>
                </select>
              </label>

            <div class="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              <TextListField :label="t('admin.modelPrices.video.operations')" :model-value="rule.conditions?.operations" placeholder="e.g. generate, edit" @update:model-value="updateCondition(index, 'operations', $event)" />
              <TextListField :label="t('admin.modelPrices.video.resolutionConditions')" :model-value="rule.conditions?.resolutions" placeholder="e.g. 480p, 720p" @update:model-value="updateCondition(index, 'resolutions', $event)" />
              <TextListField :label="t('admin.modelPrices.video.sizeConditions')" :model-value="rule.conditions?.sizes" placeholder="e.g. 1280x720" @update:model-value="updateCondition(index, 'sizes', $event)" />
              <TextListField :label="t('admin.modelPrices.video.seconds')" :model-value="(rule.conditions?.seconds || []).map(String)" placeholder="e.g. 5, 8, 10" @update:model-value="updateSecondCondition(index, $event)" />
              <TextListField :label="t('admin.modelPrices.video.inputTypes')" :model-value="rule.conditions?.input_types" placeholder="e.g. text, input_reference" @update:model-value="updateCondition(index, 'input_types', $event)" />
              <TextListField :label="t('admin.modelPrices.video.qualities')" :model-value="rule.conditions?.qualities" placeholder="e.g. standard, high" @update:model-value="updateCondition(index, 'qualities', $event)" />
              <TextListField :label="t('admin.modelPrices.video.serviceTiers')" :model-value="rule.conditions?.service_tiers" placeholder="e.g. default, priority" @update:model-value="updateCondition(index, 'service_tiers', $event)" />
              <BooleanConditionField :label="t('admin.modelPrices.video.inputHasVideo')" :model-value="rule.conditions?.input_has_video" @update:model-value="updateCondition(index, 'input_has_video', $event)" />
              <BooleanConditionField :label="t('admin.modelPrices.video.generateAudio')" :model-value="videoAudioCondition(rule.conditions || {})" @update:model-value="updateAudioCondition(index, $event)" />
            </div>

            <div class="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <ModeChecks :label="t('admin.modelPrices.video.requestModes')" :values="rule.conditions?.request_modes || []" :options="requestModeChecks" @update:values="updateCondition(index, 'request_modes', $event)" />
              <ModeChecks :label="t('admin.modelPrices.video.inferenceModes')" :values="rule.conditions?.inference_modes || []" :options="inferenceModeChecks" @update:values="updateCondition(index, 'inference_modes', $event)" />
              <label class="field-label">
                <span>{{ t('admin.modelPrices.video.validFrom') }}</span>
                <input class="input" type="datetime-local" :value="dateTimeLocal(rule.valid_from)" @change="updateRule(index, { valid_from: isoDate(inputValue($event)) })" />
              </label>
              <label class="field-label">
                <span>{{ t('admin.modelPrices.video.validUntil') }}</span>
                <input class="input" type="datetime-local" :value="dateTimeLocal(rule.valid_until)" @change="updateRule(index, { valid_until: isoDate(inputValue($event)) })" />
              </label>
            </div>
            </details>
          </div>
        </div>
        <p v-else class="empty-line">{{ t('admin.modelPrices.video.noRules') }}</p>
      </section>

      <details class="advanced-block">
        <summary>{{ t('admin.modelPrices.video.preview') }}</summary>
      <section class="section-block border-t-0 pt-3">
        <div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <label class="field-label">
            <span>{{ t('admin.modelPrices.video.operation') }}</span>
            <input v-model="preview.operation" class="input" />
          </label>
          <label class="field-label">
            <span>{{ t('admin.modelPrices.video.defaultResolution') }}</span>
            <select v-model="preview.resolution" class="input">
              <option v-for="entry in resolutionEntries" :key="entry[0]" :value="entry[0]">{{ entry[0] }}</option>
            </select>
          </label>
          <label class="field-label">
            <span>{{ t('admin.modelPrices.video.sizeConditions') }}</span>
            <input v-model="preview.size" class="input" placeholder="e.g. 1280x720" />
          </label>
          <NumberField :label="t('admin.modelPrices.video.seconds')" :model-value="preview.seconds" @update:model-value="preview.seconds = $event || 0" />
          <label class="field-label">
            <span>{{ t('admin.modelPrices.video.requestMode') }}</span>
            <select v-model="preview.requestMode" class="input"><option value="standard">Standard</option><option value="batch">Batch</option></select>
          </label>
          <label class="flex items-center gap-2 text-sm"><input v-model="preview.inputHasVideo" type="checkbox" />{{ t('admin.modelPrices.video.inputHasVideo') }}</label>
          <label class="flex items-center gap-2 text-sm"><input v-model="preview.generateAudio" type="checkbox" />{{ t('admin.modelPrices.video.generateAudio') }}</label>
          <label class="field-label"><span>{{ t('admin.modelPrices.video.inferenceMode') }}</span><select v-model="preview.inferenceMode" class="input"><option value="online">{{ t('admin.modelPrices.video.online') }}</option><option value="offline">{{ t('admin.modelPrices.video.offline') }}</option></select></label>
          <label class="field-label"><span>{{ t('admin.modelPrices.video.inputTypes') }}</span><input v-model="preview.inputType" class="input" placeholder="text" /></label>
          <label class="field-label"><span>{{ t('admin.modelPrices.video.qualities') }}</span><input v-model="preview.quality" class="input" /></label>
          <label class="field-label"><span>{{ t('admin.modelPrices.video.serviceTiers') }}</span><input v-model="preview.serviceTier" class="input" /></label>
        </div>
        <p class="mt-3 text-sm" :class="previewResponse?.matched ? 'text-gray-700 dark:text-gray-200' : 'text-red-600 dark:text-red-300'">
          {{ previewMessage }}
        </p>
        <div v-if="previewResponse && !previewResponse.matched && previewResponse.rejected_rules.length" class="mt-3 space-y-2" data-test="video-preview-mismatches">
          <div v-for="rule in previewResponse.rejected_rules" :key="rule.key" class="rounded border border-red-200/70 px-3 py-2 text-xs text-red-700 dark:border-red-900/50 dark:text-red-200">
            <div class="font-medium">{{ rule.key }}</div>
            <div v-for="mismatch in rule.mismatches" :key="`${rule.key}:${mismatch.field}`" class="mt-1">
              {{ t('admin.modelPrices.video.previewMismatch', { field: mismatch.field, expected: previewValue(mismatch.expected), actual: previewValue(mismatch.actual) }) }}
            </div>
          </div>
        </div>
      </section>
      </details>
      </template>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import VideoPricingSimpleEditor from './VideoPricingSimpleEditor.vue'
import { mTokToToken, previewVideoPrice, tokenToMTok } from '@/api/admin/modelPrices'
import { isCompleteVideoEstimator, prepareVideoPricingForSave, setUnconditionalMinimumUnits, unconditionalMinimumUnits } from './videoPricingForm'
import {
  buildSimpleVideoPricingConfig,
  parseSimpleVideoPricingConfig,
  suggestSimpleVideoPricingProfile,
} from './simpleVideoPricing'
import type {
  VideoBillingUnit,
  VideoEstimatorType,
  VideoPricingConditions,
  VideoPricingConfig,
  VideoPricingDefaults,
  VideoPricingRule,
  VideoPricingPreviewResult,
  VideoTokenScope,
  VideoUsageEstimator,
} from '@/api/admin/modelPrices'

const props = defineProps<{ modelValue: VideoPricingConfig | null; inheritedValue?: VideoPricingConfig | null }>()
const emit = defineEmits<{
  'update:modelValue': [value: VideoPricingConfig | null]
  'validation-change': [errors: string[]]
}>()
const { t } = useI18n()

const NumberField = defineComponent({
  props: { label: { type: String, required: true }, modelValue: { type: Number, default: undefined } },
  emits: ['update:modelValue'],
  setup(componentProps, { emit: componentEmit }) {
    return () => h('label', { class: 'field-label' }, [
      h('span', componentProps.label),
      h('input', {
        class: 'input', type: 'number', min: '0', step: 'any', value: componentProps.modelValue ?? '',
        onInput: (event: Event) => {
          const raw = (event.target as HTMLInputElement).value
          componentEmit('update:modelValue', raw === '' ? undefined : Number(raw))
        },
      }),
    ])
  },
})

const TextListField = defineComponent({
  props: { label: { type: String, required: true }, modelValue: { type: Array<string>, default: () => [] }, placeholder: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(componentProps, { emit: componentEmit }) {
    return () => h('label', { class: 'field-label' }, [
      h('span', componentProps.label),
      h('input', {
        class: 'input', value: componentProps.modelValue.join(', '), placeholder: componentProps.placeholder,
        onInput: (event: Event) => componentEmit('update:modelValue', splitList((event.target as HTMLInputElement).value)),
      }),
    ])
  },
})

const BooleanConditionField = defineComponent({
  props: { label: { type: String, required: true }, modelValue: { type: Boolean, default: undefined } },
  emits: ['update:modelValue'],
  setup(componentProps, { emit: componentEmit }) {
    return () => h('label', { class: 'field-label' }, [
      h('span', componentProps.label),
      h('select', {
        class: 'input', value: componentProps.modelValue == null ? '' : String(componentProps.modelValue),
        onChange: (event: Event) => {
          const raw = (event.target as HTMLSelectElement).value
          componentEmit('update:modelValue', raw === '' ? undefined : raw === 'true')
        },
      }, [h('option', { value: '' }, '-'), h('option', { value: 'false' }, t('admin.modelPrices.video.no')), h('option', { value: 'true' }, t('admin.modelPrices.video.yes'))]),
    ])
  },
})

const ModeChecks = defineComponent({
  props: {
    label: { type: String, required: true },
    values: { type: Array<string>, default: () => [] },
    options: { type: Array<{ value: string; label: string }>, default: () => [] },
  },
  emits: ['update:values'],
  setup(componentProps, { emit: componentEmit }) {
    return () => h('fieldset', { class: 'field-label' }, [
      h('legend', componentProps.label),
      h('div', { class: 'mt-2 flex flex-wrap gap-3' }, componentProps.options.map((option) => h('label', { class: 'flex items-center gap-1 text-sm font-normal' }, [
        h('input', {
          type: 'checkbox', checked: componentProps.values.includes(option.value),
          onChange: () => componentEmit('update:values', componentProps.values.includes(option.value) ? componentProps.values.filter((value) => value !== option.value) : [...componentProps.values, option.value]),
        }),
        option.label,
      ]))),
    ])
  },
})

type VideoMode = 'inherit' | 'enabled' | 'disabled'
type VideoEditorExperience = 'simple' | 'advanced'
const config = computed(() => props.modelValue)
const inheritedValue = computed(() => props.inheritedValue || null)
const mode = computed<VideoMode>(() => props.modelValue == null ? 'inherit' : props.modelValue.enabled ? 'enabled' : 'disabled')
const editorExperience = ref<VideoEditorExperience>(parseSimpleVideoPricingConfig(props.modelValue) ? 'simple' : 'advanced')
const simpleConversionPending = ref(false)
const resolutionEntries = computed(() => Object.entries(config.value?.resolutions || {}))
const estimatorEntries = computed(() => Object.entries(config.value?.estimators || {}))
const modeOptions = computed(() => [
  { value: 'inherit' as const, label: t('admin.modelPrices.video.inherit') },
  { value: 'enabled' as const, label: t('admin.modelPrices.video.enabled') },
  { value: 'disabled' as const, label: t('admin.modelPrices.video.disabled') },
])
const requestModeChecks = [{ value: 'standard', label: 'Standard' }, { value: 'batch', label: 'Batch' }]
const inferenceModeChecks = computed(() => [{ value: 'online', label: t('admin.modelPrices.video.online') }, { value: 'offline', label: t('admin.modelPrices.video.offline') }])

const preview = reactive({
	operation: 'generate', resolution: '', size: '', seconds: 5,
	inputHasVideo: false, generateAudio: false,
	requestMode: 'standard', inferenceMode: 'online', inputType: 'text', quality: '', serviceTier: '',
})
const previewResponse = ref<VideoPricingPreviewResult | null>(null)
const previewLoading = ref(false)
const previewRequestError = ref('')
let previewTimer: ReturnType<typeof setTimeout> | null = null
let previewSequence = 0

watch(resolutionEntries, (entries) => {
  if (!entries.some(([name]) => name === preview.resolution)) {
    preview.resolution = config.value?.defaults?.resolution || entries[0]?.[0] || ''
  }
}, { immediate: true })

watch(() => preview.inputHasVideo, (hasVideo) => {
  if (hasVideo && (preview.inputType === '' || preview.inputType === 'text' || preview.inputType === 'input_reference')) {
    preview.inputType = 'reference_video'
  } else if (!hasVideo && preview.inputType === 'reference_video') {
    preview.inputType = 'text'
  }
})

watch([config, () => ({ ...preview }), editorExperience], () => schedulePreview(), { deep: true, immediate: true })

onBeforeUnmount(() => {
  if (previewTimer) clearTimeout(previewTimer)
})

function defaultConfig(enabled = true): VideoPricingConfig {
  return {
    version: 1, enabled, currency: 'USD',
    defaults: { request_mode: 'standard', inference_mode: 'online', generate_audio: false },
    rules: [defaultRule()],
  }
}

function defaultRule(): VideoPricingRule {
  return { key: 'default', billing_unit: 'second', unit_price_usd: 0 }
}

function cloneConfig(value: VideoPricingConfig): VideoPricingConfig {
  return JSON.parse(JSON.stringify(value)) as VideoPricingConfig
}

function setMode(value: VideoMode) {
  if (value === 'inherit') {
    emit('update:modelValue', null)
    return
  }
	const next = props.modelValue ? cloneConfig(props.modelValue) : props.inheritedValue ? cloneConfig(props.inheritedValue) : defaultConfig()
  next.enabled = value === 'enabled'
  if (next.enabled && !next.rules?.length) next.rules = [defaultRule()]
  if (next.enabled) editorExperience.value = parseSimpleVideoPricingConfig(next) ? 'simple' : 'advanced'
  simpleConversionPending.value = false
  emit('update:modelValue', next)
}

function setEditorExperience(value: VideoEditorExperience) {
  if (value === 'advanced') {
    editorExperience.value = 'advanced'
    simpleConversionPending.value = false
    return
  }
  if (parseSimpleVideoPricingConfig(config.value)) {
    editorExperience.value = 'simple'
    simpleConversionPending.value = false
    return
  }
  simpleConversionPending.value = true
}

function confirmSimpleConversion() {
  if (!config.value) return
  const next = buildSimpleVideoPricingConfig(suggestSimpleVideoPricingProfile(config.value))
  editorExperience.value = 'simple'
  simpleConversionPending.value = false
  emit('update:modelValue', next)
}

function replaceConfig(mutator: (next: VideoPricingConfig) => void) {
  const next = props.modelValue ? cloneConfig(props.modelValue) : defaultConfig()
  mutator(next)
  emit('update:modelValue', next)
}

function updateDefaults(patch: Partial<VideoPricingDefaults>) {
  replaceConfig((next) => { next.defaults = { ...(next.defaults || {}), ...patch } })
}

function uniqueName(prefix: string, names: string[]) {
  let index = names.length + 1
  while (names.includes(`${prefix}-${index}`)) index++
  return `${prefix}-${index}`
}

function addResolution() {
  replaceConfig((next) => {
    next.resolutions ||= {}
    const name = uniqueName('resolution', Object.keys(next.resolutions))
    next.resolutions[name] = { sizes: [] }
  })
}

function renameResolution(oldName: string, newName: string) {
  const trimmed = newName.trim()
  if (!trimmed || trimmed === oldName) return
  replaceConfig((next) => {
    const resolutions = next.resolutions || {}
    const value = resolutions[oldName]
    if (!value || resolutions[trimmed]) return
    const rebuilt: typeof resolutions = {}
    for (const [name, spec] of Object.entries(resolutions)) rebuilt[name === oldName ? trimmed : name] = spec
    next.resolutions = rebuilt
    if (next.defaults?.resolution === oldName) next.defaults.resolution = trimmed
    for (const rule of next.rules || []) {
      if (rule.conditions?.resolutions) rule.conditions.resolutions = rule.conditions.resolutions.map((name) => name === oldName ? trimmed : name)
    }
  })
}

function updateResolutionSizes(name: string, raw: string) {
  replaceConfig((next) => {
    if (next.resolutions?.[name]) next.resolutions[name].sizes = splitList(raw)
  })
}

function removeResolution(name: string) {
  replaceConfig((next) => {
    if (next.resolutions) delete next.resolutions[name]
    if (next.defaults?.resolution === name) next.defaults.resolution = ''
  })
}

function addEstimator() {
  replaceConfig((next) => {
    next.estimators ||= {}
    const name = uniqueName('estimator', Object.keys(next.estimators))
    next.estimators[name] = { type: 'fixed_tokens_per_second', tokens_per_second: 1 }
  })
}

function renameEstimator(oldName: string, newName: string) {
  const trimmed = newName.trim()
  if (!trimmed || trimmed === oldName) return
  replaceConfig((next) => {
    const estimators = next.estimators || {}
    const value = estimators[oldName]
    if (!value || estimators[trimmed]) return
    const rebuilt: typeof estimators = {}
    for (const [name, estimator] of Object.entries(estimators)) rebuilt[name === oldName ? trimmed : name] = estimator
    next.estimators = rebuilt
    for (const rule of next.rules || []) if (rule.estimator === oldName) rule.estimator = trimmed
  })
}

function removeEstimator(name: string) {
  replaceConfig((next) => { if (next.estimators) delete next.estimators[name] })
}

function setEstimatorType(name: string, type: VideoEstimatorType) {
  const estimator: VideoUsageEstimator = type === 'pixel_frame'
    ? { type, token_scope: 'output_only', fps: 24, divisor: 1024 }
    : type === 'fixed_tokens_per_second'
      ? { type, tokens_per_second: 1 }
      : { type, max_units: 1 }
  replaceConfig((next) => { if (next.estimators?.[name]) next.estimators[name] = estimator })
}

function updateEstimator(name: string, patch: Partial<VideoUsageEstimator>) {
  replaceConfig((next) => {
    if (next.estimators?.[name]) next.estimators[name] = { ...next.estimators[name], ...patch }
  })
}

function updateMinimumUnits(name: string, units?: number) {
	replaceConfig((next) => {
		const estimator = next.estimators?.[name]
		if (!estimator) return
		next.estimators![name] = setUnconditionalMinimumUnits(estimator, units)
	})
}

function addRule() {
  replaceConfig((next) => {
    next.rules ||= []
    next.rules.push({ key: uniqueName('rule', next.rules.map((rule) => rule.key)), billing_unit: 'second', unit_price_usd: 0, priority: 0, conditions: {} })
  })
}

function updateRule(index: number, patch: Partial<VideoPricingRule>) {
  replaceConfig((next) => { if (next.rules?.[index]) next.rules[index] = { ...next.rules[index], ...patch } })
}

function setRuleBillingUnit(index: number, billingUnit: VideoBillingUnit) {
  replaceConfig((next) => {
    const rule = next.rules?.[index]
    if (!rule) return
    rule.billing_unit = billingUnit
    rule.unit_price_usd = 0
    if (billingUnit !== 'video_token') {
      delete rule.estimator
      return
    }
    const existing = Object.keys(next.estimators || {})[0]
    if (existing) {
      rule.estimator = existing
      return
    }
    next.estimators ||= {}
    const name = uniqueName('video-token', Object.keys(next.estimators))
    next.estimators[name] = { type: 'fixed_tokens_per_second' }
    rule.estimator = name
  })
}

function ruleEstimator(rule: VideoPricingRule): VideoUsageEstimator | undefined {
  if (!rule.estimator) return undefined
  const match = estimatorEntries.value.find(([name]) => name.toLowerCase() === rule.estimator?.toLowerCase())
  return match?.[1]
}

function updateRuleTokensPerSecond(rule: VideoPricingRule, value?: number) {
  if (!rule.estimator) return
	const match = estimatorEntries.value.find(([name]) => name.toLowerCase() === rule.estimator?.trim().toLowerCase())
	if (match) updateEstimator(match[0], { tokens_per_second: value })
}

function displayRulePrice(rule: VideoPricingRule): number | '' {
  if (!Number.isFinite(rule.unit_price_usd)) return ''
  return rule.billing_unit === 'video_token' ? tokenToMTok(rule.unit_price_usd) : rule.unit_price_usd
}

function updateRulePrice(index: number, raw: string) {
  const rule = config.value?.rules?.[index]
  if (!rule) return
  const parsed = rule.billing_unit === 'video_token' ? mTokToToken(raw) : Number(raw)
  if (parsed == null || Number.isNaN(parsed)) return
  updateRule(index, { unit_price_usd: parsed })
}

function duplicateRule(index: number) {
  replaceConfig((next) => {
    const source = next.rules?.[index]
    if (!source || !next.rules) return
    const copy = JSON.parse(JSON.stringify(source)) as VideoPricingRule
    copy.key = uniqueName(`${source.key}-copy`, next.rules.map((rule) => rule.key))
    next.rules.splice(index + 1, 0, copy)
  })
}

function removeRule(index: number) {
  replaceConfig((next) => { next.rules?.splice(index, 1) })
}

function updateCondition<K extends keyof VideoPricingConditions>(index: number, key: K, value: VideoPricingConditions[K]) {
  replaceConfig((next) => {
    const rule = next.rules?.[index]
    if (!rule) return
    const conditions: VideoPricingConditions = { ...(rule.conditions || {}) }
    if (value == null || (Array.isArray(value) && value.length === 0)) delete conditions[key]
    else conditions[key] = value
    rule.conditions = conditions
  })
}

function updateSecondCondition(index: number, values: string[]) {
  const seconds = values.map(Number).filter((value) => Number.isInteger(value) && value > 0)
  updateCondition(index, 'seconds', seconds)
}

function updateAudioCondition(index: number, value?: boolean) {
	replaceConfig((next) => {
		const rule = next.rules?.[index]
		if (!rule) return
		const conditions: VideoPricingConditions = { ...(rule.conditions || {}) }
		delete conditions.audio_enabled
		if (value == null) delete conditions.generate_audio
		else conditions.generate_audio = value
		rule.conditions = conditions
	})
}

function splitList(raw: string): string[] {
  return [...new Set(raw.split(',').map((value) => value.trim()).filter(Boolean))]
}

function inputValue(event: Event): string { return (event.target as HTMLInputElement).value }
function checkedValue(event: Event): boolean { return (event.target as HTMLInputElement).checked }

function isoDate(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

function dateTimeLocal(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function priceUnitLabel(unit: VideoBillingUnit): string {
  if (unit === 'video_token') return t('admin.modelPrices.video.perMillionVideoTokens')
  if (unit === 'second') return t('admin.modelPrices.video.perSecond')
  return t('admin.modelPrices.video.perRequest')
}

function ruleScopeSummary(rule: VideoPricingRule): string {
  const conditions = rule.conditions || {}
  const parts: string[] = []
  if (conditions.resolutions?.length) parts.push(conditions.resolutions.join(', '))
  else if (conditions.sizes?.length) parts.push(conditions.sizes.join(', '))
  if (conditions.request_modes?.length) parts.push(conditions.request_modes.join(', '))
  if (conditions.inference_modes?.length) parts.push(conditions.inference_modes.join(', '))
  if (conditions.input_has_video != null) {
    parts.push(t(conditions.input_has_video ? 'admin.modelPrices.video.withInputVideo' : 'admin.modelPrices.video.withoutInputVideo'))
  }
	const audio = videoAudioCondition(conditions)
	if (audio != null) {
		parts.push(t(audio ? 'admin.modelPrices.video.withAudio' : 'admin.modelPrices.video.withoutAudio'))
  }
  if (conditions.seconds?.length) parts.push(`${conditions.seconds.join(', ')}s`)
  if (conditions.operations?.length) parts.push(conditions.operations.join(', '))
  if (conditions.input_types?.length) parts.push(conditions.input_types.join(', '))
  if (conditions.qualities?.length) parts.push(conditions.qualities.join(', '))
  if (conditions.service_tiers?.length) parts.push(conditions.service_tiers.join(', '))
  if (rule.valid_from || rule.valid_until) parts.push(t('admin.modelPrices.video.limitedTime'))
  return parts.join(' / ') || t('admin.modelPrices.video.allRequests')
}

function listMatches(values: Array<string | number> | undefined, actual: string | number): boolean {
  if (!values?.length) return true
  return values.some((value) => String(value).toLowerCase() === String(actual).toLowerCase())
}

function ruleSpecificity(rule: VideoPricingRule): number {
  const conditions = rule.conditions || {}
  let specificity = [
		conditions.providers, conditions.operations, conditions.sizes, conditions.resolutions,
		conditions.input_types, conditions.request_modes, conditions.inference_modes,
		conditions.qualities, conditions.service_tiers,
	].filter((values) => values?.length).length
	if (conditions.seconds?.length) specificity++
	if (conditions.input_has_video != null) specificity++
	if (videoAudioCondition(conditions) != null) specificity++
	return specificity
}

function schedulePreview() {
  if (previewTimer) clearTimeout(previewTimer)
  previewTimer = setTimeout(() => void refreshPreview(), 250)
}

async function refreshPreview() {
  if (!config.value?.enabled || editorExperience.value !== 'advanced') {
    previewResponse.value = null
    previewRequestError.value = ''
    return
  }
  const sequence = ++previewSequence
  previewLoading.value = true
  previewRequestError.value = ''
  try {
    const response = await previewVideoPrice(prepareVideoPricingForSave(config.value), {
      operation: preview.operation,
      resolution: preview.resolution,
      size: preview.size,
      seconds: preview.seconds,
      input_type: preview.inputType,
      input_has_video: preview.inputHasVideo,
      generate_audio: preview.generateAudio,
      request_mode: preview.requestMode,
      inference_mode: preview.inferenceMode,
      quality: preview.quality,
      service_tier: preview.serviceTier,
    })
    if (sequence === previewSequence) previewResponse.value = response
  } catch {
    if (sequence === previewSequence) {
      previewResponse.value = null
      previewRequestError.value = t('admin.modelPrices.video.previewRequestFailed')
    }
  } finally {
    if (sequence === previewSequence) previewLoading.value = false
  }
}

const previewMessage = computed(() => {
  if (previewLoading.value) return t('admin.modelPrices.video.previewLoading')
  if (previewRequestError.value) return previewRequestError.value
  const result = previewResponse.value
  if (!result) return t('admin.modelPrices.video.previewDisabled')
  if (result.matched) {
    return t('admin.modelPrices.video.previewResult', {
      rule: result.rule_key || '-',
      units: result.estimated_units || 0,
      cost: result.estimated_cost || 0,
    })
  }
  if (result.error_code === 'video_pricing_ambiguous') return t('admin.modelPrices.video.previewAmbiguous')
  if (result.error_code === 'video_pricing_estimator_missing') return t('admin.modelPrices.video.previewEstimatorMissing')
  if (result.error_code === 'video_pricing_resolution_missing') return t('admin.modelPrices.video.previewResolutionMissing')
  if (result.error_code === 'video_source_spec_unavailable') return t('admin.modelPrices.video.previewSourceSpecUnavailable')
  return t('admin.modelPrices.video.previewNoRule')
})

function previewValue(value: unknown): string {
  if (value == null || value === '') return '-'
  if (Array.isArray(value)) return value.map(String).join(', ')
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

const validationErrors = computed(() => {
	const raw = config.value
	const value = raw ? prepareVideoPricingForSave(raw) : null
	if (!value?.enabled) return []
	const errors: string[] = []
	const resolutionNames = new Set(Object.keys(value.resolutions || {}).map((name) => name.trim().toLowerCase()))
	const estimatorNames = new Set(Object.keys(value.estimators || {}).map((name) => name.trim().toLowerCase()))
	const declaredSizes = new Set<string>()
	if (!value.rules?.length) errors.push(t('admin.modelPrices.video.validationRuleRequired'))
	for (const [name, spec] of Object.entries(value.resolutions || {})) {
		if (!name.trim() || !spec.sizes.length) errors.push(t('admin.modelPrices.video.validationResolution', { name: name || '-' }))
		for (const size of spec.sizes || []) {
			const normalized = normalizeVideoSize(size)
			if (!normalized) errors.push(t('admin.modelPrices.video.validationResolutionSize', { name: name || '-', size }))
			else if (declaredSizes.has(normalized)) errors.push(t('admin.modelPrices.video.validationDuplicateSize', { size }))
			else declaredSizes.add(normalized)
		}
	}
	if (value.defaults?.resolution && !resolutionNames.has(value.defaults.resolution.trim().toLowerCase())) {
		errors.push(t('admin.modelPrices.video.validationDefaultResolution'))
	}
	for (const [name, estimator] of Object.entries(value.estimators || {})) {
		const invalid = !isCompleteVideoEstimator(estimator)
		if (!name.trim() || invalid || (estimator.minimum_units || []).some((minimum) => !positive(minimum.units))) {
			errors.push(t('admin.modelPrices.video.validationEstimatorConfig', { name: name || '-' }))
		}
	}
	const keys = new Set<string>()
	for (const rule of value.rules || []) {
		const key = rule.key.trim().toLowerCase()
		if (!key || keys.has(key)) errors.push(t('admin.modelPrices.video.validationRuleKey', { key: rule.key || '-' }))
		keys.add(key)
		if (!Number.isFinite(rule.unit_price_usd) || rule.unit_price_usd < 0) errors.push(t('admin.modelPrices.video.validationPrice', { key: rule.key || '-' }))
		if (rule.billing_unit === 'video_token' && (!rule.estimator || !estimatorNames.has(rule.estimator.trim().toLowerCase()))) errors.push(t('admin.modelPrices.video.validationEstimator', { key: rule.key || '-' }))
		if ((rule.conditions?.resolutions || []).some((resolution) => !resolutionNames.has(resolution.trim().toLowerCase()))) errors.push(t('admin.modelPrices.video.validationConditionReference', { key: rule.key || '-' }))
		if ((rule.conditions?.sizes || []).some((size) => !normalizeVideoSize(size) || !declaredSizes.has(normalizeVideoSize(size)!))) errors.push(t('admin.modelPrices.video.validationConditionSize', { key: rule.key || '-' }))
		if (rule.conditions?.generate_audio != null && rule.conditions.audio_enabled != null && rule.conditions.generate_audio !== rule.conditions.audio_enabled) errors.push(t('admin.modelPrices.video.validationAudioConflict', { key: rule.key || '-' }))
		if (rule.valid_from && rule.valid_until && new Date(rule.valid_from).getTime() >= new Date(rule.valid_until).getTime()) errors.push(t('admin.modelPrices.video.validationWindow', { key: rule.key || '-' }))
	}
	for (let left = 0; left < (value.rules || []).length; left++) {
		for (let right = left + 1; right < (value.rules || []).length; right++) {
			const leftRule = value.rules![left]
			const rightRule = value.rules![right]
			if ((leftRule.priority || 0) === (rightRule.priority || 0) &&
				ruleSpecificity(leftRule) === ruleSpecificity(rightRule) &&
				ruleWindowsOverlap(leftRule, rightRule) &&
				ruleConditionsOverlap(leftRule.conditions || {}, rightRule.conditions || {}, value)) {
				errors.push(t('admin.modelPrices.video.validationRuleOverlap', { left: leftRule.key, right: rightRule.key }))
			}
		}
	}
	return [...new Set(errors)]
})

watch(validationErrors, (errors) => emit('validation-change', errors), { immediate: true })

function positive(value?: number): boolean {
	return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function normalizeVideoSize(value: string): string | undefined {
	const match = /^([1-9][0-9]{0,5})[xX]([1-9][0-9]{0,5})$/.exec(value.trim())
	return match ? `${match[1]}x${match[2]}` : undefined
}

function setsOverlap(left: Array<string | number> | undefined, right: Array<string | number> | undefined): boolean {
	if (!left?.length || !right?.length) return true
	const values = new Set(left.map((value) => String(value).trim().toLowerCase()))
	return right.some((value) => values.has(String(value).trim().toLowerCase()))
}

function booleansOverlap(left?: boolean, right?: boolean): boolean {
	return left == null || right == null || left === right
}

function videoAudioCondition(conditions: VideoPricingConditions): boolean | undefined {
	return conditions.generate_audio ?? conditions.audio_enabled
}

function ruleWindowsOverlap(left: VideoPricingRule, right: VideoPricingRule): boolean {
	if (left.valid_until && right.valid_from && new Date(right.valid_from) >= new Date(left.valid_until)) return false
	if (right.valid_until && left.valid_from && new Date(left.valid_from) >= new Date(right.valid_until)) return false
	return true
}

function sizeResolutionOverlap(left: VideoPricingConditions, right: VideoPricingConditions, value: VideoPricingConfig): boolean {
	if (!left.sizes?.length && !right.sizes?.length && !left.resolutions?.length && !right.resolutions?.length) return true
	if (!value.resolutions || Object.keys(value.resolutions).length === 0) {
		return setsOverlap(left.sizes, right.sizes) && setsOverlap(left.resolutions, right.resolutions)
	}
	for (const [resolution, spec] of Object.entries(value.resolutions)) {
		if (!listMatches(left.resolutions, resolution) || !listMatches(right.resolutions, resolution)) continue
		if (spec.sizes.some((size) => listMatches(left.sizes, size) && listMatches(right.sizes, size))) return true
	}
	return false
}

function ruleConditionsOverlap(left: VideoPricingConditions, right: VideoPricingConditions, value: VideoPricingConfig): boolean {
	return setsOverlap(left.providers, right.providers) &&
		setsOverlap(left.operations, right.operations) &&
		setsOverlap(left.input_types, right.input_types) &&
		setsOverlap(left.request_modes, right.request_modes) &&
		setsOverlap(left.inference_modes, right.inference_modes) &&
		setsOverlap(left.qualities, right.qualities) &&
		setsOverlap(left.service_tiers, right.service_tiers) &&
		setsOverlap(left.seconds, right.seconds) &&
		booleansOverlap(left.input_has_video, right.input_has_video) &&
		booleansOverlap(videoAudioCondition(left), videoAudioCondition(right)) &&
		sizeResolutionOverlap(left, right, value)
}
</script>

<style scoped>
.mode-btn {
  @apply min-h-9 border border-gray-300 bg-white px-3 text-sm text-gray-600 transition-colors first:rounded-l last:rounded-r hover:bg-gray-50 dark:border-dark-500 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600;
}
.mode-btn-active {
  @apply border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/30 dark:text-primary-200;
}
.experience-tab {
  @apply min-h-10 border-b-2 border-transparent px-4 text-sm text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white;
}
.experience-tab-active {
  @apply border-primary-500 font-medium text-primary-600 dark:text-primary-300;
}
.advanced-block {
  @apply rounded-md border border-gray-200 bg-gray-50/40 px-3 py-2 dark:border-dark-600 dark:bg-dark-800/30;
}
.advanced-block > summary,
.rule-details > summary {
  @apply cursor-pointer select-none text-sm font-medium text-gray-700 dark:text-gray-200;
}
.rule-details {
  @apply mt-3 border-t border-gray-200 pt-3 dark:border-dark-600;
}
.section-block {
  @apply border-t border-gray-200 pt-4 dark:border-dark-600;
}
.section-header {
  @apply mb-3 flex items-center justify-between gap-3;
}
.section-title {
  @apply text-sm font-semibold text-gray-900 dark:text-white;
}
.field-label {
  @apply text-xs font-medium text-gray-600 dark:text-gray-300;
}
.field-label > span {
  @apply mb-1 block;
}
.editor-row {
  @apply items-end py-3;
}
.repeated-item {
  @apply rounded-md border border-gray-200 bg-gray-50/60 p-3 dark:border-dark-600 dark:bg-dark-800/40;
}
.empty-line {
  @apply py-3 text-sm text-gray-500 dark:text-gray-400;
}
.icon-btn {
  @apply flex h-10 w-10 items-center justify-center rounded text-gray-500 hover:bg-gray-100 hover:text-gray-800 dark:hover:bg-dark-600 dark:hover:text-gray-100;
}
.icon-btn-danger {
  @apply hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-300;
}
</style>
