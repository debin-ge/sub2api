import type { VideoMinimumUnitsRule, VideoPricingConfig, VideoUsageEstimator } from '@/api/admin/modelPrices'

function cloneConfig(value: VideoPricingConfig): VideoPricingConfig {
  return JSON.parse(JSON.stringify(value)) as VideoPricingConfig
}

export function isCompleteVideoEstimator(estimator: VideoUsageEstimator): boolean {
  const positive = (value?: number) => typeof value === 'number' && Number.isFinite(value) && value > 0
  if (estimator.type === 'pixel_frame') {
    return (estimator.token_scope === 'output_only' || estimator.token_scope === 'input_plus_output') &&
      positive(estimator.fps) &&
      positive(estimator.divisor) &&
      (estimator.token_scope !== 'input_plus_output' || positive(estimator.max_input_video_seconds))
  }
  if (estimator.type === 'fixed_tokens_per_second') return positive(estimator.tokens_per_second)
  if (estimator.type === 'fixed_max_units') return positive(estimator.max_units)
  return false
}

function hasMinimumConditions(rule: VideoMinimumUnitsRule): boolean {
	return Boolean(rule.conditions && Object.keys(rule.conditions).length > 0)
}

export function unconditionalMinimumUnits(estimator: VideoUsageEstimator): number | undefined {
	return estimator.minimum_units?.find((rule) => !hasMinimumConditions(rule))?.units
}

export function setUnconditionalMinimumUnits(estimator: VideoUsageEstimator, units?: number): VideoUsageEstimator {
	const next = cloneConfig(estimator as unknown as VideoPricingConfig) as unknown as VideoUsageEstimator
	const minimumUnits = next.minimum_units || []
	const index = minimumUnits.findIndex((rule) => !hasMinimumConditions(rule))
	if (units == null) {
		if (index >= 0) minimumUnits.splice(index, 1)
	} else if (index >= 0) {
		minimumUnits[index] = { ...minimumUnits[index], units }
	} else {
		minimumUnits.unshift({ units })
	}
	next.minimum_units = minimumUnits
	return next
}

/**
 * Drop only unfinished, unreferenced optional rows. They are editor drafts and
 * must not make an otherwise valid profile impossible to save.
 */
export function prepareVideoPricingForSave(value: VideoPricingConfig): VideoPricingConfig {
  const next = cloneConfig(value)
  const referencedResolutions = new Set<string>()
  if (next.defaults?.resolution) referencedResolutions.add(next.defaults.resolution.trim().toLowerCase())
  for (const rule of next.rules || []) {
    for (const resolution of rule.conditions?.resolutions || []) {
      referencedResolutions.add(resolution.trim().toLowerCase())
    }
  }
  for (const [name, spec] of Object.entries(next.resolutions || {})) {
    if (!spec.sizes?.length && !referencedResolutions.has(name.trim().toLowerCase())) delete next.resolutions?.[name]
  }

  const referencedEstimators = new Set(
    (next.rules || []).map((rule) => rule.estimator?.trim().toLowerCase()).filter((name): name is string => Boolean(name)),
  )
  for (const [name, estimator] of Object.entries(next.estimators || {})) {
    if (!isCompleteVideoEstimator(estimator) && !referencedEstimators.has(name.trim().toLowerCase())) delete next.estimators?.[name]
  }

  if (next.resolutions && Object.keys(next.resolutions).length === 0) delete next.resolutions
  if (next.estimators && Object.keys(next.estimators).length === 0) delete next.estimators
  for (const rule of next.rules || []) {
    if (rule.conditions && Object.keys(rule.conditions).length === 0) delete rule.conditions
  }
  return next
}
