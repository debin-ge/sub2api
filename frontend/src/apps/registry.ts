import type { AppIconKind } from '@/components/apps/AppIcon.vue'

import claudeCodeContent from './zh/claude-code.md?raw'
import codexContent from './zh/codex.md?raw'
import cursorContent from './zh/cursor.md?raw'
import clineContent from './zh/cline.md?raw'
import continueContent from './zh/continue.md?raw'
import traeContent from './zh/trae.md?raw'
import ccSwitchContent from './zh/cc-switch.md?raw'
import cockpitToolsContent from './zh/cockpit-tools.md?raw'
import codeContent from './zh/code.md?raw'

import claudeCodeContentEn from './en/claude-code.md?raw'
import codexContentEn from './en/codex.md?raw'
import cursorContentEn from './en/cursor.md?raw'
import clineContentEn from './en/cline.md?raw'
import continueContentEn from './en/continue.md?raw'
import traeContentEn from './en/trae.md?raw'
import ccSwitchContentEn from './en/cc-switch.md?raw'
import cockpitToolsContentEn from './en/cockpit-tools.md?raw'
import codeContentEn from './en/code.md?raw'

export interface AppEntry {
  slug: string
  name: string
  tagline: string
  /** Which SVG to show in the card and detail header. */
  icon: AppIconKind
  protocols: string[]
  steps: number
  content: string
}

export type AppLocale = 'en' | 'zh'

export const appEntriesByLocale: Record<AppLocale, AppEntry[]> = {
  zh: [
    {
      slug: 'claude-code',
      name: 'Claude Code',
      tagline: 'Anthropic 官方 CLI，配一次全端可用',
      icon: 'anthropic',
      protocols: ['anthropic'],
      steps: 3,
      content: claudeCodeContent,
    },
    {
      slug: 'codex',
      name: 'Codex CLI',
      tagline: 'OpenAI 官方 CLI，与 IDE 扩展共用配置',
      icon: 'openai',
      protocols: ['openai'],
      steps: 3,
      content: codexContent,
    },
    {
      slug: 'cursor',
      name: 'Cursor',
      tagline: 'AI 编辑器，全部在设置面板完成',
      icon: 'app',
      protocols: ['openai'],
      steps: 3,
      content: cursorContent,
    },
    {
      slug: 'cline',
      name: 'Cline',
      tagline: 'VS Code AI 编程插件，推荐 Anthropic 模式',
      icon: 'app',
      protocols: ['anthropic', 'openai'],
      steps: 3,
      content: clineContent,
    },
    {
      slug: 'continue',
      name: 'Continue',
      tagline: 'VS Code / JetBrains 插件，靠 YAML 配置',
      icon: 'app',
      protocols: ['openai', 'anthropic'],
      steps: 3,
      content: continueContent,
    },
    {
      slug: 'trae',
      name: 'Trae',
      tagline: '字节 AI IDE，添加自定义模型服务',
      icon: 'app',
      protocols: ['anthropic', 'openai'],
      steps: 3,
      content: traeContent,
    },
    {
      slug: 'cc-switch',
      name: 'CC-Switch',
      tagline: '桌面多站点切换器，一键导入 {{SITE_NAME}}',
      icon: 'app',
      protocols: ['anthropic', 'openai', 'gemini'],
      steps: 3,
      content: ccSwitchContent,
    },
    {
      slug: 'cockpit-tools',
      name: 'Cockpit Tools',
      tagline: '通用 AI IDE 账号管理器，覆盖约 15 个工具',
      icon: 'app',
      protocols: ['anthropic', 'openai', 'gemini'],
      steps: 3,
      content: cockpitToolsContent,
    },
    {
      slug: 'code',
      name: '代码示例',
      tagline: 'curl / Python / TypeScript / Go 全语言示例',
      icon: 'code',
      protocols: ['openai', 'anthropic', 'gemini'],
      steps: 3,
      content: codeContent,
    },
  ],
  en: [
    {
      slug: 'claude-code',
      name: 'Claude Code',
      tagline: "Anthropic's official CLI — one config, every surface",
      icon: 'anthropic',
      protocols: ['anthropic'],
      steps: 3,
      content: claudeCodeContentEn,
    },
    {
      slug: 'codex',
      name: 'Codex CLI',
      tagline: "OpenAI's official CLI — shared config with IDE extensions",
      icon: 'openai',
      protocols: ['openai'],
      steps: 3,
      content: codexContentEn,
    },
    {
      slug: 'cursor',
      name: 'Cursor',
      tagline: 'AI editor — configured entirely in Settings',
      icon: 'app',
      protocols: ['openai'],
      steps: 3,
      content: cursorContentEn,
    },
    {
      slug: 'cline',
      name: 'Cline',
      tagline: 'VS Code AI coding plugin — Anthropic mode recommended',
      icon: 'app',
      protocols: ['anthropic', 'openai'],
      steps: 3,
      content: clineContentEn,
    },
    {
      slug: 'continue',
      name: 'Continue',
      tagline: 'VS Code / JetBrains plugin — configured via YAML',
      icon: 'app',
      protocols: ['openai', 'anthropic'],
      steps: 3,
      content: continueContentEn,
    },
    {
      slug: 'trae',
      name: 'Trae',
      tagline: "ByteDance's AI IDE — add a custom model provider",
      icon: 'app',
      protocols: ['anthropic', 'openai'],
      steps: 3,
      content: traeContentEn,
    },
    {
      slug: 'cc-switch',
      name: 'CC-Switch',
      tagline: 'Desktop multi-provider switcher — one-click {{SITE_NAME}} import',
      icon: 'app',
      protocols: ['anthropic', 'openai', 'gemini'],
      steps: 3,
      content: ccSwitchContentEn,
    },
    {
      slug: 'cockpit-tools',
      name: 'Cockpit Tools',
      tagline: 'Universal AI IDE account manager — covers ~15 tools',
      icon: 'app',
      protocols: ['anthropic', 'openai', 'gemini'],
      steps: 3,
      content: cockpitToolsContentEn,
    },
    {
      slug: 'code',
      name: 'Code',
      tagline: 'curl / Python / TypeScript / Go — every language',
      icon: 'code',
      protocols: ['openai', 'anthropic', 'gemini'],
      steps: 3,
      content: codeContentEn,
    },
  ],
}

export function normalizeAppLocale(locale: string | undefined): AppLocale {
  return locale?.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

export function findApp(slug: string | undefined, locale?: string): AppEntry | null {
  const apps = appEntriesByLocale[normalizeAppLocale(locale)]
  if (!slug) return null
  return apps.find((app) => app.slug === slug) ?? null
}
