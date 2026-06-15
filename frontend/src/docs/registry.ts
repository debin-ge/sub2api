import overviewContent from './zh/overview.md?raw'
import quickstartContent from './zh/quickstart.md?raw'
import apiReferenceContent from './zh/api-reference.md?raw'
import modelsContent from './zh/models.md?raw'
import clientsContent from './zh/clients.md?raw'
import errorsContent from './zh/errors.md?raw'
import faqContent from './zh/faq.md?raw'
import overviewContentEn from './en/overview.md?raw'
import quickstartContentEn from './en/quickstart.md?raw'
import apiReferenceContentEn from './en/api-reference.md?raw'
import modelsContentEn from './en/models.md?raw'
import clientsContentEn from './en/clients.md?raw'
import errorsContentEn from './en/errors.md?raw'
import faqContentEn from './en/faq.md?raw'

export interface UserDocEntry {
  slug: string
  title: string
  description: string
  category: string
  content: string
}

export const defaultUserDocSlug = 'overview'

export type UserDocLocale = 'en' | 'zh'

export const userDocsByLocale: Record<UserDocLocale, UserDocEntry[]> = {
  zh: [
  {
    slug: 'overview',
    title: '产品概览',
    description: '了解 Sub2API 的适用场景、核心概念和基本调用流程。',
    category: '入门',
    content: overviewContent,
  },
  {
    slug: 'quickstart',
    title: '快速开始',
    description: '配置 Base URL 和 API Key，并完成第一次模型、聊天和 Messages 请求。',
    category: '入门',
    content: quickstartContent,
  },
  {
    slug: 'api-reference',
    title: 'API 参考',
    description: '查看 OpenAI、Anthropic、Gemini 和 Antigravity 兼容端点。',
    category: '接口',
    content: apiReferenceContent,
  },
  {
    slug: 'models',
    title: '模型与平台',
    description: '理解平台矩阵，以及模型可用性如何受分组、渠道和配置影响。',
    category: '配置',
    content: modelsContent,
  },
  {
    slug: 'clients',
    title: '客户端接入',
    description: '使用 curl、OpenAI SDK、Claude Code、Gemini 和 Codex 接入 Sub2API。',
    category: '接入',
    content: clientsContent,
  },
  {
    slug: 'errors',
    title: '错误排查',
    description: '排查认证、权限、模型不可用、端点不支持和流式中断问题。',
    category: '排查',
    content: errorsContent,
  },
  {
    slug: 'faq',
    title: '常见问题',
    description: '回答 Base URL、API Key、模型列表、端点选择和模型失败等常见问题。',
    category: '帮助',
    content: faqContent,
  },
  ],
  en: [
    {
      slug: 'overview',
      title: 'Product Overview',
      description: 'Understand where Sub2API fits, its core concepts, and the basic request flow.',
      category: 'Getting Started',
      content: overviewContentEn,
    },
    {
      slug: 'quickstart',
      title: 'Quick Start',
      description: 'Configure the Base URL and API Key, then send your first model, chat, and Messages requests.',
      category: 'Getting Started',
      content: quickstartContentEn,
    },
    {
      slug: 'api-reference',
      title: 'API Reference',
      description: 'Review OpenAI, Anthropic, Gemini, and Antigravity compatible endpoints.',
      category: 'API',
      content: apiReferenceContentEn,
    },
    {
      slug: 'models',
      title: 'Models and Platforms',
      description: 'Learn how platform support and model availability depend on groups, channels, and settings.',
      category: 'Configuration',
      content: modelsContentEn,
    },
    {
      slug: 'clients',
      title: 'Client Integration',
      description: 'Connect to Sub2API with curl, the OpenAI SDK, Claude Code, Gemini, and Codex.',
      category: 'Integration',
      content: clientsContentEn,
    },
    {
      slug: 'errors',
      title: 'Troubleshooting',
      description: 'Diagnose authentication, permission, model availability, unsupported endpoint, and streaming issues.',
      category: 'Troubleshooting',
      content: errorsContentEn,
    },
    {
      slug: 'faq',
      title: 'FAQ',
      description: 'Answers for Base URL, API Key, model list, endpoint selection, and model request failures.',
      category: 'Help',
      content: faqContentEn,
    },
  ],
}

export const userDocs = userDocsByLocale.zh

export function normalizeUserDocLocale(locale: string | undefined): UserDocLocale {
  return locale?.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

export function findUserDoc(slug: string | undefined, locale?: string): UserDocEntry | null {
  const normalizedSlug = slug?.trim() || defaultUserDocSlug
  const docs = userDocsByLocale[normalizeUserDocLocale(locale)]
  return docs.find((doc) => doc.slug === normalizedSlug) ?? null
}
