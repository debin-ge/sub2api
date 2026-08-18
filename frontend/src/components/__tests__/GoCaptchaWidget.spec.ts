import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'

import GoCaptchaWidget from '../GoCaptchaWidget.vue'
import type { GoCaptchaChallenge, GoCaptchaMode } from '@/api/auth'

const { createChallengeMock, verifyMock } = vi.hoisted(() => ({
  createChallengeMock: vi.fn(),
  verifyMock: vi.fn()
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/api/auth', () => ({
  createGoCaptchaChallenge: (...args: unknown[]) => createChallengeMock(...args),
  verifyGoCaptcha: (...args: unknown[]) => verifyMock(...args)
}))

// go-captcha-vue 的真实组件依赖 canvas 与拖拽事件，测试里替换成能直接触发
// confirm 回调的桩件，聚焦在本组件的编排逻辑上。
function createPanelStub(name: string, confirmArg: unknown) {
  return defineComponent({
    name,
    props: {
      data: { type: Object, required: false },
      events: { type: Object, required: false },
      config: { type: Object, required: false }
    },
    setup(props) {
      return () =>
        h(
          'button',
          {
            'data-testid': `${name}-confirm`,
            'data-title': (props.config as { title?: string } | undefined)?.title,
            'data-button-text': (props.config as { buttonText?: string } | undefined)?.buttonText,
            'data-width': String((props.config as { width?: number } | undefined)?.width ?? ''),
            onClick: () =>
              (props.events as { confirm?: (arg: unknown, reset: () => void) => void })?.confirm?.(
                confirmArg,
                () => undefined
              )
          },
          name
        )
    }
  })
}

vi.mock('go-captcha-vue', () => ({
  Click: createPanelStub('Click', [
    { x: 10, y: 20 },
    { x: 30, y: 40 },
    { x: 50, y: 60 }
  ]),
  Slide: createPanelStub('Slide', { x: 120, y: 80 }),
  SlideRegion: createPanelStub('SlideRegion', { x: 120, y: 80 }),
  Rotate: createPanelStub('Rotate', 145)
}))

function challengeFor(mode: GoCaptchaMode): GoCaptchaChallenge {
  return {
    captcha_id: `id-${mode}`,
    mode,
    master_image: 'data:image/jpeg;base64,master',
    thumb_image: 'data:image/png;base64,thumb',
    tile_x: 12,
    tile_y: 84,
    tile_width: 62,
    tile_height: 62,
    thumb_size: 140
  }
}

function mountWidget(mode: GoCaptchaMode = 'click') {
  return mount(GoCaptchaWidget, { props: { mode }, attachTo: document.body })
}

