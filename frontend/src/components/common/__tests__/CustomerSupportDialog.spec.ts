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
    expect(image.attributes('src')).toBe('/api/v1/settings/contact-qr-code')
    expect(wrapper.text()).toContain('企业微信客服')

    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('does not request the QR image while the dialog is closed', () => {
    const wrapper = mount(CustomerSupportDialog, {
      props: {
        show: false,
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show', 'title'],
            template: '<div><slot /></div>',
          },
        },
      },
    })

    expect(wrapper.find('[data-testid="customer-support-qr-code"]').exists()).toBe(false)
  })
})
