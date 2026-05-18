/**
 * Centralized platform color definitions.
 *
 * All components that need platform-specific styling should import from here
 * instead of defining their own color mappings.
 */

export type Platform = 'anthropic' | 'openai' | 'antigravity' | 'gemini' | 'minimax' | 'glm' | 'kimi' | 'deepseek' | 'windsurf'

// ── Badge (bg + text + border, for inline badges with border) ───────
const BADGE: Record<Platform, string> = {
  anthropic: 'bg-orange-500/10 text-orange-600 border-orange-500/30 dark:text-orange-400',
  openai: 'bg-green-500/10 text-green-600 border-green-500/30 dark:text-green-400',
  antigravity: 'bg-purple-500/10 text-purple-600 border-purple-500/30 dark:text-purple-400',
  gemini: 'bg-blue-500/10 text-blue-600 border-blue-500/30 dark:text-blue-400',
  minimax: 'bg-cyan-500/10 text-cyan-700 border-cyan-500/30 dark:text-cyan-300',
  glm: 'bg-rose-500/10 text-rose-600 border-rose-500/30 dark:text-rose-400',
  kimi: 'bg-lime-500/10 text-lime-700 border-lime-500/30 dark:text-lime-300',
  deepseek: 'bg-indigo-500/10 text-indigo-700 border-indigo-500/30 dark:text-indigo-300',
  windsurf: 'bg-teal-500/10 text-teal-700 border-teal-500/30 dark:text-teal-300',
}
const BADGE_DEFAULT = 'bg-slate-500/10 text-slate-600 border-slate-500/30 dark:text-slate-400'

// ── Light badge (softer bg, no border) ──────────────────────────────
const BADGE_LIGHT: Record<Platform, string> = {
  anthropic: 'bg-orange-500/10 text-orange-600 dark:bg-orange-500/10 dark:text-orange-300',
  openai: 'bg-green-500/10 text-green-600 dark:bg-green-500/10 dark:text-green-300',
  antigravity: 'bg-purple-500/10 text-purple-600 dark:bg-purple-500/10 dark:text-purple-300',
  gemini: 'bg-blue-500/10 text-blue-600 dark:bg-blue-500/10 dark:text-blue-300',
  minimax: 'bg-cyan-500/10 text-cyan-700 dark:bg-cyan-500/10 dark:text-cyan-300',
  glm: 'bg-rose-500/10 text-rose-600 dark:bg-rose-500/10 dark:text-rose-300',
  kimi: 'bg-lime-500/10 text-lime-700 dark:bg-lime-500/10 dark:text-lime-300',
  deepseek: 'bg-indigo-500/10 text-indigo-700 dark:bg-indigo-500/10 dark:text-indigo-300',
  windsurf: 'bg-teal-500/10 text-teal-700 dark:bg-teal-500/10 dark:text-teal-300',
}

// ── Border ──────────────────────────────────────────────────────────
const BORDER: Record<Platform, string> = {
  anthropic: 'border-orange-500/20 dark:border-orange-500/20',
  openai: 'border-green-500/20 dark:border-green-500/20',
  antigravity: 'border-purple-500/20 dark:border-purple-500/20',
  gemini: 'border-blue-500/20 dark:border-blue-500/20',
  minimax: 'border-cyan-500/20 dark:border-cyan-500/20',
  glm: 'border-rose-500/20 dark:border-rose-500/20',
  kimi: 'border-lime-500/20 dark:border-lime-500/20',
  deepseek: 'border-indigo-500/20 dark:border-indigo-500/20',
  windsurf: 'border-teal-500/20 dark:border-teal-500/20',
}
const BORDER_DEFAULT = 'border-gray-200 dark:border-dark-700'

// ── Accent bar (gradient) ───────────────────────────────────────────
const ACCENT_BAR: Record<Platform, string> = {
  anthropic: 'bg-gradient-to-r from-orange-400 to-orange-500',
  openai: 'bg-gradient-to-r from-emerald-400 to-emerald-500',
  antigravity: 'bg-gradient-to-r from-purple-400 to-purple-500',
  gemini: 'bg-gradient-to-r from-blue-400 to-blue-500',
  minimax: 'bg-gradient-to-r from-cyan-400 to-cyan-500',
  glm: 'bg-gradient-to-r from-rose-400 to-rose-500',
  kimi: 'bg-gradient-to-r from-lime-400 to-lime-500',
  deepseek: 'bg-gradient-to-r from-indigo-400 to-indigo-500',
  windsurf: 'bg-gradient-to-r from-teal-400 to-teal-500',
}
const ACCENT_BAR_DEFAULT = 'bg-gradient-to-r from-primary-400 to-primary-500'

