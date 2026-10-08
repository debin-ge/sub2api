// ========== Flova（Agent API）凭据 ==========
// Flova 与 fal / WaveSpeed / Runware 同类（仅视频、强制 apikey、base_url 可空），
// 但多出一组运行参数：上传地址、环境名、Skill ID、运行时限与积分门槛。
// 键名与默认值须与后端 flova 凭据解析保持一致（方案 9.1）。

export const FLOVA_DEFAULT_BASE_URL = 'https://service.flova.ai/api/agent/v2'
export const FLOVA_DEFAULT_UPLOAD_URL = 'https://gcp.service.flova.ai/api/agent/v2/upload'
export const FLOVA_DEFAULT_CLIENT_VERSION = 'v0.0.10'

/** 方案 9.1 的全部键与默认值；api_key 必填，不在此表中。 */
export const FLOVA_CREDENTIAL_DEFAULTS = {
  base_url: FLOVA_DEFAULT_BASE_URL,
  upload_url: FLOVA_DEFAULT_UPLOAD_URL,
  env_name: '',
  skill_id: '',
  max_run_minutes: 60,
  auto_confirm_high_cost: true,
  price_drift_tolerance: 0.1,
  auto_confirm_credits_max: 0,
  round_fee_credits: 0,
  min_credits_reserve: 60,
  client_version: FLOVA_DEFAULT_CLIENT_VERSION
} as const

/**
 * 表单里可编辑的 flova 专属字段（api_key / base_url 走通用 apikey 表单）。
 * 数值字段允许为 null（输入框清空），构建凭据时回落到默认值。
 */
export interface FlovaCredentialForm {
  upload_url: string
  env_name: string
  skill_id: string
  max_run_minutes: number | null
  auto_confirm_high_cost: boolean
  price_drift_tolerance: number | null
  auto_confirm_credits_max: number | null
  round_fee_credits: number | null
  min_credits_reserve: number | null
}

export const FLOVA_FORM_CREDENTIAL_KEYS = [
  'upload_url',
  'env_name',
  'skill_id',
  'max_run_minutes',
  'auto_confirm_high_cost',
  'price_drift_tolerance',
  'auto_confirm_credits_max',
  'round_fee_credits',
  'min_credits_reserve'
] as const

export function defaultFlovaCredentialForm(): FlovaCredentialForm {
  return {
    // 上传地址留空即由后端回落到官方地址，与 base_url 同一约定；默认值只做占位提示。
    upload_url: '',
    env_name: FLOVA_CREDENTIAL_DEFAULTS.env_name,
    skill_id: FLOVA_CREDENTIAL_DEFAULTS.skill_id,
    max_run_minutes: FLOVA_CREDENTIAL_DEFAULTS.max_run_minutes,
    auto_confirm_high_cost: FLOVA_CREDENTIAL_DEFAULTS.auto_confirm_high_cost,
    price_drift_tolerance: FLOVA_CREDENTIAL_DEFAULTS.price_drift_tolerance,
    auto_confirm_credits_max: FLOVA_CREDENTIAL_DEFAULTS.auto_confirm_credits_max,
    round_fee_credits: FLOVA_CREDENTIAL_DEFAULTS.round_fee_credits,
    min_credits_reserve: FLOVA_CREDENTIAL_DEFAULTS.min_credits_reserve
  }
}

function readString(value: unknown, fallback: string): string {
  return typeof value === 'string' ? value : fallback
}

/** 接受 number 与数字字符串（v-model.number 清空后会留下 ''），其余回落。 */
function readNumber(value: unknown, fallback: number | null): number | null {
  if (typeof value === 'number') return Number.isFinite(value) ? value : fallback
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) return fallback
    const parsed = Number(trimmed)
    return Number.isFinite(parsed) ? parsed : fallback
  }
  return fallback
}

