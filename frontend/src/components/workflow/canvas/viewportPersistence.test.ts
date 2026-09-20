// @vitest-environment happy-dom
import { beforeEach, expect, it, vi } from 'vitest'
import { readSavedViewport, viewportStorageKey, writeSavedViewport } from './viewportPersistence'

beforeEach(() => {
  const values = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    get length() { return values.size },
    clear: () => values.clear(),
    getItem: (key: string) => values.get(key) ?? null,
    key: (index: number) => [...values.keys()][index] ?? null,
    removeItem: (key: string) => { values.delete(key) },
    setItem: (key: string, value: string) => { values.set(key, String(value)) },
  } as Storage)
})

it('keys viewports per workspace', () => {
  expect(viewportStorageKey('/ws/trading')).toBe('workflow-viewport-/ws/trading')
  expect(viewportStorageKey(null)).toBe('workflow-viewport-default')
  expect(viewportStorageKey(undefined)).toBe('workflow-viewport-default')
})

it('round-trips a saved viewport', () => {
  writeSavedViewport('k', { x: -120, y: 340, zoom: 0.65 })
  expect(readSavedViewport('k', 0.08, 2)).toEqual({ x: -120, y: 340, zoom: 0.65 })
})

it('returns null when nothing was saved', () => {
  expect(readSavedViewport('missing', 0.08, 2)).toBeNull()
})

it('rejects corrupt or partial data', () => {
  localStorage.setItem('bad-json', '{not json')
  expect(readSavedViewport('bad-json', 0.08, 2)).toBeNull()
  localStorage.setItem('partial', JSON.stringify({ x: 1, y: 2 }))
  expect(readSavedViewport('partial', 0.08, 2)).toBeNull()
  localStorage.setItem('non-finite', JSON.stringify({ x: NaN, y: 0, zoom: 1 }))
  expect(readSavedViewport('non-finite', 0.08, 2)).toBeNull()
})

it('rejects out-of-range zoom so a stale save cannot break the view', () => {
  localStorage.setItem('zoomed-out', JSON.stringify({ x: 0, y: 0, zoom: 0.01 }))
  expect(readSavedViewport('zoomed-out', 0.08, 2)).toBeNull()
  localStorage.setItem('zoomed-in', JSON.stringify({ x: 0, y: 0, zoom: 5 }))
  expect(readSavedViewport('zoomed-in', 0.08, 2)).toBeNull()
})