// ── Text (price, icon) ─────────────────────────────────────────────
const TEXT: Record<Platform, string> = {
  anthropic: 'text-orange-600 dark:text-orange-400',
  openai: 'text-emerald-600 dark:text-emerald-400',
  antigravity: 'text-purple-600 dark:text-purple-400',
  gemini: 'text-blue-600 dark:text-blue-400',
  minimax: 'text-cyan-700 dark:text-cyan-300',
  glm: 'text-rose-600 dark:text-rose-400',
  kimi: 'text-lime-700 dark:text-lime-300',
  deepseek: 'text-indigo-700 dark:text-indigo-300',
  windsurf: 'text-teal-700 dark:text-teal-300',
}
const TEXT_DEFAULT = 'text-primary-600 dark:text-primary-400'

// ── Icon (check mark etc.) ──────────────────────────────────────────
const ICON: Record<Platform, string> = {
  anthropic: 'text-orange-500 dark:text-orange-400',
  openai: 'text-emerald-500 dark:text-emerald-400',
  antigravity: 'text-purple-500 dark:text-purple-400',
  gemini: 'text-blue-500 dark:text-blue-400',
  minimax: 'text-cyan-500 dark:text-cyan-300',
  glm: 'text-rose-500 dark:text-rose-400',
  kimi: 'text-lime-500 dark:text-lime-300',
  deepseek: 'text-indigo-500 dark:text-indigo-300',
  windsurf: 'text-teal-500 dark:text-teal-300',
}
const ICON_DEFAULT = 'text-primary-500 dark:text-primary-400'

// ── Button (solid bg) ───────────────────────────────────────────────
const BUTTON: Record<Platform, string> = {
  anthropic: 'bg-orange-500 text-white hover:bg-orange-600 active:bg-orange-700 dark:bg-orange-500/80 dark:hover:bg-orange-500',
  openai: 'bg-green-600 text-white hover:bg-green-700 active:bg-green-800 dark:bg-green-600/80 dark:hover:bg-green-600',
  antigravity: 'bg-purple-500 text-white hover:bg-purple-600 active:bg-purple-700 dark:bg-purple-500/80 dark:hover:bg-purple-500',
  gemini: 'bg-blue-500 text-white hover:bg-blue-600 active:bg-blue-700 dark:bg-blue-500/80 dark:hover:bg-blue-500',
  minimax: 'bg-cyan-600 text-white hover:bg-cyan-700 active:bg-cyan-800 dark:bg-cyan-600/80 dark:hover:bg-cyan-600',
  glm: 'bg-rose-600 text-white hover:bg-rose-700 active:bg-rose-800 dark:bg-rose-600/80 dark:hover:bg-rose-600',
  kimi: 'bg-lime-600 text-white hover:bg-lime-700 active:bg-lime-800 dark:bg-lime-600/80 dark:hover:bg-lime-600',
  deepseek: 'bg-indigo-600 text-white hover:bg-indigo-700 active:bg-indigo-800 dark:bg-indigo-600/80 dark:hover:bg-indigo-600',
  windsurf: 'bg-teal-600 text-white hover:bg-teal-700 active:bg-teal-800 dark:bg-teal-600/80 dark:hover:bg-teal-600',
}
const BUTTON_DEFAULT = 'bg-primary-500 text-white hover:bg-primary-600 dark:bg-primary-600 dark:hover:bg-primary-500'

// ── Discount badge ──────────────────────────────────────────────────
const DISCOUNT: Record<Platform, string> = {
  anthropic: 'bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300',
  openai: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300',
  antigravity: 'bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300',
  gemini: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300',
  minimax: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/40 dark:text-cyan-300',
  glm: 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300',
  kimi: 'bg-lime-100 text-lime-700 dark:bg-lime-900/40 dark:text-lime-300',
  deepseek: 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-300',
  windsurf: 'bg-teal-100 text-teal-700 dark:bg-teal-900/40 dark:text-teal-300',
}
const DISCOUNT_DEFAULT = 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'

