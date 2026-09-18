import React from 'react'

const BADGE_TONES = [
  'bg-blue-100 text-blue-700 dark:bg-blue-950/60 dark:text-blue-300',
  'bg-violet-100 text-violet-700 dark:bg-violet-950/60 dark:text-violet-300',
  'bg-emerald-100 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300',
  'bg-amber-100 text-amber-700 dark:bg-amber-950/60 dark:text-amber-300',
  'bg-rose-100 text-rose-700 dark:bg-rose-950/60 dark:text-rose-300',
] as const

function entityInitial(label?: string): string {
  const firstWord = (label || '').trim().split(/\s+/)[0] || 'A'
  return Array.from(firstWord)[0]?.toLocaleUpperCase() || 'A'
}

function toneForEntity(label?: string): string {
  const value = label || 'Agent'
  let hash = 0
  for (const char of value) hash = ((hash * 31) + (char.codePointAt(0) || 0)) >>> 0
  return BADGE_TONES[hash % BADGE_TONES.length]
}

export type EntityIdentityIconProps = {
  icon?: string | null
  label?: string
  className?: string
}

/** Shared compact identity for workflows and product projects. */
export function EntityIdentityIcon({ icon, label, className = '' }: EntityIdentityIconProps) {
  const content = icon?.trim() || entityInitial(label)
  return (
    <span
      className={`inline-flex h-5 w-5 shrink-0 items-center justify-center overflow-hidden rounded text-[11px] font-semibold leading-none ${toneForEntity(label)} ${className}`}
      aria-hidden="true"
    >
      {content}
    </span>
  )
}
