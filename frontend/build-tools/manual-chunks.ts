export function manualChunkName(rawId: string): string | undefined {
  const id = rawId.replace(/\\/g, '/')
  if (!id.includes('/node_modules/')) return undefined

  // These SDK entry modules inject third-party scripts as soon as they execute.
  // Keep them out of the shared vendor chunk so route-level dynamic imports stay lazy.
  if (id.includes('/@stripe/stripe-js/')) return 'vendor-payment-stripe'
  if (
    id.includes('/@airwallex/components-sdk/') ||
    id.includes('/@airwallex/airtracker/')
  ) {
    return 'vendor-payment-airwallex'
  }

  if (
    id.includes('/vue/') ||
    id.includes('/vue-router/') ||
    id.includes('/pinia/') ||
    id.includes('/@vue/')
  ) {
    return 'vendor-vue'
  }

  if (id.includes('/@vueuse/') || id.includes('/xlsx/')) return 'vendor-ui'
  if (id.includes('/chart.js/') || id.includes('/vue-chartjs/')) return 'vendor-chart'
  if (id.includes('/vue-i18n/') || id.includes('/@intlify/')) return 'vendor-i18n'
  return 'vendor-misc'
}