// ── Header gradient (subscription confirm) ─────────────────────────
const GRADIENT: Record<Platform, string> = {
  anthropic: 'from-orange-500 to-orange-600',
  openai: 'from-emerald-500 to-emerald-600',
  antigravity: 'from-purple-500 to-purple-600',
  gemini: 'from-blue-500 to-blue-600',
  minimax: 'from-cyan-500 to-cyan-600',
  glm: 'from-rose-500 to-rose-600',
  kimi: 'from-lime-500 to-lime-600',
  deepseek: 'from-indigo-500 to-indigo-600',
  windsurf: 'from-teal-500 to-teal-600',
}
const GRADIENT_DEFAULT = 'from-primary-500 to-primary-600'

// ── Header text (light text on gradient bg) ────────────────────────
const GRADIENT_TEXT: Record<Platform, string> = {
  anthropic: 'text-orange-100',
  openai: 'text-emerald-100',
  antigravity: 'text-purple-100',
  gemini: 'text-blue-100',
  minimax: 'text-cyan-100',
  glm: 'text-rose-100',
  kimi: 'text-lime-100',
  deepseek: 'text-indigo-100',
  windsurf: 'text-teal-100',
}
const GRADIENT_TEXT_DEFAULT = 'text-primary-100'

const GRADIENT_SUBTEXT: Record<Platform, string> = {
  anthropic: 'text-orange-200',
  openai: 'text-emerald-200',
  antigravity: 'text-purple-200',
  gemini: 'text-blue-200',
  minimax: 'text-cyan-200',
  glm: 'text-rose-200',
  kimi: 'text-lime-200',
  deepseek: 'text-indigo-200',
  windsurf: 'text-teal-200',
}
const GRADIENT_SUBTEXT_DEFAULT = 'text-primary-200'

// ── Public API ──────────────────────────────────────────────────────

function isPlatform(p: string): p is Platform {
  return p === 'anthropic' || p === 'openai' || p === 'antigravity' || p === 'gemini' || p === 'minimax' || p === 'glm' || p === 'kimi' || p === 'deepseek' || p === 'windsurf'
}

export function platformBadgeClass(p: string): string {
  return isPlatform(p) ? BADGE[p] : BADGE_DEFAULT
}

export function platformBadgeLightClass(p: string): string {
  return isPlatform(p) ? BADGE_LIGHT[p] : BADGE_DEFAULT
}

export function platformBorderClass(p: string): string {
  return isPlatform(p) ? BORDER[p] : BORDER_DEFAULT
}

export function platformAccentBarClass(p: string): string {
  return isPlatform(p) ? ACCENT_BAR[p] : ACCENT_BAR_DEFAULT
}

export function platformTextClass(p: string): string {
  return isPlatform(p) ? TEXT[p] : TEXT_DEFAULT
}

export function platformIconClass(p: string): string {
  return isPlatform(p) ? ICON[p] : ICON_DEFAULT
}

export function platformButtonClass(p: string): string {
  return isPlatform(p) ? BUTTON[p] : BUTTON_DEFAULT
}

export function platformDiscountClass(p: string): string {
  return isPlatform(p) ? DISCOUNT[p] : DISCOUNT_DEFAULT
}

export function platformGradientClass(p: string): string {
  return isPlatform(p) ? GRADIENT[p] : GRADIENT_DEFAULT
}

export function platformGradientTextClass(p: string): string {
  return isPlatform(p) ? GRADIENT_TEXT[p] : GRADIENT_TEXT_DEFAULT
}

export function platformGradientSubtextClass(p: string): string {
  return isPlatform(p) ? GRADIENT_SUBTEXT[p] : GRADIENT_SUBTEXT_DEFAULT
}

export function platformLabel(p: string): string {
  switch (p) {
    case 'anthropic': return 'Anthropic'
    case 'openai': return 'OpenAI'
    case 'antigravity': return 'Antigravity'
    case 'gemini': return 'Gemini'
    case 'minimax': return 'MiniMax'
    case 'glm': return 'GLM'
    case 'kimi': return 'Kimi'
    case 'deepseek': return 'DeepSeek'
    case 'windsurf': return 'Windsurf'
    default: return p || 'API'
  }
}
