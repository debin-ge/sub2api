import { describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

import { useCaptcha, type UseCaptchaReturn } from '../useCaptcha'
import type { PublicSettings } from '@/types'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

function withCaptcha(onErrorMessage?: (message: string) => void): UseCaptchaReturn {
  let captured!: UseCaptchaReturn
  mount(
    defineComponent({
      setup() {
        captured = useCaptcha(onErrorMessage)
        return () => null
      }
    })
  )
  return captured
}

function settings(overrides: Partial<PublicSettings>): PublicSettings {
  return { turnstile_enabled: false, turnstile_site_key: '', ...overrides } as PublicSettings
}

describe('useCaptcha', () => {
  it('starts with every provider disabled', () => {
    const captcha = withCaptcha()

    expect(captcha.captchaEnabled.value).toBe(false)
    expect(captcha.actionCaptchaEnabled.value).toBe(false)
  })

  it('treats Turnstile as inline and the other three as action-triggered', () => {
    const turnstile = withCaptcha()
    turnstile.applyPublicSettings(settings({ turnstile_enabled: true, turnstile_site_key: 'site' }))
    expect(turnstile.actionCaptchaEnabled.value).toBe(false)
    expect(turnstile.inlineCaptchaPending.value).toBe(true)

    for (const provider of [
      settings({ tencent_captcha_enabled: true, tencent_captcha_app_id: 'app' }),
      settings({
        aliyun_captcha_enabled: true,
        aliyun_captcha_scene_id: 'scene',
        aliyun_captcha_prefix: 'prefix'
      }),
      settings({ gocaptcha_enabled: true, gocaptcha_mode: 'slide' })
    ]) {
      const captcha = withCaptcha()
      captcha.applyPublicSettings(provider)
      expect(captcha.actionCaptchaEnabled.value).toBe(true)
      expect(captcha.inlineCaptchaPending.value).toBe(false)
      expect(captcha.captchaEnabled.value).toBe(true)
    }
  })

  it('stays disabled when a provider is on but its config is incomplete', () => {
    const captcha = withCaptcha()

    captcha.applyPublicSettings(settings({ turnstile_enabled: true, turnstile_site_key: '' }))
    expect(captcha.captchaEnabled.value).toBe(false)

    captcha.applyPublicSettings(settings({ aliyun_captcha_enabled: true, aliyun_captcha_scene_id: 's' }))
    expect(captcha.captchaEnabled.value).toBe(false)
  })

  it('maps the self-hosted token onto turnstile_token, matching the Aliyun convention', () => {
    const captcha = withCaptcha()
    captcha.applyPublicSettings(settings({ gocaptcha_enabled: true }))
    captcha.onVerify('gocaptcha-token')

    expect(captcha.requestPayload()).toEqual({
      turnstile_token: 'gocaptcha-token',
      tencent_captcha_ticket: undefined,
      tencent_captcha_randstr: undefined
    })
  })

  it('maps Tencent proofs onto the ticket and randstr fields', () => {
    const captcha = withCaptcha()
    captcha.applyPublicSettings(
      settings({ tencent_captcha_enabled: true, tencent_captcha_app_id: 'app' })
    )
    captcha.onVerify('ticket', 'rand')

    expect(captcha.requestPayload()).toEqual({
      turnstile_token: undefined,
      tencent_captcha_ticket: 'ticket',
      tencent_captcha_randstr: 'rand'
    })
  })

  it('normalises an empty token to undefined instead of an empty string', () => {
    const captcha = withCaptcha()
    captcha.applyPublicSettings(settings({ gocaptcha_enabled: true }))

    expect(captcha.requestPayload().turnstile_token).toBeUndefined()
  })

  it('omits emit fields entirely when no proof was obtained', () => {
    const captcha = withCaptcha()
    captcha.applyPublicSettings(settings({ gocaptcha_enabled: true }))

    expect(captcha.emitPayload()).toEqual({})

    captcha.onVerify('token')
    expect(captcha.emitPayload()).toEqual({ turnstileToken: 'token' })
  })

  it('reports expiry and failure through the error callback and clears the proof', () => {
    const messages: string[] = []
    const captcha = withCaptcha((message) => messages.push(message))
    captcha.applyPublicSettings(settings({ turnstile_enabled: true, turnstile_site_key: 'site' }))

    captcha.onVerify('token')
    expect(captcha.token.value).toBe('token')

    captcha.onExpire()
    expect(captcha.token.value).toBe('')
    expect(messages).toEqual(['', 'auth.turnstileExpired'])

    captcha.onError()
    expect(messages.at(-1)).toBe('auth.turnstileFailed')
  })

  it('gives each proof slot independent state while sharing provider config', () => {
    const captcha = withCaptcha()
    captcha.applyPublicSettings(settings({ gocaptcha_enabled: true }))
    const second = captcha.createProofSlot()

    captcha.onVerify('first-token')
    second.onVerify('second-token')

    expect(captcha.requestPayload().turnstile_token).toBe('first-token')
    expect(second.requestPayload().turnstile_token).toBe('second-token')
  })

  it('acquireActionProof short-circuits when only Turnstile is enabled', async () => {
    const captcha = withCaptcha()
    captcha.applyPublicSettings(settings({ turnstile_enabled: true, turnstile_site_key: 'site' }))

    await expect(captcha.acquireActionProof()).resolves.toBe(true)
  })

  it('resetPublicSettings turns every provider back off', () => {
    const captcha = withCaptcha()
    captcha.applyPublicSettings(settings({ gocaptcha_enabled: true, gocaptcha_mode: 'rotate' }))
    expect(captcha.captchaEnabled.value).toBe(true)

    captcha.resetPublicSettings()

    expect(captcha.captchaEnabled.value).toBe(false)
    expect(captcha.goCaptchaMode.value).toBe('click')
  })

  it('exposes the full CaptchaChallenge prop set for v-bind', () => {
    const captcha = withCaptcha()
    captcha.applyPublicSettings(settings({ gocaptcha_enabled: true, gocaptcha_mode: 'drag' }))

    expect(captcha.captchaProps.value).toMatchObject({
      turnstileEnabled: false,
      tencentEnabled: false,
      aliyunEnabled: false,
      goCaptchaEnabled: true,
      goCaptchaMode: 'drag'
    })
  })

  it('toActionProof picks the field layout the OAuth start endpoint expects', () => {
    const goCaptcha = withCaptcha()
    goCaptcha.applyPublicSettings(settings({ gocaptcha_enabled: true }))
    expect(goCaptcha.toActionProof({ token: 'tok', randstr: '' })).toEqual({
      turnstile_token: 'tok'
    })

    const tencent = withCaptcha()
    tencent.applyPublicSettings(
      settings({ tencent_captcha_enabled: true, tencent_captcha_app_id: 'app' })
    )
    expect(tencent.toActionProof({ token: 'ticket', randstr: 'rand' })).toEqual({
      tencent_captcha_ticket: 'ticket',
      tencent_captcha_randstr: 'rand'
    })
  })
})
