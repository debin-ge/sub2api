import type { AccountPlatform, GroupPlatform } from '@/types'

export interface PlatformOption<T extends string = string> {
  value: T
  label: string
}

/**
 * Concrete upstream platforms supported by accounts and request routing.
 * Keep platform selectors derived from this catalog so newly added providers
 * do not silently disappear from list filters.
 */
export const CONCRETE_PLATFORM_OPTIONS = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
  { value: 'minimax', label: 'MiniMax' },
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: 'Zhipu GLM' },
  { value: 'glm', label: 'GLM (Legacy)' },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'windsurf', label: 'Windsurf' },
  { value: 'opencode_go', label: 'OpenCode' },
  { value: 'bytedance', label: 'ByteDance' }
] as const satisfies readonly PlatformOption<AccountPlatform>[]

/** Platforms that can own a group. */
export const GROUP_PLATFORM_OPTIONS = [
  ...CONCRETE_PLATFORM_OPTIONS,
  { value: 'composite', label: 'Composite' }
] as const satisfies readonly PlatformOption<GroupPlatform>[]

/**
 * glm 是 zhipu 的历史平台 ID：仅保留在按 ID 精确查找历史行的场景（如禁用态的编辑框回显）中，
 * 新建、筛选等一切"可交互选择"场景一律用此过滤后的清单，避免它作为一个可选、可展示的独立平台出现。
 */
export const CREATABLE_PLATFORM_OPTIONS = CONCRETE_PLATFORM_OPTIONS.filter(
  option => option.value !== 'glm'
)

/** Platforms that can own a group, excluding the legacy glm alias — for create/filter pickers. */
export const CREATABLE_GROUP_PLATFORM_OPTIONS = [
  ...CREATABLE_PLATFORM_OPTIONS,
  { value: 'composite', label: 'Composite' }
] as const satisfies readonly PlatformOption<GroupPlatform>[]
