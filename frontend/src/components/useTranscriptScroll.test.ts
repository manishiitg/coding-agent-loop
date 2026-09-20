import { describe, expect, it, vi } from 'vitest'
import { prependedIndex, TranscriptScrollController, transcriptReadingState } from './useTranscriptScroll'

describe('transcript scroll ownership', () => {
  it('coalesces updates, cancels pending movement, and resumes explicitly', () => {
    const frames = new Map<number, FrameRequestCallback>()
    let id = 0
    const move = vi.fn()
    const controller = new TranscriptScrollController(true, move, vi.fn(), cb => { frames.set(++id, cb); return id }, frame => { frames.delete(frame) })
    controller.layoutChanged()
    controller.layoutChanged()
    expect(frames.size).toBe(1)
    const stale = frames.values().next().value!
    controller.pause()
    stale(0)
    expect(move).not.toHaveBeenCalled()
    controller.layoutChanged()
    expect(frames.size).toBe(0)
    controller.resume()
    frames.values().next().value!(0)
    expect(move).toHaveBeenCalledOnce()
  })
  it('preserves the virtual index on prepends and merged boundary tool batches', () => {
    expect(prependedIndex(['a', 'b'], ['older', 'a', 'b'], 100)).toBe(99)
    expect(prependedIndex(['old-batch', 'b'], ['older', 'merged-batch', 'b'], 100)).toBe(99)
    expect(prependedIndex(['a', 'b'], ['a', 'b', 'c'], 100)).toBe(100)
    expect(prependedIndex(
      ['old-progress', 'old-reply'],
      ['replacement-progress', 'old-reply', 'new-user', 'new-reply'],
      100,
    )).toBe(100)
  })
  it('keeps reading positions and expanded tools scoped to the chat', () => {
    const first = transcriptReadingState('test-chat-a')
    first.following = false
    first.anchor = { key: 'row-4', offset: 18 }
    first.disclosures.set('tool-4', true)
    const second = transcriptReadingState('test-chat-b')
    expect(second.following).toBe(true)
    expect(second.disclosures.size).toBe(0)
    expect(transcriptReadingState('test-chat-a')).toBe(first)
  })
})
