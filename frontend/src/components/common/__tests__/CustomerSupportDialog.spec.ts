import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CustomerSupportDialog from '../CustomerSupportDialog.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

describe('CustomerSupportDialog', () => {
  it('shows the uploaded QR code and closes from the footer action', async () => {
    const wrapper = mount(CustomerSupportDialog, {
      props: {
        show: true,
        qrCode: 'data:image/png;base64,c3VwcG9ydA==',
        contactInfo: '企业微信客服',
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show', 'title'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
        },
      },
    })

    const image = wrapper.get('[data-testid="customer-support-qr-code"]')
    expect(image.attributes('src')).toBe('data:image/png;base64,c3VwcG9ydA==')
    expect(wrapper.text()).toContain('企业微信客服')

    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
