import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AccountGroupsCell from '../AccountGroupsCell.vue'
import type { Group } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function makeGroup(id: number, name: string): Group {
  return {
    id,
    name,
    description: null,
    platform: 'anthropic',
    rate_multiplier: 1,
    is_exclusive: false,
    status: 'active',
    subscription_type: 'standard',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null
  } as Group
}

const LONG_NAME = '生产环境-Claude-高优先级-长名称分组-需要完整展示'

/** jsdom 的 scrollWidth/clientWidth 恒为 0，手动伪造出“文本被截断”的量测结果 */
function stubTruncation() {
  const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollWidth')
  Object.defineProperty(HTMLElement.prototype, 'scrollWidth', {
    configurable: true,
    get() {
      return 200
    }
  })
  return () => {
    if (original) {
      Object.defineProperty(HTMLElement.prototype, 'scrollWidth', original)
    } else {
      delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollWidth
    }
  }
}

let restoreTruncation: (() => void) | null = null

afterEach(() => {
  restoreTruncation?.()
  restoreTruncation = null
  document.body.innerHTML = ''
})

describe('AccountGroupsCell', () => {
  it('每个分组徽章都带上完整名称的 title，便于悬停查看', () => {
    const wrapper = mount(AccountGroupsCell, {
      props: { groups: [makeGroup(1, LONG_NAME), makeGroup(2, '备用分组')] }
    })

    const titles = wrapper.findAll('[title]').map(el => el.attributes('title'))
    expect(titles).toContain(LONG_NAME)
    expect(titles).toContain('备用分组')
  })

  it('单个分组时放宽徽章宽度，多个分组时保持原有列宽', () => {
    const single = mount(AccountGroupsCell, {
      props: { groups: [makeGroup(1, LONG_NAME)] }
    })
    expect(single.find('[title]').classes()).toContain('max-w-48')

    const multiple = mount(AccountGroupsCell, {
      props: { groups: [makeGroup(1, LONG_NAME), makeGroup(2, '备用分组')] }
    })
    expect(multiple.find('[title]').classes()).toContain('max-w-24')
  })

  it('名称未被截断且没有折叠分组时不提供展开入口', async () => {
    const wrapper = mount(AccountGroupsCell, {
      props: { groups: [makeGroup(1, '短名')] }
    })
    await nextTick()

    expect(wrapper.find('[role="button"]').exists()).toBe(false)
  })

  it('名称被截断时整块区域可点击，popover 换行展示完整名称', async () => {
    restoreTruncation = stubTruncation()

    const wrapper = mount(AccountGroupsCell, {
      props: { groups: [makeGroup(1, LONG_NAME), makeGroup(2, '备用分组')] },
      attachTo: document.body
    })
    await nextTick()
    await nextTick()

    const trigger = wrapper.find('[role="button"]')
    expect(trigger.exists()).toBe(true)
    expect(trigger.classes()).toContain('cursor-pointer')

    await trigger.trigger('click')
    await nextTick()

    const popover = document.body.querySelector('.z-50')
    expect(popover).not.toBeNull()
    expect(popover?.textContent).toContain(LONG_NAME)
    // popover 内的名称改为换行展示，不再 truncate
    expect(popover?.querySelector('.whitespace-normal')).not.toBeNull()
    expect(popover?.querySelector('.truncate')).toBeNull()

    wrapper.unmount()
  })

  it('分组数量超过上限时仍展示 +N，并可展开查看全部', async () => {
    const groups = [1, 2, 3, 4, 5, 6].map(i => makeGroup(i, `分组-${i}`))
    const wrapper = mount(AccountGroupsCell, {
      props: { groups, maxDisplay: 4 },
      attachTo: document.body
    })
    await nextTick()

    expect(wrapper.text()).toContain('+3')

    await wrapper.find('[role="button"]').trigger('click')
    await nextTick()

    const popover = document.body.querySelector('.z-50')
    expect(popover).not.toBeNull()
    for (const group of groups) {
      expect(popover?.textContent).toContain(group.name)
    }

    wrapper.unmount()
  })
})