function readBoolean(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

/** 从已存凭据回显表单（编辑弹窗）；缺失或脏数据一律回落到默认值。 */
export function readFlovaCredentialForm(
  credentials: Record<string, unknown> | undefined | null
): FlovaCredentialForm {
  const defaults = defaultFlovaCredentialForm()
  const c = credentials ?? {}
  return {
    upload_url: readString(c.upload_url, defaults.upload_url),
    env_name: readString(c.env_name, defaults.env_name),
    skill_id: readString(c.skill_id, defaults.skill_id),
    max_run_minutes: readNumber(c.max_run_minutes, defaults.max_run_minutes),
    auto_confirm_high_cost: readBoolean(c.auto_confirm_high_cost, defaults.auto_confirm_high_cost),
    price_drift_tolerance: readNumber(c.price_drift_tolerance, defaults.price_drift_tolerance),
    auto_confirm_credits_max: readNumber(c.auto_confirm_credits_max, defaults.auto_confirm_credits_max),
    round_fee_credits: readNumber(c.round_fee_credits, defaults.round_fee_credits),
    min_credits_reserve: readNumber(c.min_credits_reserve, defaults.min_credits_reserve)
  }
}

/**
 * 由表单构建 flova 专属凭据片段（不含 api_key / base_url / model_mapping 等通用键）。
 * 字符串去空白；数值清空回落默认值；全部键都写出，便于后端与方案 9.1 逐键对齐。
 */
export function buildFlovaCredentials(form: FlovaCredentialForm): Record<string, unknown> {
  const d = FLOVA_CREDENTIAL_DEFAULTS
  return {
    upload_url: form.upload_url.trim(),
    env_name: form.env_name.trim(),
    skill_id: form.skill_id.trim(),
    max_run_minutes: readNumber(form.max_run_minutes, d.max_run_minutes),
    auto_confirm_high_cost: form.auto_confirm_high_cost === true,
    price_drift_tolerance: readNumber(form.price_drift_tolerance, d.price_drift_tolerance),
    auto_confirm_credits_max: readNumber(form.auto_confirm_credits_max, d.auto_confirm_credits_max),
    round_fee_credits: readNumber(form.round_fee_credits, d.round_fee_credits),
    min_credits_reserve: readNumber(form.min_credits_reserve, d.min_credits_reserve)
  }
}

/** 把表单字段合并进凭据对象（创建 / 编辑共用；编辑时覆盖同名旧值，其余键原样保留）。 */
export function applyFlovaCredentials(
  credentials: Record<string, unknown>,
  form: FlovaCredentialForm
): void {
  Object.assign(credentials, buildFlovaCredentials(form))
}

function isHttpUrl(value: unknown): boolean {
  if (typeof value !== 'string') return false
  const trimmed = value.trim()
  if (!trimmed) return true
  try {
    const url = new URL(trimmed)
    return (url.protocol === 'http:' || url.protocol === 'https:') && !!url.host
  } catch {
    return false
  }
}

function isNonNegativeNumber(value: unknown): boolean {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0
}

/**
 * 校验 flova 凭据（接受 buildFlovaCredentials 的产物或完整凭据对象）。
 * 返回 i18n 键（admin.accounts.flova.errors.*）或 null。缺失的键视为采用默认值，不报错。
 */
export function validateFlovaCredentials(credentials: Record<string, unknown>): string | null {
  if ('base_url' in credentials && !isHttpUrl(credentials.base_url)) {
    return 'admin.accounts.flova.errors.invalidBaseUrl'
  }
  if ('upload_url' in credentials && !isHttpUrl(credentials.upload_url)) {
    return 'admin.accounts.flova.errors.invalidUploadUrl'
  }
  if ('max_run_minutes' in credentials) {
    const v = credentials.max_run_minutes
    if (typeof v !== 'number' || !Number.isFinite(v) || v <= 0) {
      return 'admin.accounts.flova.errors.invalidMaxRunMinutes'
    }
  }
  if ('price_drift_tolerance' in credentials) {
    const v = credentials.price_drift_tolerance
    if (typeof v !== 'number' || !Number.isFinite(v) || v < 0 || v > 1) {
      return 'admin.accounts.flova.errors.invalidPriceDriftTolerance'
    }
  }
  if ('auto_confirm_credits_max' in credentials && !isNonNegativeNumber(credentials.auto_confirm_credits_max)) {
    return 'admin.accounts.flova.errors.negativeAutoConfirmCreditsMax'
  }
  if ('round_fee_credits' in credentials && !isNonNegativeNumber(credentials.round_fee_credits)) {
    return 'admin.accounts.flova.errors.negativeRoundFeeCredits'
  }
  if ('min_credits_reserve' in credentials && !isNonNegativeNumber(credentials.min_credits_reserve)) {
    return 'admin.accounts.flova.errors.negativeMinCreditsReserve'
  }
  return null
}

/** 读取积分同步快照（extra.flova_credits / extra.flova_subscription.lookup_key）。 */
export function readFlovaCreditsSnapshot(
  extra: Record<string, unknown> | undefined | null
): { credits: number; plan: string } | null {
  const credits = extra?.flova_credits
  if (typeof credits !== 'number' || !Number.isFinite(credits)) return null
  const subscription = extra?.flova_subscription
  const lookupKey =
    subscription && typeof subscription === 'object'
      ? (subscription as Record<string, unknown>).lookup_key
      : undefined
  return { credits, plan: typeof lookupKey === 'string' ? lookupKey : '' }
}
