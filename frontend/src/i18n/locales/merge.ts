type LocaleTree = Record<string, unknown>

function isLocaleTree(value: unknown): value is LocaleTree {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

/** Fill keys missing from the upstream locale without overriding upstream messages. */
export function mergeLocaleFallbacks(primary: LocaleTree, fallback: LocaleTree): LocaleTree {
  const result: LocaleTree = { ...primary }
  for (const [key, fallbackValue] of Object.entries(fallback)) {
    const primaryValue = result[key]
    if (primaryValue === undefined) {
      result[key] = fallbackValue
    } else if (isLocaleTree(primaryValue) && isLocaleTree(fallbackValue)) {
      result[key] = mergeLocaleFallbacks(primaryValue, fallbackValue)
    }
  }
  return result
}
