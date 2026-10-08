import { describe, expect, it } from 'vitest'
import {
  FLOVA_CREDENTIAL_DEFAULTS,
  FLOVA_DEFAULT_BASE_URL,
  FLOVA_DEFAULT_UPLOAD_URL,
  FLOVA_FORM_CREDENTIAL_KEYS,
  applyFlovaCredentials,
  buildFlovaCredentials,
  defaultFlovaCredentialForm,
  readFlovaCreditsSnapshot,
  readFlovaCredentialForm,
  validateFlovaCredentials
} from '../flovaCredentials'

describe('flovaCredentials', () => {
  it('exposes the spec 9.1 defaults', () => {
    expect(FLOVA_DEFAULT_BASE_URL).toBe('https://service.flova.ai/api/agent/v2')
    expect(FLOVA_DEFAULT_UPLOAD_URL).toBe('https://gcp.service.flova.ai/api/agent/v2/upload')
    expect(FLOVA_CREDENTIAL_DEFAULTS).toEqual({
      base_url: 'https://service.flova.ai/api/agent/v2',
      upload_url: 'https://gcp.service.flova.ai/api/agent/v2/upload',
      env_name: '',
      skill_id: '',
      max_run_minutes: 60,
      auto_confirm_high_cost: true,
      price_drift_tolerance: 0.1,
      auto_confirm_credits_max: 0,
      round_fee_credits: 0,
      min_credits_reserve: 60,
      client_version: 'v0.0.10'
    })
  })

  it('builds the form-driven keys with defaults when the form is untouched', () => {
    const built = buildFlovaCredentials(defaultFlovaCredentialForm())
    expect(Object.keys(built).sort()).toEqual([...FLOVA_FORM_CREDENTIAL_KEYS].sort())
    expect(built).toEqual({
      upload_url: '',
      env_name: '',
      skill_id: '',
      max_run_minutes: 60,
      auto_confirm_high_cost: true,
      price_drift_tolerance: 0.1,
      auto_confirm_credits_max: 0,
      round_fee_credits: 0,
      min_credits_reserve: 60
    })
    // api_key / base_url / client_version 不由这里产出
    expect('api_key' in built).toBe(false)
    expect('base_url' in built).toBe(false)
    expect('client_version' in built).toBe(false)
  })

  it('trims strings and falls back to defaults for cleared numeric inputs', () => {
    const form = defaultFlovaCredentialForm()
    form.upload_url = '  https://upload.example.com/v2/upload  '
    form.env_name = ' prod '
    form.skill_id = ' skill_1 '
    form.max_run_minutes = null
    form.price_drift_tolerance = '' as unknown as number
    form.min_credits_reserve = 120
    form.auto_confirm_high_cost = false

    expect(buildFlovaCredentials(form)).toEqual({
      upload_url: 'https://upload.example.com/v2/upload',
      env_name: 'prod',
      skill_id: 'skill_1',
      max_run_minutes: 60,
      auto_confirm_high_cost: false,
      price_drift_tolerance: 0.1,
      auto_confirm_credits_max: 0,
      round_fee_credits: 0,
      min_credits_reserve: 120
    })
  })

  it('reads stored credentials back into the form, ignoring dirty values', () => {
    const form = readFlovaCredentialForm({
      upload_url: 'https://upload.example.com',
      env_name: 'staging',
      skill_id: 'skill_9',
      max_run_minutes: '45',
      auto_confirm_high_cost: 'yes',
      price_drift_tolerance: 0.25,
      auto_confirm_credits_max: null,
      round_fee_credits: 3,
      min_credits_reserve: Number.NaN
    })
    expect(form).toEqual({
      upload_url: 'https://upload.example.com',
      env_name: 'staging',
      skill_id: 'skill_9',
      max_run_minutes: 45,
      auto_confirm_high_cost: true,
      price_drift_tolerance: 0.25,
      auto_confirm_credits_max: 0,
      round_fee_credits: 3,
      min_credits_reserve: 60
    })
    expect(readFlovaCredentialForm(undefined)).toEqual(defaultFlovaCredentialForm())
  })

  it('applyFlovaCredentials overwrites the form keys and keeps everything else', () => {
    const credentials: Record<string, unknown> = {
      api_key: 'k',
      base_url: '',
      client_version: 'v0.0.10',
      skill_id: 'old',
      model_mapping: { 'flova/seedance-2.5': 'flova/seedance-2.5' }
    }
    const form = defaultFlovaCredentialForm()
    form.skill_id = 'new'
    applyFlovaCredentials(credentials, form)
    expect(credentials.api_key).toBe('k')
    expect(credentials.client_version).toBe('v0.0.10')
    expect(credentials.model_mapping).toEqual({ 'flova/seedance-2.5': 'flova/seedance-2.5' })
    expect(credentials.skill_id).toBe('new')
    expect(credentials.min_credits_reserve).toBe(60)
  })

  describe('validateFlovaCredentials', () => {
    it('accepts defaults and empty optional URLs', () => {
      expect(validateFlovaCredentials(buildFlovaCredentials(defaultFlovaCredentialForm()))).toBeNull()
      expect(validateFlovaCredentials({ base_url: '', upload_url: '' })).toBeNull()
      expect(validateFlovaCredentials({})).toBeNull()
    })

    it('rejects invalid URLs', () => {
      expect(validateFlovaCredentials({ base_url: 'not a url' })).toBe('admin.accounts.flova.errors.invalidBaseUrl')
      expect(validateFlovaCredentials({ base_url: 'ftp://x.example' })).toBe('admin.accounts.flova.errors.invalidBaseUrl')
      expect(validateFlovaCredentials({ upload_url: 'upload.example.com' })).toBe('admin.accounts.flova.errors.invalidUploadUrl')
      expect(validateFlovaCredentials({ upload_url: 'https://upload.example.com/v2' })).toBeNull()
    })

    it('rejects non-positive run minutes and tolerance outside [0, 1]', () => {
      expect(validateFlovaCredentials({ max_run_minutes: 0 })).toBe('admin.accounts.flova.errors.invalidMaxRunMinutes')
      expect(validateFlovaCredentials({ max_run_minutes: -5 })).toBe('admin.accounts.flova.errors.invalidMaxRunMinutes')
      expect(validateFlovaCredentials({ max_run_minutes: 1 })).toBeNull()
      expect(validateFlovaCredentials({ price_drift_tolerance: -0.1 })).toBe('admin.accounts.flova.errors.invalidPriceDriftTolerance')
      expect(validateFlovaCredentials({ price_drift_tolerance: 1.01 })).toBe('admin.accounts.flova.errors.invalidPriceDriftTolerance')
      expect(validateFlovaCredentials({ price_drift_tolerance: 0 })).toBeNull()
      expect(validateFlovaCredentials({ price_drift_tolerance: 1 })).toBeNull()
    })

    it('rejects negative credit thresholds', () => {
      expect(validateFlovaCredentials({ auto_confirm_credits_max: -1 })).toBe('admin.accounts.flova.errors.negativeAutoConfirmCreditsMax')
      expect(validateFlovaCredentials({ round_fee_credits: -1 })).toBe('admin.accounts.flova.errors.negativeRoundFeeCredits')
      expect(validateFlovaCredentials({ min_credits_reserve: -1 })).toBe('admin.accounts.flova.errors.negativeMinCreditsReserve')
      expect(validateFlovaCredentials({ auto_confirm_credits_max: 0, round_fee_credits: 0, min_credits_reserve: 0 })).toBeNull()
    })
  })

  it('reads the credits snapshot from account extra', () => {
    expect(readFlovaCreditsSnapshot(undefined)).toBeNull()
    expect(readFlovaCreditsSnapshot({ flova_credits: '12' })).toBeNull()
    expect(readFlovaCreditsSnapshot({ flova_credits: 1234 })).toEqual({ credits: 1234, plan: '' })
    expect(
      readFlovaCreditsSnapshot({ flova_credits: 0, flova_subscription: { lookup_key: 'pro_monthly' } })
    ).toEqual({ credits: 0, plan: 'pro_monthly' })
  })
})
