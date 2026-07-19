import { describe, expect, it } from 'vitest'

import { manualChunkName } from '../../build-tools/manual-chunks'

describe('manualChunkName', () => {
  it('isolates provider SDKs from the eagerly loaded shared vendor chunk', () => {
    expect(manualChunkName('/repo/node_modules/@stripe/stripe-js/dist/index.mjs')).toBe('vendor-payment-stripe')
    expect(manualChunkName('/repo/node_modules/@airwallex/components-sdk/dist/index.js')).toBe('vendor-payment-airwallex')
    expect(manualChunkName('/repo/node_modules/@airwallex/airtracker/dist/index.js')).toBe('vendor-payment-airwallex')
  })

  it('handles pnpm and Windows module paths', () => {
    expect(manualChunkName('/repo/node_modules/.pnpm/@stripe+stripe-js@9/node_modules/@stripe/stripe-js/dist/index.mjs'))
      .toBe('vendor-payment-stripe')
    expect(manualChunkName('C:\\repo\\node_modules\\@airwallex\\components-sdk\\dist\\index.js'))
      .toBe('vendor-payment-airwallex')
  })

  it('preserves the existing shared chunk categories', () => {
    expect(manualChunkName('/repo/node_modules/vue/dist/vue.runtime.esm.js')).toBe('vendor-vue')
    expect(manualChunkName('/repo/node_modules/axios/index.js')).toBe('vendor-misc')
    expect(manualChunkName('/repo/src/main.ts')).toBeUndefined()
  })
})
