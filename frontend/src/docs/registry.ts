import overviewContent from './zh/overview.md?raw'
import quickstartContent from './zh/quickstart.md?raw'
import apiKeysContent from './zh/api-keys.md?raw'
import apiReferenceContent from './zh/api-reference.md?raw'
import endpointSelectionContent from './zh/endpoint-selection.md?raw'
import modelsContent from './zh/models.md?raw'
import billingUsageContent from './zh/billing-usage.md?raw'
import clientsContent from './zh/clients.md?raw'
import configurationSnippetsContent from './zh/configuration-snippets.md?raw'
import errorsContent from './zh/errors.md?raw'
import bestPracticesContent from './zh/best-practices.md?raw'
import faqContent from './zh/faq.md?raw'
import overviewContentEn from './en/overview.md?raw'
import quickstartContentEn from './en/quickstart.md?raw'
import apiKeysContentEn from './en/api-keys.md?raw'
import apiReferenceContentEn from './en/api-reference.md?raw'
import endpointSelectionContentEn from './en/endpoint-selection.md?raw'
import modelsContentEn from './en/models.md?raw'
import billingUsageContentEn from './en/billing-usage.md?raw'
import clientsContentEn from './en/clients.md?raw'
import configurationSnippetsContentEn from './en/configuration-snippets.md?raw'
import errorsContentEn from './en/errors.md?raw'
import bestPracticesContentEn from './en/best-practices.md?raw'
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
      description: '了解 {{SITE_NAME}} 的适用场景、核心概念和基本调用流程。',
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
      slug: 'api-keys',
      title: 'API Key 与账户',
      description: '了解登录、API Key、权限、密钥安全和轮换流程。',
      category: '入门',
      content: apiKeysContent,
    },
    {
      slug: 'api-reference',
      title: 'API 参考',
      description: '查看 OpenAI、Anthropic、Gemini 和 Antigravity 兼容端点。',
      category: '接口',
      content: apiReferenceContent,
    },
    {
      slug: 'endpoint-selection',
      title: '端点选择指南',
      description: '根据客户端、请求格式和模型能力选择正确的 API 端点。',
      category: '接口',
      content: endpointSelectionContent,
    },
    {
      slug: 'models',
      title: '模型与平台',
      description: '理解平台矩阵、模型选择方法，以及可用性如何受分组、渠道和配置影响。',
      category: '配置',
      content: modelsContent,
    },
    {
      slug: 'billing-usage',
      title: '计费与用量',
      description: '理解用量来源、价格影响因素、成本控制和异常对账方式。',
      category: '配置',
      content: billingUsageContent,
    },
    {
      slug: 'clients',
      title: '客户端接入',
      description: '使用 curl、OpenAI SDK、Claude Code、Gemini 和 Codex 接入 {{SITE_NAME}}。',
      category: '接入',
      content: clientsContent,
    },
    {
      slug: 'configuration-snippets',
      title: '可复制配置模板',
      description: '复制常用环境变量、SDK、curl、Claude、Gemini 和安全代理配置。',
      category: '接入',
      content: configurationSnippetsContent,
    },
    {
      slug: 'errors',
      title: '错误排查',
      description: '排查认证、权限、模型不可用、端点不支持、限流和流式中断问题。',
      category: '排查',
      content: errorsContent,
    },
    {
      slug: 'best-practices',
      title: '最佳实践',
      description: '学习生产接入、模型选择、重试、流式、成本控制和日志建议。',
      category: '帮助',
      content: bestPracticesContent,
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
      description: 'Understand where {{SITE_NAME}} fits, its core concepts, and the basic request flow.',
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
      slug: 'api-keys',
      title: 'API Keys and Accounts',
      description: 'Understand login, API Keys, permissions, key security, and rotation.',
      category: 'Getting Started',
      content: apiKeysContentEn,
    },
    {
      slug: 'api-reference',
      title: 'API Reference',
      description: 'Review OpenAI, Anthropic, Gemini, and Antigravity compatible endpoints.',
      category: 'API',
      content: apiReferenceContentEn,
    },
    {
      slug: 'endpoint-selection',
      title: 'Endpoint Selection Guide',
      description: 'Choose the right API endpoint based on client, request format, and model capability.',
      category: 'API',
      content: endpointSelectionContentEn,
    },
    {
      slug: 'models',
      title: 'Models and Platforms',
      description: 'Learn platform support, model selection, and how availability depends on groups, channels, and settings.',
      category: 'Configuration',
      content: modelsContentEn,
    },
    {
      slug: 'billing-usage',
      title: 'Billing and Usage',
      description: 'Understand usage sources, pricing factors, cost control, and anomaly reconciliation.',
      category: 'Configuration',
      content: billingUsageContentEn,
    },
    {
      slug: 'clients',
      title: 'Client Integration',
      description: 'Connect to {{SITE_NAME}} with curl, the OpenAI SDK, Claude Code, Gemini, and Codex.',
      category: 'Integration',
      content: clientsContentEn,
    },
    {
      slug: 'configuration-snippets',
      title: 'Copy-Ready Configuration Snippets',
      description: 'Copy environment, SDK, curl, Claude, Gemini, and safe proxy configuration templates.',
      category: 'Integration',
      content: configurationSnippetsContentEn,
    },
    {
      slug: 'errors',
      title: 'Troubleshooting',
      description: 'Diagnose authentication, permission, model availability, unsupported endpoint, rate limit, and streaming issues.',
      category: 'Troubleshooting',
      content: errorsContentEn,
    },
    {
      slug: 'best-practices',
      title: 'Best Practices',
      description: 'Learn production integration, model selection, retries, streaming, cost control, and logging practices.',
      category: 'Help',
      content: bestPracticesContentEn,
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