describe('GoCaptchaWidget', () => {
  beforeEach(() => {
    createChallengeMock.mockReset()
    verifyMock.mockReset()
    document.body.innerHTML = ''
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1024 })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it.each<[GoCaptchaMode, string, string]>([
    ['click', 'Click', '10,20,30,40,50,60'],
    ['shape', 'Click', '10,20,30,40,50,60'],
    ['slide', 'Slide', '120,80'],
    ['drag', 'SlideRegion', '120,80'],
    ['rotate', 'Rotate', '145']
  ])('%s 模式渲染 %s 面板并提交对应格式的作答', async (mode, panel, expectedAnswer) => {
    createChallengeMock.mockResolvedValue(challengeFor(mode))
    verifyMock.mockResolvedValue({ token: 'issued-token', expires_in: 300 })
    const wrapper = mountWidget(mode)

    await wrapper.find('[data-testid="gocaptcha-trigger"]').trigger('click')
    await flushPromises()

    const confirmButton = document.querySelector(`[data-testid="${panel}-confirm"]`)
    expect(confirmButton).not.toBeNull()
    confirmButton?.dispatchEvent(new Event('click'))
    await flushPromises()

    expect(verifyMock).toHaveBeenCalledWith(`id-${mode}`, expectedAnswer)
    expect(wrapper.emitted('verify')?.[0]).toEqual(['issued-token'])
    wrapper.unmount()
  })

  it('把本地化标题和按钮文案传给真实面板契约', async () => {
    createChallengeMock.mockResolvedValue(challengeFor('click'))
    const wrapper = mountWidget()

    await wrapper.find('[data-testid="gocaptcha-trigger"]').trigger('click')
    await flushPromises()

    const panel = document.querySelector('[data-testid="Click-confirm"]')
    expect(panel?.getAttribute('data-title')).toBe('auth.captchaClickTitle')
    expect(panel?.getAttribute('data-button-text')).toBe('auth.captchaConfirm')
    wrapper.unmount()
  })

  it('窄屏下缩放面板并把提交坐标换算回原图坐标', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 320 })
    createChallengeMock.mockResolvedValue(challengeFor('slide'))
    verifyMock.mockResolvedValue({ token: 'issued-token', expires_in: 300 })
    const wrapper = mountWidget('slide')

    await wrapper.find('[data-testid="gocaptcha-trigger"]').trigger('click')
    await flushPromises()
    const panel = document.querySelector('[data-testid="Slide-confirm"]')
    expect(panel?.getAttribute('data-width')).toBe('272')
    panel?.dispatchEvent(new Event('click'))
    await flushPromises()

    expect(verifyMock).toHaveBeenCalledWith('id-slide', '132,88')
    wrapper.unmount()
  })

  it('作答成功后关闭面板并把按钮切到已验证态', async () => {
    createChallengeMock.mockResolvedValue(challengeFor('click'))
    verifyMock.mockResolvedValue({ token: 'issued-token', expires_in: 300 })
    const wrapper = mountWidget()

    await wrapper.find('[data-testid="gocaptcha-trigger"]').trigger('click')
    await flushPromises()
    document.querySelector('[data-testid="Click-confirm"]')?.dispatchEvent(new Event('click'))
    await flushPromises()

    expect(document.querySelector('[data-testid="gocaptcha-panel"]')).toBeNull()
    expect(wrapper.find('[data-testid="gocaptcha-trigger"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('答错后自动换一张新图，因为一个挑战只有一次作答机会', async () => {
    createChallengeMock
      .mockResolvedValueOnce({ ...challengeFor('click'), captcha_id: 'first' })
      .mockResolvedValueOnce({ ...challengeFor('click'), captcha_id: 'second' })
    verifyMock.mockRejectedValue({ status: 400, reason: 'GO_CAPTCHA_VERIFICATION_FAILED' })
    const wrapper = mountWidget()

    await wrapper.find('[data-testid="gocaptcha-trigger"]').trigger('click')
    await flushPromises()
    document.querySelector('[data-testid="Click-confirm"]')?.dispatchEvent(new Event('click'))
    await flushPromises()

    expect(createChallengeMock).toHaveBeenCalledTimes(2)
    expect(document.querySelector('[data-testid="gocaptcha-panel"]')).not.toBeNull()
    expect(document.querySelector('[data-testid="gocaptcha-error"]')?.textContent).toContain(
      'auth.captchaIncorrect'
    )
    expect(wrapper.emitted('verify')).toBeUndefined()
    wrapper.unmount()
  })

  it('冷却限流时打开面板并展示友好提示', async () => {
    createChallengeMock.mockRejectedValue({ status: 429 })
    const wrapper = mountWidget()

    await wrapper.find('[data-testid="gocaptcha-trigger"]').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('error')).toBeUndefined()
    expect(document.querySelector('[data-testid="gocaptcha-panel"]')).not.toBeNull()
    expect(document.querySelector('[data-testid="gocaptcha-error"]')?.textContent).toContain(
      'auth.captchaTooManyFailures'
    )
    wrapper.unmount()
  })

  it('verify 打开面板并在用户完成后兑现令牌', async () => {
    createChallengeMock.mockResolvedValue(challengeFor('slide'))
    verifyMock.mockResolvedValue({ token: 'action-token', expires_in: 300 })
    const wrapper = mountWidget('slide')

    const pending = (wrapper.vm as unknown as { verify: () => Promise<string | null> }).verify()
    await flushPromises()
    document.querySelector('[data-testid="Slide-confirm"]')?.dispatchEvent(new Event('click'))
    await flushPromises()

    await expect(pending).resolves.toBe('action-token')
    wrapper.unmount()
  })

  it('用户关闭面板时 verify 以 null 兑现，表示放弃本次验证', async () => {
    createChallengeMock.mockResolvedValue(challengeFor('click'))
    const wrapper = mountWidget()

    const pending = (wrapper.vm as unknown as { verify: () => Promise<string | null> }).verify()
    await flushPromises()
    ;(document.querySelector('[data-testid="gocaptcha-panel"]') as HTMLElement)?.click()
    await flushPromises()

    await expect(pending).resolves.toBeNull()
    wrapper.unmount()
  })

  it('reset 清空已签发的令牌与验证态', async () => {
    createChallengeMock.mockResolvedValue(challengeFor('click'))
    verifyMock.mockResolvedValue({ token: 'issued-token', expires_in: 300 })
    const wrapper = mountWidget()

    await wrapper.find('[data-testid="gocaptcha-trigger"]').trigger('click')
    await flushPromises()
    document.querySelector('[data-testid="Click-confirm"]')?.dispatchEvent(new Event('click'))
    await flushPromises()

    ;(wrapper.vm as unknown as { reset: () => void }).reset()
    await flushPromises()

    expect(wrapper.find('[data-testid="gocaptcha-trigger"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('令牌到期后清除已验证态并 emit expire', async () => {
    vi.useFakeTimers()
    createChallengeMock.mockResolvedValue(challengeFor('click'))
    verifyMock.mockResolvedValue({ token: 'short-lived-token', expires_in: 1 })
    const wrapper = mountWidget()

    await wrapper.find('[data-testid="gocaptcha-trigger"]').trigger('click')
    await flushPromises()
    document.querySelector('[data-testid="Click-confirm"]')?.dispatchEvent(new Event('click'))
    await flushPromises()
    expect(wrapper.find('[data-testid="gocaptcha-trigger"]').attributes('disabled')).toBeDefined()

    await vi.advanceTimersByTimeAsync(1000)

    expect(wrapper.emitted('expire')).toHaveLength(1)
    expect(wrapper.find('[data-testid="gocaptcha-trigger"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
