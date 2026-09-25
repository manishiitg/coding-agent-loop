// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

type Status = 'connecting' | 'live' | 'offline'
const feed = vi.hoisted(() => ({
  status: 'live' as Status,
  statusListeners: new Set<(s: Status) => void>(),
  onChange: [] as (() => void)[],
}))
vi.mock('../services/liveFeed', () => ({
  liveFeed: {
    getStatus: () => feed.status,
    onStatus: (cb: (s: Status) => void) => { feed.statusListeners.add(cb); return () => feed.statusListeners.delete(cb) },
    subscribe: (_kinds: string[], _workflow: string | null, onChange: () => void) => {
      feed.onChange.push(onChange)
      return () => { feed.onChange = feed.onChange.filter(fn => fn !== onChange) }
    },
  },
}))
import { useLiveRefetch } from './useLiveRefetch'

let hidden = false
beforeEach(() => {
  vi.useFakeTimers()
  hidden = false
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
  feed.status = 'live'
  feed.onChange = []
})
afterEach(() => vi.useRealTimers())

function mount(refetch: () => void, minIntervalMs = 0) {
  function Probe() {
    useLiveRefetch(refetch, { kinds: ['report'], workflow: 'Workflow/t', fallbackMs: 1_000, safetyMs: 0, minIntervalMs })
    return null
  }
  const root = createRoot(document.createElement('div'))
  act(() => root.render(<Probe />))
  return root
}

it('refetches on a notice instead of a timer while the feed is live', () => {
  const refetch = vi.fn()
  const root = mount(refetch)
  act(() => { vi.advanceTimersByTime(10_000) })
  expect(refetch).not.toHaveBeenCalled()
  act(() => feed.onChange.forEach(fn => fn()))
  expect(refetch).toHaveBeenCalledTimes(1)
  act(() => root.unmount())
})

it('defers a notice seen while hidden until the tab is visible again', () => {
  const refetch = vi.fn()
  const root = mount(refetch)
  hidden = true
  act(() => feed.onChange.forEach(fn => fn()))
  expect(refetch).not.toHaveBeenCalled()
  hidden = false
  act(() => { document.dispatchEvent(new Event('visibilitychange')) })
  expect(refetch).toHaveBeenCalledTimes(1)
  act(() => root.unmount())
})

it('coalesces a burst to one refetch now and one trailing refetch', () => {
  const refetch = vi.fn()
  const root = mount(refetch, 10_000)
  act(() => { for (let i = 0; i < 5; i++) feed.onChange.forEach(fn => fn()) })
  expect(refetch).toHaveBeenCalledTimes(1)
  act(() => { vi.advanceTimersByTime(10_000) })
  expect(refetch).toHaveBeenCalledTimes(2)
  act(() => root.unmount())
})

it('falls back to the old poll rate while the feed is offline', () => {
  feed.status = 'offline'
  const refetch = vi.fn()
  const root = mount(refetch)
  act(() => { vi.advanceTimersByTime(3_000) })
  expect(refetch).toHaveBeenCalledTimes(3)
  act(() => root.unmount())
})
