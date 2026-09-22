import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('formatted Chat structured-source boundary', () => {
  it('does not reconcile provider-native transcripts in the frontend', () => {
    const source = readFileSync('src/components/ChatArea.tsx', 'utf8')

    expect(source).not.toContain('codingCliTranscriptReconciliation')
    expect(source).not.toContain('reconciledCodingCliCompletionsRef')
  })

  it('shows rapid messages immediately and leaves session ordering to the durable backend dispatcher', () => {
    const source = readFileSync('src/components/ChatArea.tsx', 'utf8')
    const stage = source.indexOf('captured.optimisticUserEventId = optimistic.id')
    const submit = source.indexOf('submitQueryImmediately(query, executionOptions, captured)', stage)

    expect(stage).toBeGreaterThan(-1)
    expect(submit).toBeGreaterThan(stage)
    expect(source).not.toContain('chatSubmissionLane')
  })

  it('hydrates a fresh restore cursor before opening the session SSE stream', () => {
    const source = readFileSync('src/components/ChatArea.tsx', 'utf8')
    const boundary = source.indexOf('tabEventIndices[sid] === undefined')
    const hydration = source.indexOf('hydrateTabEvents(sid', boundary)
    const connection = source.indexOf('connectSessionStream(sid)', hydration)

    expect(boundary).toBeGreaterThan(-1)
    expect(hydration).toBeGreaterThan(boundary)
    expect(connection).toBeGreaterThan(hydration)
    expect(source.slice(hydration, connection)).toContain('.finally(() =>')
  })
})
