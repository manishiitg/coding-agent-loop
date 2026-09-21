import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('formatted Chat structured-source boundary', () => {
  it('does not reconcile provider-native transcripts in the frontend', () => {
    const source = readFileSync('src/components/ChatArea.tsx', 'utf8')

    expect(source).not.toContain('codingCliTranscriptReconciliation')
    expect(source).not.toContain('reconciledCodingCliCompletionsRef')
  })

  it('shows rapid retained-chat messages before serialized delivery begins', () => {
    const source = readFileSync('src/components/ChatArea.tsx', 'utf8')
    const stage = source.indexOf('captured.optimisticUserEventId = optimistic.id')
    const enqueue = source.indexOf('return chatSubmissionLane.enqueue', stage)

    expect(stage).toBeGreaterThan(-1)
    expect(enqueue).toBeGreaterThan(stage)
  })
})
