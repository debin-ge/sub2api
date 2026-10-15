import { openAIPlanTypeLabel } from '@/utils/planType'
import { describe, it, expect } from 'vitest'
import {
  ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY,
  HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY,
  HEADER_OVERRIDES_CREDENTIAL_KEY,
  OPENCODE_GO_PROTOCOL_RULES_KEY,
  applyAntigravityProjectID,
  applyHeaderOverride,
  applyInterceptWarmup,
  applyOpenCodeGoProtocolRules,
  applyPlanType,
  buildHeaderOverridesObject,
  buildPlanTypeOptions,
  cloneOpenCodeGoProtocolRules,
  cnQuotaCellVisible,
  defaultCNBaseUrl,
  defaultOpenCodeProtocolRules,
  isCustomGrokBaseUrl,
  resolveOpenCodeAccountMode,
  isHeaderOverrideCapable,
  GROK_BASE_URL_PRESETS,
  parseHeaderOverridesJson,
  parseOpenCodeGoProtocolRules,
  planTypeDisplayLabel,
  protocolRuleSet,
  readPlanType,
  serializeHeaderOverrideRows,
  splitHeaderOverridesObject,
  validateHeaderOverrideRows,
  BYTEDANCE_PROTOCOL_MODE_CREDENTIAL_KEY,
  applyByteDanceProtocolMode,
  byteDanceProtocolRequiresBaseUrl,
  readByteDanceProtocolMode,
  VIDEO_VENDOR_DEFAULT_BASE_URLS,
  VIDEO_VENDOR_PLATFORMS,
  isVideoOnlyPlatform,
  isVideoVendorPlatform,
  videoVendorDefaultBaseUrl
} from '../credentialsBuilder'

describe('applyInterceptWarmup', () => {
  it('create + enabled=true: should set intercept_warmup_requests to true', () => {
    const creds: Record<string, unknown> = { access_token: 'tok' }
    applyInterceptWarmup(creds, true, 'create')
    expect(creds.intercept_warmup_requests).toBe(true)
  })

  it('create + enabled=false: should not add the field', () => {
    const creds: Record<string, unknown> = { access_token: 'tok' }
    applyInterceptWarmup(creds, false, 'create')
    expect('intercept_warmup_requests' in creds).toBe(false)
  })

  it('edit + enabled=true: should set intercept_warmup_requests to true', () => {
    const creds: Record<string, unknown> = { api_key: 'sk' }
    applyInterceptWarmup(creds, true, 'edit')
    expect(creds.intercept_warmup_requests).toBe(true)
  })

  it('edit + enabled=false + field exists: should delete the field', () => {
    const creds: Record<string, unknown> = { api_key: 'sk', intercept_warmup_requests: true }
    applyInterceptWarmup(creds, false, 'edit')
    expect('intercept_warmup_requests' in creds).toBe(false)
  })

  it('edit + enabled=false + field absent: should not throw', () => {
    const creds: Record<string, unknown> = { api_key: 'sk' }
    applyInterceptWarmup(creds, false, 'edit')
    expect('intercept_warmup_requests' in creds).toBe(false)
  })

  it('should not affect other fields', () => {
    const creds: Record<string, unknown> = {
      api_key: 'sk',
      base_url: 'url',
      intercept_warmup_requests: true
    }
    applyInterceptWarmup(creds, false, 'edit')
    expect(creds.api_key).toBe('sk')
    expect(creds.base_url).toBe('url')
    expect('intercept_warmup_requests' in creds).toBe(false)
  })
})
describe('applyAntigravityProjectID', () => {
  it('create + project id: trims and stores configured project fallback', () => {
    const creds: Record<string, unknown> = { access_token: 'tok' }
    applyAntigravityProjectID(creds, '  configured-project  ', 'create')
    expect(creds[ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY]).toBe('configured-project')
  })

  it('create + empty project id: should not add the field', () => {
    const creds: Record<string, unknown> = { access_token: 'tok' }
    applyAntigravityProjectID(creds, '   ', 'create')
    expect(ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY in creds).toBe(false)
  })

  it('edit + empty project id: deletes existing fallback', () => {
    const creds: Record<string, unknown> = {
      access_token: 'tok',
      [ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY]: 'old-project'
    }
    applyAntigravityProjectID(creds, '', 'edit')
    expect(ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY in creds).toBe(false)
  })

  it('does not affect onboard project_id or other credentials', () => {
    const creds: Record<string, unknown> = {
      project_id: 'onboard-project',
      model_mapping: { 'gemini-*': 'gemini-2.5-flash' }
    }
    applyAntigravityProjectID(creds, 'configured-project', 'edit')
    expect(creds.project_id).toBe('onboard-project')
    expect(creds.model_mapping).toEqual({ 'gemini-*': 'gemini-2.5-flash' })
    expect(creds[ANTIGRAVITY_PROJECT_ID_CREDENTIAL_KEY]).toBe('configured-project')
  })
})

