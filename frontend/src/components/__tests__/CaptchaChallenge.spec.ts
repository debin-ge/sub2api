import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'

import CaptchaChallenge from '../CaptchaChallenge.vue'

function widgetStub(name: string, verifyResult: unknown = null) {
  return defineComponent({
    name,
    // 桩件不声明 props，透传到根 div 上的 prefix 等属性会触发 DOM 告警
    inheritAttrs: false,
    setup(_, { expose }) {
      expose({
        reset: () => undefined,
        verify: () => Promise.resolve(verifyResult)
      })
      return () => h('div', { 'data-testid': name })
    }
  })
}

const stubs = {
  TurnstileWidget: widgetStub('TurnstileWidget'),
  TencentCaptchaGate: widgetStub('TencentCaptchaGate', { ticket: 'ticket', randstr: 'rand' }),
  AliyunCaptchaWidget: widgetStub('AliyunCaptchaWidget', 'aliyun-param'),
  GoCaptchaWidget: widgetStub('GoCaptchaWidget', 'gocaptcha-token')
}

const disabledProps = {
  turnstileEnabled: false,
  turnstileSiteKey: '',
  tencentEnabled: false,
  tencentAppId: '',
  aliyunEnabled: false,
  aliyunSceneId: '',
  aliyunPrefix: '',
  goCaptchaEnabled: false
}

function mountChallenge(props: Record<string, unknown>) {
  return mount(CaptchaChallenge, {
    props: { ...disabledProps, ...props },
    global: { stubs }
  })
}

describe('CaptchaChallenge provider selection', () => {
  it('renders no widget when every provider is disabled', () => {
    const wrapper = mountChallenge({})

    for (const name of Object.keys(stubs)) {
      expect(wrapper.find(`[data-testid="${name}"]`).exists()).toBe(false)
    }
  })

  it.each([
    ['TurnstileWidget', { turnstileEnabled: true, turnstileSiteKey: 'site' }],
    ['TencentCaptchaGate', { tencentEnabled: true, tencentAppId: 'app' }],
    [
      'AliyunCaptchaWidget',
      { aliyunEnabled: true, aliyunSceneId: 'scene', aliyunPrefix: 'prefix' }
    ],
    ['GoCaptchaWidget', { goCaptchaEnabled: true }]
  ])('renders only %s when that provider is enabled', (expected, props) => {
    const wrapper = mountChallenge(props)

    for (const name of Object.keys(stubs)) {
      expect(wrapper.find(`[data-testid="${name}"]`).exists()).toBe(name === expected)
    }
  })

  it('does not render the self-hosted widget while another provider wins the chain', () => {
    // 后台已经保证四选一，这里守住渲染侧的兜底顺序不被后续改动破坏
    const wrapper = mountChallenge({
      turnstileEnabled: true,
      turnstileSiteKey: 'site',
      goCaptchaEnabled: true
    })

    expect(wrapper.find('[data-testid="TurnstileWidget"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="GoCaptchaWidget"]').exists()).toBe(false)
  })

  it('verifyAction returns the self-hosted token with an empty randstr', async () => {
    const wrapper = mountChallenge({ goCaptchaEnabled: true })

    const result = await (
      wrapper.vm as unknown as {
        verifyAction: () => Promise<{ token: string; randstr: string } | null>
      }
    ).verifyAction()

    expect(result).toEqual({ token: 'gocaptcha-token', randstr: '' })
  })

  it('verifyAction returns null when no action provider is enabled', async () => {
    const wrapper = mountChallenge({ turnstileEnabled: true, turnstileSiteKey: 'site' })

    const result = await (
      wrapper.vm as unknown as { verifyAction: () => Promise<unknown> }
    ).verifyAction()

    expect(result).toBeNull()
  })
})
