import type {
  VideoBillingUnit,
  VideoPricingConditions,
  VideoPricingConfig,
  VideoPricingRule,
} from '@/api/admin/modelPrices'

const SIMPLE_ESTIMATOR = 'default'

export interface SimpleVideoPricingProfile {
  billingUnit: VideoBillingUnit
  standardEnabled: boolean
  standardUnitPriceUsd: number
  referenceEnabled: boolean
  referenceSameAsStandard: boolean
  referenceUnitPriceUsd: number
  tokensPerSecond?: number
}

function normalizedStrings(values?: string[]): string[] {
  return (values || []).map((value) => value.trim().toLowerCase()).filter(Boolean).sort()
}

function matchesOnly(values: string[] | undefined, allowed: string[]): boolean {
  const normalized = normalizedStrings(values)
  return normalized.length === 0 || (
    normalized.length === allowed.length && normalized.every((value, index) => value === allowed[index])
  )
}

function isSimpleConditions(conditions: VideoPricingConditions = {}): boolean {
  const allowedKeys = new Set(['operations', 'input_has_video', 'request_modes', 'inference_modes'])
  if (Object.keys(conditions).some((key) => !allowedKeys.has(key))) return false
  return matchesOnly(conditions.operations, ['generate']) &&
    matchesOnly(conditions.request_modes, ['standard']) &&
    matchesOnly(conditions.inference_modes, ['online'])
}

function simpleConditions(inputHasVideo: boolean): VideoPricingConditions {
  return {
    operations: ['generate'],
    input_has_video: inputHasVideo,
    request_modes: ['standard'],
    inference_modes: ['online'],
  }
}

function simpleRule(
  key: string,
  billingUnit: VideoBillingUnit,
  unitPriceUsd: number,
  inputHasVideo: boolean,
): VideoPricingRule {
  return {
    key,
    billing_unit: billingUnit,
    unit_price_usd: unitPriceUsd,
    ...(billingUnit === 'video_token' ? { estimator: SIMPLE_ESTIMATOR } : {}),
    conditions: simpleConditions(inputHasVideo),
  }
}

export function buildSimpleVideoPricingConfig(profile: SimpleVideoPricingProfile): VideoPricingConfig {
  const rules: VideoPricingRule[] = []
  if (profile.standardEnabled) {
    rules.push(simpleRule('standard', profile.billingUnit, profile.standardUnitPriceUsd, false))
  }
  if (profile.referenceEnabled) {
    const price = profile.referenceSameAsStandard && profile.standardEnabled
      ? profile.standardUnitPriceUsd
      : profile.referenceUnitPriceUsd
    rules.push(simpleRule('reference-video', profile.billingUnit, price, true))
  }
  return {
    version: 1,
    enabled: true,
    currency: 'USD',
    defaults: {
      request_mode: 'standard',
      inference_mode: 'online',
      generate_audio: false,
    },
    ...(profile.billingUnit === 'video_token'
      ? {
          estimators: {
            [SIMPLE_ESTIMATOR]: {
              type: 'fixed_tokens_per_second' as const,
              tokens_per_second: profile.tokensPerSecond,
            },
          },
        }
      : {}),
    rules,
  }
}

function ruleIsSimple(rule: VideoPricingRule, billingUnit: VideoBillingUnit, estimator?: string): boolean {
  if (rule.billing_unit !== billingUnit || !isSimpleConditions(rule.conditions || {})) return false
  if ((rule.priority || 0) !== 0 || rule.valid_from || rule.valid_until) return false
  if (billingUnit === 'video_token') return rule.estimator === estimator
  return !rule.estimator
}

export function parseSimpleVideoPricingConfig(config: VideoPricingConfig | null | undefined): SimpleVideoPricingProfile | null {
  if (!config?.enabled || config.version !== 1 || config.currency !== 'USD') return null
  if (Object.keys(config.resolutions || {}).length > 0) return null
  if (config.defaults?.resolution || config.defaults?.generate_audio) return null
  if (!matchesOnly(config.defaults?.request_mode ? [config.defaults.request_mode] : [], ['standard'])) return null
  if (!matchesOnly(config.defaults?.inference_mode ? [config.defaults.inference_mode] : [], ['online'])) return null
  if (!config.rules?.length || config.rules.length > 2) return null

  const billingUnit = config.rules[0].billing_unit
  if (!config.rules.every((rule) => ruleIsSimple(rule, billingUnit, config.rules![0].estimator))) return null

  let tokensPerSecond: number | undefined
  if (billingUnit === 'video_token') {
    const estimatorName = config.rules[0].estimator
    const estimator = estimatorName ? config.estimators?.[estimatorName] : undefined
    if (!estimator || estimator.type !== 'fixed_tokens_per_second') return null
    if (Object.keys(config.estimators || {}).length !== 1) return null
    tokensPerSecond = estimator.tokens_per_second
  } else if (Object.keys(config.estimators || {}).length > 0) {
    return null
  }

  let standardRule: VideoPricingRule | undefined
  let referenceRule: VideoPricingRule | undefined
  let duplicateScope = false
  for (const rule of config.rules) {
    if (rule.conditions?.input_has_video === true) {
      if (referenceRule) duplicateScope = true
      referenceRule = rule
    } else if (rule.conditions?.input_has_video === false) {
      if (standardRule) duplicateScope = true
      standardRule = rule
    }
    else {
      if (standardRule || referenceRule || config.rules.length !== 1) duplicateScope = true
      standardRule = rule
      referenceRule = rule
    }
  }
  if (duplicateScope || (!standardRule && !referenceRule)) return null

  const standardPrice = standardRule?.unit_price_usd ?? 0
  const referencePrice = referenceRule?.unit_price_usd ?? standardPrice
  return {
    billingUnit,
    standardEnabled: Boolean(standardRule),
    standardUnitPriceUsd: standardPrice,
    referenceEnabled: Boolean(referenceRule),
    referenceSameAsStandard: Boolean(standardRule && referenceRule && standardPrice === referencePrice),
    referenceUnitPriceUsd: referencePrice,
    tokensPerSecond,
  }
}

export function suggestSimpleVideoPricingProfile(config: VideoPricingConfig): SimpleVideoPricingProfile {
  const firstRule = config.rules?.[0]
  const billingUnit = firstRule?.billing_unit || 'video_token'
  const standardRule = config.rules?.find((rule) => rule.conditions?.input_has_video !== true)
  const referenceRule = config.rules?.find((rule) => rule.conditions?.input_has_video === true)
  const fallbackRule = standardRule || referenceRule || firstRule
  const estimatorName = fallbackRule?.estimator
  const estimator = estimatorName ? config.estimators?.[estimatorName] : undefined
  const standardPrice = standardRule?.unit_price_usd ?? fallbackRule?.unit_price_usd ?? 0
  const referencePrice = referenceRule?.unit_price_usd ?? standardPrice
  return {
    billingUnit,
    standardEnabled: Boolean(standardRule) || !referenceRule,
    standardUnitPriceUsd: standardPrice,
    referenceEnabled: Boolean(referenceRule),
    referenceSameAsStandard: Boolean(standardRule && referenceRule && standardPrice === referencePrice),
    referenceUnitPriceUsd: referencePrice,
    tokensPerSecond: estimator?.type === 'fixed_tokens_per_second' ? estimator.tokens_per_second : undefined,
  }
}
