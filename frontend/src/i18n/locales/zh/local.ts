// 上游模块化语言包中不存在的本地二开文案。
// 此文件仅做增量补充；如键重叠，以上游模块文案为准。
export default {
  "radar": {
    "pageTitle": "模型雷达",
    "common": {
      "date": "日期",
      "never": "尚未获取",
      "stale": "数据可能已过期",
      "unknownTime": "未知时间"
    },
    "header": {
      "lightTheme": "切换至浅色主题",
      "darkTheme": "切换至深色主题",
      "dashboard": "控制台",
      "login": "登录",
      "sectionNavigation": "雷达页面导航",
      "health": "服务健康",
      "quota": "额度雷达",
      "degradation": "降智雷达"
    },
    "hero": {
      "eyebrow": "公共数据看板",
      "title": "模型雷达",
      "description": "跟踪服务健康、匿名额度聚合，以及公开模型评测指标的变化。",
      "clientUpdated": "页面数据获取时间"
    },
    "state": {
      "loading": "数据加载中",
      "empty": "暂无可用数据",
      "retry": "重试"
    },
    "error": {
      "title": "雷达数据加载失败",
      "safeReason": "公共数据源暂时不可用，请稍后重试。",
      "loadFailed": "此模块加载失败",
      "generic": "此模块暂时不可用"
    },
    "health": {
      "title": "服务健康",
      "subtitle": "查看当前已添加模型平台与厂商的服务状态。",
      "empty": "当前没有可展示的已添加模型平台。",
      "updated": "更新时间",
      "uptime": "90 天可用率",
      "lastIncident": "最近事故",
      "history30d": "近 30 天历史",
      "dailyStatus": "每日状态",
      "statusLegend": "状态标识",
      "incidents30d": "起事件",
      "incidents": "起事件",
      "closeHistory": "关闭历史详情",
      "moreIncidents": "起其他事件",
      "historyUnavailable": "该日期暂无可靠的历史覆盖数据。",
      "noIncidents": "该日期未报告服务事件。",
      "status": {
        "operational": "正常",
        "degraded": "性能降级",
        "partialOutage": "部分故障",
        "majorOutage": "重大故障",
        "maintenance": "维护中",
        "unknown": "状态未知"
      }
    },
    "quota": {
      "title": "额度雷达",
      "subtitle": "按套餐展示 5H / 7D API 等值金额估算与样本数。",
      "emptyPending": "暂无额度数据。服务启动后会立即执行聚合，请稍后重试。",
      "emptyNoPublishable": "暂无可公开的额度数据。套餐桶需要近期被动额度快照，并满足对应的最小样本要求。",
      "emptyFailed": "额度聚合暂时不可用，请稍后重试。",
      "emptyDisabled": "额度聚合当前已关闭。",
      "accounts": "账号数",
      "snapshotStale": "额度快照可能已过期",
      "smallSample": "样本量较小",
      "singleSampleLowConfidence": "单样本估算 · 低置信度",
      "fiveHourUtilization": "5 小时利用率",
      "sevenDayUtilization": "7 天利用率",
      "inferredLimit": "API 等值金额估算",
      "inference": {
        "insufficient": "样本不足",
        "dispersed": "数据离散度过高",
        "invalid": "聚合结果无效",
        "unknownPlan": "套餐未知，暂不提供估算",
        "unavailable": "暂无可信结果"
      },
      "noWindow": "当前额度窗口暂无数据",
      "sevenDayTrend": "近 7 天的 5 小时利用率趋势",
      "sevenDayQuotaTrend": "7 天额度利用率趋势",
      "trendStale": "趋势数据可能已过期",
      "sparklineLabel": "额度利用率趋势",
      "trendRange": "时间范围",
      "trendPoints": "个数据点",
      "trendLatest": "最新值",
      "trendMinimum": "最小值",
      "trendMaximum": "最大值",
      "noTrend": "暂无趋势数据",
      "openDetails": "查看详情",
      "capturedAt": "快照时间",
      "closeDetails": "关闭额度详情",
      "windowTabs": "额度窗口",
      "window5h": "5 小时",
      "window7d": "7 天",
      "averageUtilization": "平均利用率",
      "range": "利用率范围",
      "averageCost": "平均消费金额",
      "sampleSize": "样本量",
      "modelUtilization": "Anthropic 模型利用率",
      "breakdown": "模型消费分解",
      "model": "模型",
      "averageRequests": "平均请求数",
      "share": "消费占比",
      "noBreakdown": "暂无模型消费分解数据",
      "trend": "趋势"
    },
    "trend": {
      "loading": "趋势数据加载中",
      "error": "趋势数据加载失败"
    },
    "degradation": {
      "title": "降智雷达",
      "subtitle": "展示 Artificial Analysis 与模型广场交集模型的当前指标，以及模型榜单排名。",
      "tabs": "模型评测视图",
      "overview": "指数概览",
      "lmarena": "模型榜单",
      "loading": "评测数据加载中",
      "error": "评测数据加载失败",
      "chartTitle": "指标对比",
      "scaleHint": "各项指数均为 0–100 分制",
      "radarLabel": "模型评测指标对比图表",
      "empty": "等待评测数据",
      "emptyIntersection": "暂无三项指标完整且能与模型广场匹配的 Artificial Analysis 模型。",
      "stale": "数据可能已过期",
      "projectFetchedAt": "项目获取时间",
      "selectModels": "选择模型",
      "searchModels": "搜索 AA 或模型广场模型",
      "selectionLimit": "最多同时比较 10 个模型",
      "removeModel": "移除模型",
      "noSearchResults": "没有匹配的模型",
      "intelligence": "智能指数",
      "intelligenceShort": "智能",
      "intelligenceDescription": "综合衡量模型在广泛推理与知识评测中的表现。",
      "coding": "编码指数",
      "codingShort": "编码",
      "codingDescription": "衡量模型在软件开发与代码生成评测中的表现。",
      "agentic": "代理指数",
      "agenticShort": "代理",
      "agenticDescription": "衡量模型在多步骤智能体与工具使用评测中的表现。"
    },
    "lmarena": {
      "loading": "模型榜单加载中",
      "error": "模型榜单加载失败",
      "totalVotes": "榜单模型票数合计",
      "fetchedAt": "抓取时间",
      "rank": "排名",
      "model": "模型",
      "vendor": "厂商",
      "elo": "Elo",
      "confidence": "置信区间",
      "votes": "投票数",
      "empty": "榜单中暂无与当前模型广场匹配的模型。"
    },
    "sources": {
      "title": "数据来源",
      "every": "每隔",
      "lastAttempt": "最近尝试时间",
      "lastSuccess": "最近成功时间",
      "nextRun": "下次计划执行时间",
      "aggregatedUsage": "Sub2API 聚合用量",
      "empty": "暂无数据源元信息",
      "healthy": "正常",
      "failed": "获取失败",
      "notConfigured": "未配置",
      "neverAttempted": "尚未尝试",
      "stale": "数据陈旧",
      "error": {
        "network": "网络错误",
        "unauthorized": "数据源授权失败",
        "rateLimited": "数据源请求受到限流",
        "invalidResponse": "数据源响应无效",
        "upstream": "上游数据源错误",
        "aggregation": "用量聚合失败",
        "generic": "数据源不可用"
      },
      "disclaimer": "数据聚合自站内匿名统计与公开第三方来源。模型评测结果受评测方法影响；API 等值金额为统计估算，并非厂商官方额度上限或承诺，不应作为关键业务决策的唯一依据。"
    }
  },
  "plaza": {
    "title": "模型广场",
    "header": {
      "label": "模型广场",
      "home": "首页",
      "docs": "文档",
      "dashboard": "控制台",
      "register": "注册"
    },
    "hero": {
      "eyebrow": "公开模型目录",
      "title": "模型广场",
      "subtitle": "Token 价格默认按每 100 万 tokens 展示，按次计费会单独标注。人民币金额仅基于公开设置估算展示，不影响后端实际计费。",
      "rateTag": "¥1 = ${rate}",
      "boostValue": "{boost}倍"
    },
    "metrics": {
      "models": "模型总数",
      "platforms": "供应商",
      "boost": "充值加成"
    },
    "card": {
      "input": "输入",
      "output": "输出",
      "cacheWrite": "缓存写入",
      "cacheRead": "缓存读取",
      "imageOutput": "图像输出",
      "perRequest": "按次计费",
      "supportedChannels": "{n} 个渠道",
      "viewDetails": "查看详情",
      "billingPerToken": "按量计费",
      "billingPerRequest": "按次计费",
      "discountBadge": "约 {discount} 折",
      "notAvailable": "暂无",
      "recentCalls": "近 7 日 {count} 次调用"
    },
    "searchBar": {
      "total": "共 {total} 个模型",
      "filtered": "{visible}/{total} 个模型"
    },
    "infoBanner": {
      "text": "同一模型在不同渠道可能有不同价格，默认展示最低价。点击卡片查看该模型在所有渠道的具体价格。"
    },
    "modal": {
      "close": "关闭",
      "fullPricing": "完整定价",
      "input": "输入",
      "output": "输出",
      "cacheWrite": "缓存写入",
      "cacheRead": "缓存读取",
      "imageOutput": "图像输出",
      "perRequest": "按次计费",
      "supportedChannels": "支持该模型的渠道",
      "channelsCount": "{n} 个渠道",
      "tieredPricing": "分层定价",
      "tierRange": "{min} - {max} tokens",
      "tierRangeOpenEnded": "{min}+ tokens"
    },
    "price": {
      "unitPerMillion": "/1M",
      "unitPerRequest": "/次"
    },
    "platform": {
      "modelCount": "{n} 个模型"
    },
    "loading": "模型加载中...",
    "filters": {
      "title": "筛选",
      "subtitle": "按供应商和排序方式筛选模型",
      "search": "搜索模型",
      "searchPlaceholder": "搜索模型名称...",
      "platform": "供应商",
      "allPlatforms": "全部",
      "sort": "排序",
      "sortPopularity": "近 7 日热度",
      "sortDefault": "按名称",
      "sortInputAsc": "输入价格从低到高",
      "sortInputDesc": "输入价格从高到低"
    },
    "footer": {
      "scaleNote": "Token 价格默认按每 100 万 tokens 展示，按次计费会单独标注。",
      "referenceRateDisclaimer": "人民币金额仅基于公开设置估算展示，不影响后端实际计费。",
      "registerCta": "创建账号",
      "rechargeCta": "充值"
    },
    "empty": {
      "title": "暂无公开模型",
      "subtitle": "发布可用渠道后，公开模型数据会展示在这里。",
      "filteredTitle": "没有匹配的模型",
      "filteredSubtitle": "请尝试其他平台、关键词或排序方式。"
    },
    "unavailable": {
      "title": "模型广场暂不可用",
      "subtitle": "当前部署未开启公开可用渠道浏览。"
    },
    "error": {
      "title": "模型广场加载失败",
      "subtitle": "请刷新页面或稍后重试。"
    }
  },
  "home": {
    "apps": "应用集成",
    "viewApps": "查看应用集成",
    "providers": {
      "grok": "Grok",
      "minimax": "MiniMax",
      "glm": "GLM",
      "kimi": "Kimi",
      "deepseek": "DeepSeek",
      "windsurf": "Windsurf",
      "opencode": "OpenCode"
    }
  },
  "common": {
    "apply": "应用",
    "clear": "清空",
    "creating": "创建中...",
    "login": "登录",
    "required": "必填",
    "sending": "发送中...",
    "tryAgain": "重试"
  },
  "nav": {
    "apps": "应用集成"
  },
  "monitorCommon": {
    "providers": {
      "minimax": "MiniMax",
      "glm": "GLM",
      "kimi": "Kimi",
      "deepseek": "DeepSeek",
      "windsurf": "Windsurf",
      "opencode": "OpenCode"
    }
  },
  "admin": {
    "dashboard": {
      "upstreamBalance": "上游账户余额",
      "upstreamBalanceConnected": "已连接",
      "upstreamBalanceDisabled": "子站模式未启用",
      "upstreamBalanceNotConfigured": "缺少上游 Endpoint 或 API Key",
      "upstreamBalanceAuthFailed": "上游 API Key 无效",
      "upstreamBalanceUnreachable": "上游不可达",
      "upstreamBalanceInvalidResponse": "上游余额响应格式异常",
      "upstreamBalanceError": "上游返回错误"
    },
    "users": {
      "passwordCopied": "密码已复制"
    },
    "groups": {
      "platforms": {
        "minimax": "MiniMax",
        "glm": "GLM",
        "kimi": "Kimi",
        "deepseek": "DeepSeek",
        "windsurf": "Windsurf",
        "opencode": "OpenCode"
      }
    },
    "channels": {
      "emptyModelsInPricing": "请至少配置一个模型价格。",
      "noGroupsSelected": "请至少选择一个分组。"
    },
    "accounts": {
      "capacity": {
        "minimax": {
          "exhausted": "MiniMax 官方 5h 请求余量已用完",
          "warning": "MiniMax 官方 5h 请求余量偏低",
          "normal": "MiniMax 官方 5h 请求余量正常"
        },
        "deepseek": {
          "normal": "DeepSeek 官方余额可用",
          "unavailable": "DeepSeek 官方余额不可用",
          "error": "DeepSeek 官方余额检测失败"
        }
      },
      "platforms": {
        "minimax": "MiniMax",
        "glm": "GLM",
        "kimi": "Kimi",
        "deepseek": "DeepSeek",
        "windsurf": "Windsurf",
        "opencode": "OpenCode"
      },
      "syncMiniMaxRemains": "同步 MiniMax 余量",
      "syncMiniMaxRemainsSuccess": "MiniMax 余量已同步",
      "syncMiniMaxRemainsFailed": "同步 MiniMax 余量失败",
      "openai": {
        "codexCLIOnlyAllowClaudeCode": "额外放行 Claude Code 的 Codex 插件",
        "codexCLIOnlyAllowClaudeCodeDesc": "仅在上方开关开启时生效。额外放行通过 Claude Code 的 Codex 插件发起的请求（精确匹配 originator=Claude Code），不影响对其他非官方客户端的拦截。",
        "codexImageGenerationBridge": "Codex 图片生成桥接",
        "codexImageGenerationBridgeDesc": "账号级策略优先于渠道和全局配置。仅控制 Codex 走 /responses 文本端点时是否注入 image_generation 工具；不影响独立图片生成接口。",
        "codexImageGenerationBridgeInherit": "跟随渠道",
        "codexImageGenerationBridgeInheritDesc": "不写入账号覆盖，继续使用渠道或全局策略。",
        "codexImageGenerationBridgeEnabled": "强制开启",
        "codexImageGenerationBridgeEnabledDesc": "允许 Codex /responses 请求获得图片工具注入。",
        "codexImageGenerationBridgeDisabled": "强制关闭",
        "codexImageGenerationBridgeDisabledDesc": "阻断 Codex /responses 的图片工具注入。",
        "codexImageGenerationBridgeBadgeInherit": "渠道策略",
        "codexImageGenerationBridgeBadgeEnabled": "账号开启",
        "codexImageGenerationBridgeBadgeDisabled": "账号关闭"
      },
      "glm": {
        "apiKeyHint": "您的 GLM Coding Plan API Key"
      },
      "kimi": {
        "apiKeyHint": "您的 Kimi Coding Plan API Key"
      },
      "deepseek": {
        "apiKeyHint": "您的 DeepSeek API Key"
      },
      "windsurf": {
        "apiKeyHint": "您的 Windsurf 反代 API Key"
      },
      "opencode": {
        "apiKeyHint": "您的 OpenCode2API API Key"
      },
      "failedToLoadModels": "加载最新支持模型失败",
      "glmAccount": "GLM 账号",
      "fromModel": "源模型",
      "headerOverride": {
        "fillTemplate": "填入示例"
      },
      "toModel": "目标模型"
    },
    "ops": {
      "errorDetail": {
        "attemptedKeyPrefix": "尝试的 Key 前缀",
        "deletedKeyOwner": "已删除 Key 所有者"
      },
      "runtime": {
        "metricThresholds": "指标阈值",
        "metricThresholdsHint": "配置运行时健康评分和告警显示使用的阈值。",
        "requestErrorRateMaxPercent": "请求错误率上限（%）",
        "requestErrorRateMaxPercentHint": "超过该比例时请求错误率指标会标记为异常。",
        "slaMinPercent": "SLA 最低百分比",
        "slaMinPercentHint": "低于该值时 SLA 指标会标记为异常。",
        "ttftP99MaxMs": "TTFT P99 上限（毫秒）",
        "ttftP99MaxMsHint": "超过该值时首 token 延迟指标会标记为异常。",
        "upstreamErrorRateMaxPercent": "上游错误率上限（%）",
        "upstreamErrorRateMaxPercentHint": "超过该比例时上游错误率指标会标记为异常。"
      },
      "settings": {
        "ignoreInvalidApiKeyErrors": "忽略无效 API Key 错误",
        "ignoreInvalidApiKeyErrorsHint": "启用后，无效或缺失 API Key 的错误（INVALID_API_KEY、API_KEY_REQUIRED）将不会写入错误日志。"
      }
    },
    "settings": {
      "tabs": {
        "reseller": "子站配置"
      },
      "reseller": {
        "title": "子站上游配置",
        "description": "配置子站连接上级供应商的参数，包括上游 Endpoint 和 API Key。",
        "enabledLabel": "启用子站模式",
        "enabledHint": "开启后将作为子站连接到上游供应商",
        "endpointLabel": "上游 Endpoint",
        "endpointPlaceholder": "https://parent.example.com",
        "endpointHint": "上游供应商的 API 基础地址",
        "apiKeyLabel": "上游 API Key",
        "apiKeyPlaceholder": "填写上游 API Key",
        "apiKeyConfigured": "API Key 已配置",
        "apiKeyConfiguredHint": "密钥已配置，留空以保留当前值",
        "apiKeyHint": "填写后会覆盖当前 API Key",
        "mode": "子站模式",
        "endpoint": "上游 Endpoint",
        "apiKey": "上游 API Key",
        "apiKeyNotConfigured": "未配置",
        "testConnection": "测试连接",
        "upstreamBalance": "上游账户余额",
        "localBalanceHint": "该余额属于子站在上级供应商中的账户，不是本地用户余额。",
        "status": {
          "ok": "已连接",
          "disabled": "子站模式未启用",
          "notConfigured": "缺少上游 Endpoint 或 API Key",
          "authFailed": "上游 API Key 无效",
          "unreachable": "上游不可达",
          "invalidResponse": "上游余额响应格式异常",
          "upstreamError": "上游返回错误"
        }
      },
      "features": {
        "radar": {
          "title": "模型雷达运维",
          "description": "控制雷达后台调度、安全触发手动刷新，并查看各数据源的执行状态。",
          "loading": "正在加载雷达状态…",
          "loadError": "雷达状态暂时不可用。",
          "retry": "重试",
          "enabled": "启用雷达定时更新",
          "enabledHint": "关闭后停止后续定时任务并保留现有数据；仍可使用手动刷新。",
          "updateError": "无法更新雷达运行时设置，已恢复为原值。",
          "sourcesTitle": "数据源执行状态",
          "never": "从未",
          "unavailable": "不可用",
          "none": "无",
          "status": {
            "healthy": "健康",
            "failed": "失败",
            "never_attempted": "尚未执行",
            "stale": "陈旧快照"
          },
          "fields": {
            "lastSuccess": "最近成功",
            "lastFailure": "最近失败",
            "nextFire": "下次计划执行",
            "error": "安全错误"
          },
          "sources": {
            "aa": "Artificial Analysis 模型",
            "lmarena": "LMArena",
            "status_claude": "Claude 状态页",
            "status_openai": "OpenAI 状态页",
            "quota_aggregator": "Sub2API 聚合用量"
          },
          "errors": {
            "network_error": "网络错误",
            "unauthorized": "鉴权被拒绝",
            "rate_limited": "触发限流",
            "invalid_response": "响应无效",
            "upstream_error": "上游错误",
            "aggregation_error": "聚合错误"
          },
          "refresh": {
            "action": "立即刷新",
            "pending": "正在请求刷新…",
            "triggered": "手动刷新已启动。",
            "coalesced": "已有刷新任务正在运行，本次请求已合并。",
            "error": "无法请求手动刷新。",
            "id": "刷新 ID：{id}",
            "tasks": "任务：{tasks}"
          }
        }
      },
      "registration": {
        "rateLimit": "注册速率限制",
        "rateLimitHint": "限制单个 IP、单个邮箱地址及邮箱域名的注册/验证码频率，防止批量注册薅羊毛。域名级为高阈值兜底，请勿设置过低以免误伤正常用户",
        "rateLimitPerIp": "每 IP 注册次数上限",
        "rateLimitWindowIp": "每 IP 时间窗口（秒）",
        "rateLimitPerEmail": "每邮箱地址次数上限",
        "rateLimitWindowEmail": "每邮箱地址时间窗口（秒）",
        "rateLimitPerEmailDomain": "每邮箱域名次数上限（反批量）",
        "rateLimitWindowEmailDomain": "每邮箱域名时间窗口（秒）"
      },
      "gatewayForwarding": {
        "openaiAllowClaudeCodeCodexPlugin": "允许在 Claude Code 中使用 Codex 插件",
        "openaiAllowClaudeCodeCodexPluginDesc": "全局开关，仅对已开启「仅允许 Codex 官方客户端」的 OpenAI OAuth 账号生效。开启后，所有此类账号都额外放行通过 Claude Code 的 Codex 插件发起的请求（精确匹配 originator=Claude Code），无需逐账号配置；上游请求仍保持透传。"
      },
      "payment": {
        "providerWise": "Wise",
        "field_quickPayBaseUrl": "Quick Pay 基础链接",
        "field_apiToken": "API Token",
        "field_profileId": "Profile ID",
        "field_balanceId": "Balance ID",
        "field_environment": "环境",
        "field_webhookPublicKey": "Webhook 公钥",
        "field_settlementStrategy": "结算策略",
        "field_allowedMethodsNote": "允许方式备注",
        "field_reconcileWindowHours": "对账窗口小时数",
        "field_autoFulfillFeePayments": "手续费支付自动入账",
        "field_wiseQuickPayBaseUrlHint": "Wise Quick Pay / payment link 基础链接，系统会自动追加 amount、currency，以及纯字母数字订单 reference 作为 description。",
        "field_wiseApiBaseHint": "Wise API 地址，生产环境通常为 https://api.wise.com",
        "field_wiseWebhookPublicKeyHint": "可选覆盖 Wise Webhook RSA 公钥；留空时使用系统内置公钥配置，不是必填密钥。",
        "field_wiseSettlementStrategyHint": "v1 固定 exact_only，仅到账金额等于订单金额时自动入账",
        "field_wiseAllowedMethodsNoteHint": "仅作为运营备注。v1 应只启用 Wise balance / bank transfer 这类到账金额等于订单金额的付款方式。",
        "field_wiseReconcileWindowHoursHint": "Statement 查询窗口，单位小时。默认 72。",
        "field_wiseAutoFulfillFeePaymentsHint": "为后续 card / Apple Pay / Google Pay 手续费策略预留。v1 必须保持 false；后端会拒绝 true。",
        "wiseWebhookHint": "保存并启用 Wise 服务商后，系统会自动创建以下 Webhook 订阅地址。",
        "wiseWebhookSubscriptionTitle": "Wise Webhook 自动订阅",
        "wiseWebhookSubscriptionActive": "已自动订阅",
        "wiseWebhookSubscriptionFailed": "订阅失败",
        "wiseWebhookSubscriptionUnknown": "尚未同步",
        "wiseWebhookSubscriptionId": "订阅 ID",
        "wiseWebhookSubscriptionError": "失败原因",
        "wiseWebhookDeliveryUrl": "投递 URL",
        "wiseGuideSummary": "Wise v1 使用 hosted redirect + 自动对账，仅建议启用 Wise balance / bank transfer。",
        "wiseGuideNote": "请勿在 v1 启用 card / Apple Pay / Google Pay 自动入账；手续费扣减交易将进入人工审核。"
      }
    }
  },
  "payment": {
    "wisePaymentNoticeTitle": "Wise 支付提示",
    "wisePaymentNoticeBody": "请在 Wise 页面确认金额和币种，不要修改金额。v1 仅自动处理 Wise balance / bank transfer；使用 card、Apple Pay 或 Google Pay 可能产生手续费扣减，系统不会自动入账。",
    "methods": {
      "wise": "Wise"
    },
    "admin": {
      "wiseReconcile": {
        "title": "Wise 对账详情",
        "manualReviewWarning": "该 Wise 交易需要人工审核；手续费扣减、金额不一致或元数据不一致时不会自动入账。",
        "decision": "对账决策",
        "reason": "原因",
        "transactionId": "Wise 交易号",
        "grossAmount": "到账前金额",
        "feeAmount": "手续费",
        "netAmount": "到账金额",
        "currency": "币种",
        "status": "交易状态",
        "description": "描述",
        "reference": "付款备注",
        "occurredAt": "交易时间"
      },
      "validityDays": "有效期（天）",
      "validityDaysRequired": "有效期天数必须大于 0"
    }
  }
} as const
