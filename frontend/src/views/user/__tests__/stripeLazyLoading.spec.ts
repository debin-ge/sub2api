import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { manualChunkName } from '../../../../build-tools/manual-chunks'

const frontendRoot = resolve(__dirname, '../../../..')
const stripeConsumers = [
  'src/views/user/StripePaymentView.vue',
  'src/views/user/StripePopupView.vue',
  'src/components/payment/StripePaymentInline.vue',
]

function readFrontendFile(path: string): string {
  return readFileSync(resolve(frontendRoot, path), 'utf8')
}

describe('Stripe lazy-loading contract', () => {
  it.each(stripeConsumers)('%s uses the side-effect-free Stripe loader', (path) => {
    const source = readFrontendFile(path)

    expect(source).toContain("await import('@stripe/stripe-js/pure')")
    expect(source).not.toMatch(/await import\(['"]@stripe\/stripe-js['"]\)/)
  })

  it('keeps Stripe out of the shared vendor chunk', () => {
    const viteConfig = readFrontendFile('vite.config.ts')
    expect(viteConfig).toContain('manualChunks: manualChunkName')
    expect(manualChunkName('/repo/node_modules/@stripe/stripe-js/dist/index.mjs'))
      .toBe('vendor-payment-stripe')
    expect(manualChunkName('/repo/node_modules/axios/index.js')).toBe('vendor-misc')
  })
})