describe('openCodeGo protocol rules', () => {
  it('resolves missing OpenCode account_mode as GO and zen as Zen', () => {
    expect(resolveOpenCodeAccountMode(undefined)).toBe('go')
    expect(resolveOpenCodeAccountMode('coding')).toBe('go')
    expect(resolveOpenCodeAccountMode('zen')).toBe('zen')
    expect(resolveOpenCodeAccountMode('go')).toBe('go')
  })

  it('uses Zen vs GO default endpoints and protocol rules', () => {
    expect(defaultCNBaseUrl('opencode_go', 'zen', 'adaptive')).toBe('https://opencode.ai/zen/v1')
    expect(defaultCNBaseUrl('opencode_go', 'zen', 'anthropic')).toBe('https://opencode.ai/zen')
    expect(defaultCNBaseUrl('opencode_go', 'go', 'adaptive')).toBe('https://opencode.ai/zen/go/v1')
    expect(defaultCNBaseUrl('opencode_go', 'go', 'anthropic')).toBe('https://opencode.ai/zen/go')
    expect(defaultOpenCodeProtocolRules('zen').some(rule => rule.pattern === 'claude-*')).toBe(true)
    expect(defaultOpenCodeProtocolRules('go').some(rule => rule.pattern === 'minimax-*')).toBe(true)
    const openCodeAccount = (account_mode?: string) => ({ platform: 'opencode_go', type: 'apikey', credentials: { account_mode } })
    expect(cnQuotaCellVisible(openCodeAccount('zen'))).toBe(false)
    expect(cnQuotaCellVisible(openCodeAccount('go'))).toBe(true)
    expect(cnQuotaCellVisible(openCodeAccount())).toBe(true)
  })

  it('parses stored rules and skips invalid entries', () => {
    expect(parseOpenCodeGoProtocolRules(null)).toBeNull()
    expect(parseOpenCodeGoProtocolRules([])).toEqual([])
    expect(
      parseOpenCodeGoProtocolRules([
        { pattern: 'grok-*', protocol: 'responses' },
        { pattern: '', protocol: 'anthropic' },
        { pattern: 'qwen*', protocol: 'adaptive' },
        { pattern: 'minimax-*', protocol: 'anthropic' }
      ])
    ).toEqual([
      { pattern: 'grok-*', protocol: 'responses' },
      { pattern: 'minimax-*', protocol: 'anthropic' }
    ])
  })

  it('round-trips protocol sets with the preferred protocol first', () => {
    const parsed = parseOpenCodeGoProtocolRules([
      { pattern: 'gpt-*', protocol: 'responses', protocols: ['responses', 'chat_completions', 'adaptive', 'chat_completions'] },
      { pattern: 'glm-*', protocols: ['anthropic', 'chat_completions'] },
      { pattern: 'kimi-*', protocol: 'chat_completions', protocols: ['chat_completions'] }
    ])
    expect(parsed).toEqual([
      { pattern: 'gpt-*', protocol: 'responses', extraProtocols: ['chat_completions'] },
      { pattern: 'glm-*', protocol: 'anthropic', extraProtocols: ['chat_completions'] },
      { pattern: 'kimi-*', protocol: 'chat_completions' }
    ])
    expect(protocolRuleSet(parsed![0])).toEqual(['responses', 'chat_completions'])

    const cloned = cloneOpenCodeGoProtocolRules(parsed!)
    expect(cloned).toEqual(parsed)
    expect(cloned[0].extraProtocols).not.toBe(parsed![0].extraProtocols)

    const credentials: Record<string, unknown> = {}
    applyOpenCodeGoProtocolRules(credentials, [{ pattern: ' GPT-* ', protocol: 'responses', extraProtocols: ['chat_completions', 'responses'] }, ...parsed!.slice(2)], 'create')
    expect(credentials[OPENCODE_GO_PROTOCOL_RULES_KEY]).toEqual([
      { pattern: 'gpt-*', protocol: 'responses', protocols: ['responses', 'chat_completions'] },
      { pattern: 'kimi-*', protocol: 'chat_completions' }
    ])
  })

  it('writes lowercase patterns on create and keeps an empty list on edit', () => {
    const created: Record<string, unknown> = {}
    applyOpenCodeGoProtocolRules(created, cloneOpenCodeGoProtocolRules(), 'create')
    expect(created[OPENCODE_GO_PROTOCOL_RULES_KEY]).toEqual(cloneOpenCodeGoProtocolRules())

    const edited: Record<string, unknown> = { api_key: 'sk' }
    applyOpenCodeGoProtocolRules(edited, [], 'edit')
    expect(edited[OPENCODE_GO_PROTOCOL_RULES_KEY]).toEqual([])
  })
})

