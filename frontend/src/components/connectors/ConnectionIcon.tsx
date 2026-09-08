import { Boxes } from 'lucide-react'
import { BRAND_MARKS } from './brandMarks'
import { RASTER_MARKS } from './rasterMarks'

interface ConnectionIconProps {
  /** Catalog `icon` slug, e.g. "notion". */
  icon?: string
  /**
   * Server name, used only for the monogram fallback. Without it a connector
   * that has no published mark falls back to the neutral glyph instead.
   */
  name?: string
  size?: 'xs' | 'sm' | 'md' | 'lg'
  className?: string
}

/** Tailwind classes for the eight monogram tints, picked by name hash. */
const MONOGRAM_TINTS = [
  'bg-blue-500/15 text-blue-600 dark:text-blue-300',
  'bg-emerald-500/15 text-emerald-600 dark:text-emerald-300',
  'bg-violet-500/15 text-violet-600 dark:text-violet-300',
  'bg-amber-500/15 text-amber-600 dark:text-amber-300',
  'bg-rose-500/15 text-rose-600 dark:text-rose-300',
  'bg-cyan-500/15 text-cyan-600 dark:text-cyan-300',
  'bg-indigo-500/15 text-indigo-600 dark:text-indigo-300',
  'bg-teal-500/15 text-teal-600 dark:text-teal-300',
]

/**
 * Stable per-name tint. A plain sum of char codes is enough here — the only
 * requirement is that a given connector keeps the same colour between renders,
 * not that the distribution is uniform.
 */
const tintFor = (name: string) => {
  let sum = 0
  for (let i = 0; i < name.length; i++) sum += name.charCodeAt(i)
  return MONOGRAM_TINTS[sum % MONOGRAM_TINTS.length]
}

/**
 * A connector's mark, identified by its catalog `icon` slug alone.
 *
 * Brands whose logo genuinely carries several colours are drawn as published.
 * Brands that are single-colour by design — Notion, GitHub, Vercel — are drawn
 * in `currentColor`, so they take the tile's neutral foreground and stay legible
 * in both themes without needing a colour of their own.
 *
 * Brands with no vector logo anywhere fall back to `rasterMarks` — the bitmap
 * icon their own site serves. It cannot take `currentColor`, so those are
 * pre-composited to clear the contrast bar on both grounds.
 *
 * Not every service publishes a mark at all, and a grid of identical fallback
 * glyphs is harder to scan than no logo. Those fall back to a tinted monogram of
 * the connector's initial when `name` is supplied, and to a neutral glyph when
 * it is not.
 */
export default function ConnectionIcon({
  icon,
  name,
  size = 'md',
  className = '',
}: ConnectionIconProps) {
  const mark = icon ? BRAND_MARKS[icon] : undefined
  const raster = !mark && icon ? RASTER_MARKS[icon] : undefined
  const box = size === 'xs' ? 'h-5 w-5' : size === 'sm' ? 'h-9 w-9' : size === 'lg' ? 'h-12 w-12' : 'h-11 w-11'
  // The mark fills more of its tile than it used to — at the old ratio the
  // logos read as small dots inside a large rounded square.
  const glyph = size === 'xs' ? 'h-3 w-3' : size === 'sm' ? 'h-5 w-5' : size === 'lg' ? 'h-7 w-7' : 'h-6 w-6'
  const letter = size === 'xs' ? 'text-[9px]' : size === 'sm' ? 'text-sm' : size === 'lg' ? 'text-lg' : 'text-base'

  const initial = name?.trim().replace(/[^A-Za-z0-9]/g, '').charAt(0).toUpperCase()
  const useMonogram = !mark && !raster && !!initial

  return (
    <span
      // A neutral tile in both themes; the mark itself carries the recognition.
      // Monogram tiles carry their own tint so the grid stays scannable.
      className={`flex ${box} shrink-0 items-center justify-center rounded-lg ${
        useMonogram
          ? tintFor(name as string)
          : 'bg-gray-100 text-gray-500 dark:bg-slate-800 dark:text-gray-400'
      } ${className}`}
    >
      {mark ? (
        <svg
          viewBox={mark.viewBox}
          className={glyph}
          // Non-square marks keep their proportions rather than being stretched
          // to fill the tile.
          preserveAspectRatio="xMidYMid meet"
          fill={mark.mono ? 'currentColor' : undefined}
          aria-hidden="true"
          focusable="false"
          dangerouslySetInnerHTML={{ __html: mark.markup }}
        />
      ) : raster ? (
        <img
          src={raster}
          className={`${glyph} object-contain`}
          alt=""
          aria-hidden="true"
        />
      ) : useMonogram ? (
        <span className={`${letter} font-semibold leading-none`} aria-hidden="true">
          {initial}
        </span>
      ) : (
        <Boxes className={glyph} aria-hidden="true" />
      )}
    </span>
  )
}
