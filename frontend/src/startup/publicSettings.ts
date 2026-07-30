type PublicSettingsBootstrapStore = {
  initFromInjectedConfig: () => boolean
  fetchPublicSettings: (force?: boolean) => Promise<unknown>
}

/**
 * Populate branding before the router performs its initial navigation.
 *
 * Server-injected settings are synchronous. When injection is unavailable, wait
 * for the regular public-settings request so route titles and page components do
 * not render the built-in brand first.
 */
export async function initializePublicSettings(
  appStore: PublicSettingsBootstrapStore
): Promise<void> {
  const hasInjectedSettings = appStore.initFromInjectedConfig()
  const injectedSettingsAreStale = window.__APP_CONFIG_STALE__ === true

  if (hasInjectedSettings && !injectedSettingsAreStale) {
    return
  }

  try {
    await appStore.fetchPublicSettings(injectedSettingsAreStale)
  } catch (error) {
    // The store normally converts request failures to null. Keep bootstrap
    // resilient if an unexpected implementation error escapes that boundary.
    console.error('Failed to initialize public settings before mount:', error)
  }
}
