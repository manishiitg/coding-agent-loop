// Semantic color tokens for report rendering. Centralizes the Tailwind class
// strings that were previously inlined throughout ReportViewer so the report
// palette can be swapped (per-report theme, brand override, etc.) without
// hunting through render code.
//
// All values are literal Tailwind class strings — Tailwind's content scanner
// must see them as literals to keep them in the production CSS, so do not
// build class names by concatenation here.

export type TrendDirection = 'positive' | 'negative' | 'neutral'

export const trendTones: Record<TrendDirection, string> = {
  positive: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 ring-1 ring-emerald-500/30',
  negative: 'bg-red-500/15 text-red-700 dark:text-red-300 ring-1 ring-red-500/30',
  neutral: 'bg-muted text-muted-foreground ring-1 ring-border/50',
}

export const trendArrow: Record<TrendDirection, string> = {
  positive: '▲',
  negative: '▼',
  neutral: '·',
}

export function trendDirection(value: number | undefined | null): TrendDirection {
  if (value == null || !Number.isFinite(value)) return 'neutral'
  if (value > 0) return 'positive'
  if (value < 0) return 'negative'
  return 'neutral'
}

export type AlertSeverity = 'info' | 'warning' | 'error' | 'success'

export const severityTones: Record<AlertSeverity, string> = {
  info: 'border-blue-500/30 bg-blue-500/5 text-foreground',
  warning: 'border-amber-500/40 bg-amber-500/10 text-foreground',
  error: 'border-red-500/40 bg-red-500/10 text-foreground',
  success: 'border-emerald-500/40 bg-emerald-500/10 text-foreground',
}

export const severityIcons: Record<AlertSeverity, string> = {
  info: 'ℹ',
  warning: '⚠',
  error: '✕',
  success: '✓',
}

export type ScoreTier = 'excellent' | 'strong' | 'watch' | 'poor'

export interface ScoreTone {
  pillClassName: string
  accentClassName: string
  barClassName: string
  label: string
}

export const scoreTones: Record<ScoreTier, ScoreTone> = {
  excellent: {
    pillClassName: 'border-emerald-500/30 bg-emerald-500/14 text-emerald-300',
    accentClassName: 'from-emerald-500/18 via-emerald-500/8 to-transparent',
    barClassName: 'bg-emerald-400',
    label: 'Excellent',
  },
  strong: {
    pillClassName: 'border-sky-500/30 bg-sky-500/14 text-sky-300',
    accentClassName: 'from-sky-500/18 via-sky-500/8 to-transparent',
    barClassName: 'bg-sky-400',
    label: 'Strong',
  },
  watch: {
    pillClassName: 'border-amber-500/30 bg-amber-500/14 text-amber-300',
    accentClassName: 'from-amber-500/18 via-amber-500/8 to-transparent',
    barClassName: 'bg-amber-400',
    label: 'Watch',
  },
  poor: {
    pillClassName: 'border-red-500/30 bg-red-500/14 text-red-300',
    accentClassName: 'from-red-500/18 via-red-500/8 to-transparent',
    barClassName: 'bg-red-400',
    label: 'Needs work',
  },
}

export function scoreTier(scorePercentage: number): ScoreTier {
  if (scorePercentage >= 95) return 'excellent'
  if (scorePercentage >= 85) return 'strong'
  if (scorePercentage >= 70) return 'watch'
  return 'poor'
}

export function evalScoreTone(scorePercentage: number): ScoreTone {
  return scoreTones[scoreTier(scorePercentage)]
}