describe('isHeaderOverrideCapable', () => {
  it('anthropic/openai only support apikey accounts', () => {
    expect(isHeaderOverrideCapable('anthropic', 'apikey')).toBe(true)
    expect(isHeaderOverrideCapable('openai', 'apikey')).toBe(true)
    expect(isHeaderOverrideCapable('anthropic', 'oauth')).toBe(false)
    expect(isHeaderOverrideCapable('openai', 'oauth')).toBe(false)
  })

  it('kimi/zhipu/deepseek only support apikey accounts', () => {
    for (const platform of ['kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'bytedance', 'fal', 'wavespeed', 'runware', 'flova']) {
      expect(isHeaderOverrideCapable(platform, 'apikey')).toBe(true)
      expect(isHeaderOverrideCapable(platform, 'oauth')).toBe(false)
    }
  })

  // glm 是 zhipu 的历史平台 ID，资格判定需与 zhipu 保持一致（与后端 IsHeaderOverrideEligible 对齐）。
  it('glm (legacy zhipu alias) only supports apikey accounts', () => {
    expect(isHeaderOverrideCapable('glm', 'apikey')).toBe(true)
    expect(isHeaderOverrideCapable('glm', 'oauth')).toBe(false)
  })

  it('grok supports both apikey and oauth accounts', () => {
    expect(isHeaderOverrideCapable('grok', 'apikey')).toBe(true)
    expect(isHeaderOverrideCapable('grok', 'oauth')).toBe(true)
    expect(isHeaderOverrideCapable('grok', 'bedrock')).toBe(false)
  })

  it('other platforms are not supported', () => {
    expect(isHeaderOverrideCapable('gemini', 'apikey')).toBe(false)
    expect(isHeaderOverrideCapable('antigravity', 'apikey')).toBe(false)
    expect(isHeaderOverrideCapable('', 'apikey')).toBe(false)
  })
})

describe('parseHeaderOverridesJson', () => {
  it('parses a flat object and normalizes values to trimmed strings', () => {
    expect(
      parseHeaderOverridesJson('{"User-Agent": " my-client/1.0 ", "x-num": 3, "x-flag": true}')
    ).toEqual([
      { name: 'User-Agent', value: 'my-client/1.0' },
      { name: 'x-flag', value: 'true' },
      { name: 'x-num', value: '3' }
    ])
  })

  it('drops entries with blank names', () => {
    expect(parseHeaderOverridesJson('{"  ": "v", "x-app": "cli"}')).toEqual([
      { name: 'x-app', value: 'cli' }
    ])
  })

  it('rejects invalid JSON, arrays, primitives and nested values', () => {
    expect(parseHeaderOverridesJson('not json')).toBeNull()
    expect(parseHeaderOverridesJson('[1,2]')).toBeNull()
    expect(parseHeaderOverridesJson('"str"')).toBeNull()
    expect(parseHeaderOverridesJson('null')).toBeNull()
    expect(parseHeaderOverridesJson('{"a": {"b": 1}}')).toBeNull()
    expect(parseHeaderOverridesJson('{"a": null}')).toBeNull()
  })

  it('parses an empty object to an empty row list', () => {
    expect(parseHeaderOverridesJson('{}')).toEqual([])
  })
})

describe('serializeHeaderOverrideRows', () => {
  it('serializes named rows and skips empty placeholder rows', () => {
    const text = serializeHeaderOverrideRows([
      { name: ' user-agent ', value: ' my-client/1.0 ' },
      { name: '', value: 'ignored' },
      { name: 'x-app', value: '' }
    ])
    expect(JSON.parse(text)).toEqual({ 'user-agent': 'my-client/1.0', 'x-app': '' })
  })

  it('round-trips with parseHeaderOverridesJson', () => {
    const rows = [
      { name: 'a-header', value: '1' },
      { name: 'b-header', value: '2' }
    ]
    expect(parseHeaderOverridesJson(serializeHeaderOverrideRows(rows))).toEqual(rows)
  })
})

describe('isCustomGrokBaseUrl', () => {
  it('treats only the default CLI gateway host as not customized', () => {
    expect(isCustomGrokBaseUrl('https://cli-chat-proxy.grok.com/v1')).toBe(false)
    expect(isCustomGrokBaseUrl('HTTPS://CLI-CHAT-PROXY.GROK.COM:443/')).toBe(false)
  })

  it('treats manually switched official/regional endpoints as customized (must echo back)', () => {
    expect(isCustomGrokBaseUrl('https://api.x.ai/v1')).toBe(true)
    expect(isCustomGrokBaseUrl('https://us-west-2.api.x.ai/v1')).toBe(true)
    expect(isCustomGrokBaseUrl('https://eu-west-1.api.x.ai/v1')).toBe(true)
  })

  it('treats empty, non-string and unparseable values as not customized', () => {
    expect(isCustomGrokBaseUrl('')).toBe(false)
    expect(isCustomGrokBaseUrl('   ')).toBe(false)
    expect(isCustomGrokBaseUrl(undefined)).toBe(false)
    expect(isCustomGrokBaseUrl(42)).toBe(false)
    expect(isCustomGrokBaseUrl('not a url')).toBe(false)
  })

  it('treats third-party hosts as customized', () => {
    expect(isCustomGrokBaseUrl('https://relay.example.com/v1')).toBe(true)
    expect(isCustomGrokBaseUrl('https://relay.example.com/xai/v1')).toBe(true)
    expect(isCustomGrokBaseUrl('http://relay.example.com/v1')).toBe(true)
  })
})

describe('GROK_BASE_URL_PRESETS', () => {
  it('covers the CLI gateway, official API and regional endpoints', () => {
    const urls = GROK_BASE_URL_PRESETS.map((p) => p.url)
    expect(urls).toEqual([
      'https://cli-chat-proxy.grok.com/v1',
      'https://api.x.ai/v1',
      'https://us-east-1.api.x.ai/v1',
      'https://us-west-2.api.x.ai/v1',
      'https://eu-west-1.api.x.ai/v1'
    ])
    for (const preset of GROK_BASE_URL_PRESETS) {
      // 每个预设要么有 i18n 标签键，要么有区域标识等字面标签
      expect(Boolean(preset.labelKey) || Boolean(preset.label)).toBe(true)
      if (preset.labelKey) {
        expect(['cli', 'official']).toContain(preset.labelKey)
      }
    }
    // 区域端点用区域标识作字面标签（us-east-1 这样的专有名词不做 i18n）
    expect(GROK_BASE_URL_PRESETS[2].label).toBe('us-east-1')
    expect(GROK_BASE_URL_PRESETS[3].label).toBe('us-west-2')
    expect(GROK_BASE_URL_PRESETS[4].label).toBe('eu-west-1')
  })
})

describe('validateHeaderOverrideRows', () => {
  it('accepts valid rows and empty placeholder rows', () => {
    expect(
      validateHeaderOverrideRows([
        { name: 'user-agent', value: 'my-agent/1.0' },
        { name: 'x-app', value: '' },
        { name: '', value: '' }
      ])
    ).toBeNull()
  })

  it('rejects empty name with non-empty value', () => {
    expect(validateHeaderOverrideRows([{ name: '', value: 'v' }])).toBe('invalidName')
  })

  it('rejects invalid header names', () => {
    expect(validateHeaderOverrideRows([{ name: 'bad name', value: '' }])).toBe('invalidName')
    expect(validateHeaderOverrideRows([{ name: 'bad:name', value: '' }])).toBe('invalidName')
    expect(validateHeaderOverrideRows([{ name: '名称', value: '' }])).toBe('invalidName')
  })

  it('rejects blocked header names case-insensitively', () => {
    expect(validateHeaderOverrideRows([{ name: 'Authorization', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'X-Api-Key', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'host', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'Content-Length', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'Content-Type', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'Cookie', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'x-goog-api-key', value: '' }])).toBe('blockedName')
  })

  it('rejects duplicate names case-insensitively', () => {
    expect(
      validateHeaderOverrideRows([
        { name: 'User-Agent', value: 'a' },
        { name: 'user-agent', value: 'b' }
      ])
    ).toBe('duplicateName')
  })
})

describe('buildHeaderOverridesObject / splitHeaderOverridesObject', () => {
  it('lowercases names, trims values and drops empty-name rows', () => {
    expect(
      buildHeaderOverridesObject([
        { name: ' User-Agent ', value: ' my-agent ' },
        { name: 'X-App', value: '' },
        { name: '', value: 'ignored' }
      ])
    ).toEqual({ 'user-agent': 'my-agent', 'x-app': '' })
  })

  it('splits an object into sorted rows and ignores non-string values', () => {
    expect(
      splitHeaderOverridesObject({ 'x-app': 'cli', 'user-agent': 'ua', bogus: 42 })
    ).toEqual([
      { name: 'user-agent', value: 'ua' },
      { name: 'x-app', value: 'cli' }
    ])
    expect(splitHeaderOverridesObject(null)).toEqual([])
    expect(splitHeaderOverridesObject(['a'])).toEqual([])
    expect(splitHeaderOverridesObject('str')).toEqual([])
  })

  it('roundtrips through build and split', () => {
    const rows = [
      { name: 'user-agent', value: 'ua' },
      { name: 'x-app', value: 'cli' }
    ]
    expect(splitHeaderOverridesObject(buildHeaderOverridesObject(rows))).toEqual(rows)
  })
})

describe('applyHeaderOverride', () => {
  it('create + enabled: writes enabled flag and overrides object', () => {
    const creds: Record<string, unknown> = { api_key: 'sk' }
    applyHeaderOverride(creds, true, [{ name: 'User-Agent', value: 'ua' }], 'create')
    expect(creds[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY]).toBe(true)
    expect(creds[HEADER_OVERRIDES_CREDENTIAL_KEY]).toEqual({ 'user-agent': 'ua' })
  })

  it('create + disabled: does not add fields', () => {
    const creds: Record<string, unknown> = { api_key: 'sk' }
    applyHeaderOverride(creds, false, [{ name: 'user-agent', value: 'ua' }], 'create')
    expect(HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY in creds).toBe(false)
    expect(HEADER_OVERRIDES_CREDENTIAL_KEY in creds).toBe(false)
  })

  it('edit + disabled: deletes existing fields', () => {
    const creds: Record<string, unknown> = {
      api_key: 'sk',
      [HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY]: true,
      [HEADER_OVERRIDES_CREDENTIAL_KEY]: { 'user-agent': 'ua' }
    }
    applyHeaderOverride(creds, false, [], 'edit')
    expect(HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY in creds).toBe(false)
    expect(HEADER_OVERRIDES_CREDENTIAL_KEY in creds).toBe(false)
    expect(creds.api_key).toBe('sk')
  })

  it('edit + enabled: replaces overrides object wholesale', () => {
    const creds: Record<string, unknown> = {
      [HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY]: true,
      [HEADER_OVERRIDES_CREDENTIAL_KEY]: { 'x-old': 'old' }
    }
    applyHeaderOverride(creds, true, [{ name: 'x-new', value: 'new' }], 'edit')
    expect(creds[HEADER_OVERRIDES_CREDENTIAL_KEY]).toEqual({ 'x-new': 'new' })
  })
})

describe('validateHeaderOverrideRows value/entry limits', () => {
  it('rejects websocket handshake headers', () => {
    expect(validateHeaderOverrideRows([{ name: 'Sec-WebSocket-Key', value: '' }])).toBe(
      'blockedName'
    )
  })

  it('rejects control characters in values', () => {
    expect(validateHeaderOverrideRows([{ name: 'x-app', value: 'a\x0bb' }])).toBe('invalidValue')
  })

  it('rejects oversized values', () => {
    expect(validateHeaderOverrideRows([{ name: 'x-app', value: 'a'.repeat(8193) }])).toBe(
      'invalidValue'
    )
  })

  it('measures value length in UTF-8 bytes to match backend', () => {
    // 3000 个 CJK 字符 = 3000 UTF-16 code units，但 9000 UTF-8 字节 > 8192
    expect(validateHeaderOverrideRows([{ name: 'x-app', value: '测'.repeat(3000) }])).toBe(
      'invalidValue'
    )
    expect(validateHeaderOverrideRows([{ name: 'x-app', value: '测'.repeat(2000) }])).toBeNull()
  })

  it('rejects too many entries', () => {
    const rows = Array.from({ length: 65 }, (_, i) => ({ name: `x-h-${i}`, value: 'v' }))
    expect(validateHeaderOverrideRows(rows)).toBe('tooManyEntries')
  })
})

describe('validateHeaderOverrideRows session isolation headers', () => {
  it('rejects per-request session headers', () => {
    expect(validateHeaderOverrideRows([{ name: 'session_id', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'Conversation_ID', value: '' }])).toBe('blockedName')
    expect(validateHeaderOverrideRows([{ name: 'x-codex-turn-state', value: '' }])).toBe(
      'blockedName'
    )
    expect(validateHeaderOverrideRows([{ name: 'X-Claude-Code-Session-Id', value: '' }])).toBe(
      'blockedName'
    )
    expect(validateHeaderOverrideRows([{ name: 'x-client-request-id', value: '' }])).toBe(
      'blockedName'
    )
  })

  it('allows tab inside value', () => {
    expect(validateHeaderOverrideRows([{ name: 'x-app', value: 'a\tb' }])).toBeNull()
  })

  it('rejects oversized names', () => {
    expect(validateHeaderOverrideRows([{ name: 'x'.repeat(201), value: 'v' }])).toBe('invalidName')
  })
})

describe('plan_type helpers', () => {
  describe('planTypeDisplayLabel', () => {
    it('maps canonical + alias values to friendly labels', () => {
      expect(planTypeDisplayLabel('plus')).toBe('Plus')
      expect(planTypeDisplayLabel('pro')).toBe('Pro 200')
      expect(planTypeDisplayLabel('chatgptpro')).toBe('Pro 200')
      expect(planTypeDisplayLabel('prolite')).toBe('Pro 100')
      expect(planTypeDisplayLabel('free')).toBe('Free')
      expect(planTypeDisplayLabel('team')).toBe('Business')
      expect(planTypeDisplayLabel('self_serve_business_prolite')).toBe('Business Premium')
    })
    it('normalizes case, separators and surrounding blanks', () => {
      expect(planTypeDisplayLabel('CHATGPTPRO')).toBe('Pro 200')
      expect(planTypeDisplayLabel('PROLITE')).toBe('Pro 100')
      expect(planTypeDisplayLabel('  Pro Lite  ')).toBe('Pro 100')
      expect(planTypeDisplayLabel('self-serve-business-pro-lite')).toBe('Business Premium')
    })
    it('returns unknown values verbatim', () => {
      expect(planTypeDisplayLabel('self_serve_business')).toBe('self_serve_business')
    })
  })

  describe('readPlanType', () => {
    it('reads a string plan_type', () => {
      expect(readPlanType({ plan_type: 'plus' })).toBe('plus')
    })
    it('treats non-string / missing values as empty', () => {
      expect(readPlanType({ plan_type: 42 })).toBe('')
      expect(readPlanType({ plan_type: true })).toBe('')
      expect(readPlanType({})).toBe('')
      expect(readPlanType(undefined)).toBe('')
      expect(readPlanType(null)).toBe('')
    })
  })

  describe('buildPlanTypeOptions', () => {
    it('includes all current SKUs with their status labels', () => {
      const opts = buildPlanTypeOptions('', 'Clear')
      expect(opts[0]).toEqual({ value: '', label: 'Clear' })
      expect(opts).toContainEqual({ value: 'prolite', label: 'Pro 100' })
      expect(opts).toContainEqual({ value: 'pro', label: 'Pro 200' })
      expect(opts).toContainEqual({ value: 'promax', label: 'Pro 500' })
      expect(opts).toContainEqual({ value: 'edu_plus', label: 'Edu Plus' })
      expect(opts).toContainEqual({ value: 'edu_pro', label: 'Edu Pro' })
      expect(opts).toContainEqual({ value: 'enterprise_cbp_automation', label: 'Enterprise (Automation)' })
    })
    it('preserves aliases without duplicating the same SKU', () => {
      for (const [alias, canonical] of [['chatgpt_pro', 'pro'], ['pro_lite', 'prolite'], ['selfservebusinessprolite', 'self_serve_business_prolite']]) {
        const opts = buildPlanTypeOptions(alias, 'Clear')
        expect(opts.filter(option => option.value === alias)).toHaveLength(1)
        expect(opts.some(option => option.value === canonical)).toBe(false)
      }
    })
    it('keeps distinct SKUs that share the Enterprise label', () => {
      const opts = buildPlanTypeOptions('ent26', 'Clear')
      expect(opts.filter(option => option.label === 'Enterprise').map(option => option.value)).toEqual([
        'business', 'enterprise', 'ent26', 'enterprise_cbp_usage_based'
      ])
      for (const value of ['business', 'enterprise', 'ent26', 'enterprise_cbp_usage_based']) {
        expect(applyPlanType({}, value)).toEqual({ plan_type: value })
      }
    })
    it('keeps unknown values and exact presets without duplicates', () => {
      expect(buildPlanTypeOptions('future_sku', 'Clear').at(-1)).toEqual({ value: 'future_sku', label: 'future_sku' })
      for (const value of ['promax', 'team', 'ent26', 'edu_plus']) {
        expect(buildPlanTypeOptions(value, 'Clear').filter(option => option.value === value)).toHaveLength(1)
      }
    })
  })

  describe('applyPlanType', () => {
    it('sets plan_type and preserves all other credential keys', () => {
      const creds = {
        chatgpt_account_id: 'acc',
        email: 'a@b.c',
        subscription_expires_at: '2026-01-01',
        model_mapping: { x: 'y' }
      }
      const out = applyPlanType({ ...creds }, 'plus')
      expect(out).toEqual({ ...creds, plan_type: 'plus' })
    })
    it('trims the value', () => {
      expect(applyPlanType({}, '  pro  ')).toEqual({ plan_type: 'pro' })
    })
    it('deletes the key when cleared (empty), keeping other keys', () => {
      const out = applyPlanType({ plan_type: 'pro', email: 'a@b.c' }, '')
      expect(out).toEqual({ email: 'a@b.c' })
      expect('plan_type' in out).toBe(false)
    })
  })
})

describe('video vendor platforms (fal / wavespeed / runware / flova)', () => {
  it('classifies the four vendors and bytedance as video-only, nothing else', () => {
    expect(VIDEO_VENDOR_PLATFORMS).toEqual(['fal', 'wavespeed', 'runware', 'flova'])
    for (const platform of VIDEO_VENDOR_PLATFORMS) {
      expect(isVideoVendorPlatform(platform)).toBe(true)
      expect(isVideoOnlyPlatform(platform)).toBe(true)
    }
    expect(isVideoVendorPlatform('bytedance')).toBe(false)
    expect(isVideoOnlyPlatform('bytedance')).toBe(true)
    for (const platform of ['openai', 'anthropic', 'grok', 'minimax', 'composite', '']) {
      expect(isVideoVendorPlatform(platform)).toBe(false)
      expect(isVideoOnlyPlatform(platform)).toBe(false)
    }
  })

  it('exposes each vendor default base URL for placeholders only', () => {
    expect(videoVendorDefaultBaseUrl('fal')).toBe('https://queue.fal.run')
    expect(videoVendorDefaultBaseUrl('wavespeed')).toBe('https://api.wavespeed.ai/api/v3')
    expect(videoVendorDefaultBaseUrl('runware')).toBe('https://api.runware.ai/v1')
    expect(videoVendorDefaultBaseUrl('flova')).toBe('https://service.flova.ai/api/agent/v2')
    expect(videoVendorDefaultBaseUrl('bytedance')).toBe('')
    expect(videoVendorDefaultBaseUrl('openai')).toBe('')
    expect(Object.keys(VIDEO_VENDOR_DEFAULT_BASE_URLS).sort()).toEqual(['fal', 'flova', 'runware', 'wavespeed'])
  })
})

describe('ByteDance protocol mode', () => {
  describe('readByteDanceProtocolMode', () => {
    it('reads the relay mode', () => {
      expect(readByteDanceProtocolMode({ protocol_mode: 'openai_compatible' })).toBe(
        'openai_compatible'
      )
    })
    it('defaults to native when absent', () => {
      expect(readByteDanceProtocolMode({})).toBe('native')
      expect(readByteDanceProtocolMode(undefined)).toBe('native')
      expect(readByteDanceProtocolMode(null)).toBe('native')
    })
    // The backend treats any unrecognised mode as unroutable, so the form must
    // not present dirty data as a third, selectable state.
    it('falls back to native for unknown or non-string values', () => {
      expect(readByteDanceProtocolMode({ protocol_mode: 'grpc' })).toBe('native')
      expect(readByteDanceProtocolMode({ protocol_mode: 42 })).toBe('native')
      expect(readByteDanceProtocolMode({ protocol_mode: true })).toBe('native')
    })
  })

  describe('byteDanceProtocolRequiresBaseUrl', () => {
    it('requires a host only for the relay mode', () => {
      expect(byteDanceProtocolRequiresBaseUrl('openai_compatible')).toBe(true)
      expect(byteDanceProtocolRequiresBaseUrl('native')).toBe(false)
    })
  })

  describe('applyByteDanceProtocolMode', () => {
    it('create + native: omits the key, since native is the backend default', () => {
      const creds: Record<string, unknown> = { api_key: 'sk' }
      applyByteDanceProtocolMode(creds, 'native', 'create')
      expect(BYTEDANCE_PROTOCOL_MODE_CREDENTIAL_KEY in creds).toBe(false)
    })
    it('create + openai_compatible: writes the key', () => {
      const creds: Record<string, unknown> = { api_key: 'sk' }
      applyByteDanceProtocolMode(creds, 'openai_compatible', 'create')
      expect(creds.protocol_mode).toBe('openai_compatible')
    })
    it('edit + native: deletes a previously stored relay mode', () => {
      const creds: Record<string, unknown> = { api_key: 'sk', protocol_mode: 'openai_compatible' }
      applyByteDanceProtocolMode(creds, 'native', 'edit')
      expect(BYTEDANCE_PROTOCOL_MODE_CREDENTIAL_KEY in creds).toBe(false)
    })
    it('edit + native + key absent: does not throw', () => {
      const creds: Record<string, unknown> = { api_key: 'sk' }
      applyByteDanceProtocolMode(creds, 'native', 'edit')
      expect(BYTEDANCE_PROTOCOL_MODE_CREDENTIAL_KEY in creds).toBe(false)
    })
    it('preserves unrelated credentials', () => {
      const creds: Record<string, unknown> = { api_key: 'sk', base_url: 'https://relay/v1' }
      applyByteDanceProtocolMode(creds, 'openai_compatible', 'edit')
      expect(creds).toEqual({
        api_key: 'sk',
        base_url: 'https://relay/v1',
        protocol_mode: 'openai_compatible'
      })
    })
  })
})

describe('Codex subscription analytics labels', () => {
  it.each([
    ['business', 'Business'], ['self_serve_business_prolite', 'Business'],
    ['enterprise_cbp_automation', 'Enterprise'], ['ent26', 'Enterprise'],
    ['edu', 'Education'], ['edu_plus', 'Education'], ['edu_pro', 'Education'],
    ['unknown', 'Account'], ['promax', 'Pro 500']
  ])('groups %s without changing its status label', (sku, label) => {
    expect(openAIPlanTypeLabel(sku, 'analytics')).toBe(label)
  })
})
