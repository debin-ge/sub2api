import { describe, expect, it } from 'vitest'
import {
  isRegistrationEmailSuffixBlocked,
  isRegistrationEmailSuffixDomainValid,
  normalizeRegistrationEmailSuffixDomain,
  normalizeRegistrationEmailSuffixDomains,
  normalizeRegistrationEmailSuffixBlacklist,
  parseRegistrationEmailSuffixBlacklistInput
} from '@/utils/registrationEmailPolicy'

describe('registrationEmailPolicy utils', () => {
  it('normalizeRegistrationEmailSuffixDomain lowercases, strips @, and ignores invalid chars', () => {
    expect(normalizeRegistrationEmailSuffixDomain(' @Exa!mple.COM ')).toBe('example.com')
    expect(normalizeRegistrationEmailSuffixDomain(' *.EDU!.CN ')).toBe('*.edu.cn')
  })

  it('normalizeRegistrationEmailSuffixDomains deduplicates normalized domains', () => {
    expect(
      normalizeRegistrationEmailSuffixDomains([
        '@example.com',
        'Example.com',
        '',
        '-invalid.com',
        'foo..bar.com',
        ' @foo.bar ',
        '@foo.bar',
        '*.EDU.CN',
        '*.edu.cn'
      ])
    ).toEqual(['example.com', 'foo.bar', '*.edu.cn'])
  })

  it('parseRegistrationEmailSuffixBlacklistInput supports separators and deduplicates', () => {
    const input = '\n  @example.com,example.com，@foo.bar\t@FOO.bar *.EDU.CN  '
    expect(parseRegistrationEmailSuffixBlacklistInput(input)).toEqual([
      'example.com',
      'foo.bar',
      '*.edu.cn'
    ])
  })

  it('parseRegistrationEmailSuffixBlacklistInput drops tokens containing invalid chars', () => {
    const input = '@exa!mple.com, @foo.bar, @bad#token.com, @ok-domain.com'
    expect(parseRegistrationEmailSuffixBlacklistInput(input)).toEqual(['foo.bar', 'ok-domain.com'])
  })

  it('parseRegistrationEmailSuffixBlacklistInput drops structurally invalid domains', () => {
    const input = '@-bad.com, @foo..bar.com, @foo.bar, @xn--ok.com, *., *, *.@, *.foo'
    expect(parseRegistrationEmailSuffixBlacklistInput(input)).toEqual(['foo.bar', 'xn--ok.com'])
  })

  it('parseRegistrationEmailSuffixBlacklistInput returns empty list for blank input', () => {
    expect(parseRegistrationEmailSuffixBlacklistInput('   \n \n')).toEqual([])
  })

  it('normalizeRegistrationEmailSuffixBlacklist returns canonical @domain list', () => {
    expect(
      normalizeRegistrationEmailSuffixBlacklist([
        '@Example.com',
        'foo.bar',
        '',
        '-invalid.com',
        ' @foo.bar ',
        '*.EDU.CN'
      ])
    ).toEqual(['@example.com', '@foo.bar', '*.edu.cn'])
  })

  it('isRegistrationEmailSuffixDomainValid matches backend-compatible domain rules', () => {
    expect(isRegistrationEmailSuffixDomainValid('example.com')).toBe(true)
    expect(isRegistrationEmailSuffixDomainValid('foo-bar.example.com')).toBe(true)
    expect(isRegistrationEmailSuffixDomainValid('*.edu.cn')).toBe(true)
    expect(isRegistrationEmailSuffixDomainValid('-bad.com')).toBe(false)
    expect(isRegistrationEmailSuffixDomainValid('foo..bar.com')).toBe(false)
    expect(isRegistrationEmailSuffixDomainValid('localhost')).toBe(false)
    expect(isRegistrationEmailSuffixDomainValid('*.foo')).toBe(false)
    expect(isRegistrationEmailSuffixDomainValid('*')).toBe(false)
    expect(isRegistrationEmailSuffixDomainValid('*.@')).toBe(false)
  })

  it('isRegistrationEmailSuffixBlocked allows any email when blacklist is empty', () => {
    expect(isRegistrationEmailSuffixBlocked('user@example.com', [])).toBe(false)
  })

  it('isRegistrationEmailSuffixBlocked applies exact suffix matching', () => {
    expect(isRegistrationEmailSuffixBlocked('user@example.com', ['@example.com'])).toBe(true)
    expect(isRegistrationEmailSuffixBlocked('user@sub.example.com', ['@example.com'])).toBe(false)
    expect(isRegistrationEmailSuffixBlocked('user@qq.com', ['@qq.com'])).toBe(true)
    expect(isRegistrationEmailSuffixBlocked('user@sub.qq.com', ['@qq.com'])).toBe(false)
  })

  it('isRegistrationEmailSuffixBlocked applies wildcard suffix matching', () => {
    expect(isRegistrationEmailSuffixBlocked('student@cs.edu.cn', ['*.edu.cn'])).toBe(true)
    expect(isRegistrationEmailSuffixBlocked('student@edu.cn', ['*.edu.cn'])).toBe(true)
    expect(isRegistrationEmailSuffixBlocked('student@foo.cn', ['*.edu.cn'])).toBe(false)
  })

  it('isRegistrationEmailSuffixBlocked supports mixed exact and wildcard entries', () => {
    const blacklist = ['@a.com', '*.b.cn']
    expect(isRegistrationEmailSuffixBlocked('user@a.com', blacklist)).toBe(true)
    expect(isRegistrationEmailSuffixBlocked('user@school.b.cn', blacklist)).toBe(true)
    expect(isRegistrationEmailSuffixBlocked('user@b.cn', blacklist)).toBe(true)
    expect(isRegistrationEmailSuffixBlocked('user@c.cn', blacklist)).toBe(false)
  })
})
