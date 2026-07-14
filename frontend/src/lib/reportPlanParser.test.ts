import { describe, expect, it } from 'vitest'
import { parseReportPlan } from './reportPlanParser'

describe('parseReportPlan interaction widgets', () => {
  it('parses a configured interaction without a file source', () => {
    const plan = parseReportPlan(JSON.stringify({
      version: 1,
      sections: [{
        heading: 'Review',
        entries: [{
          kind: 'single',
          widget: {
            id: 'linkedin-draft-review',
            kind: 'interaction',
            title: 'Draft review',
            question: 'What should happen to this draft?',
            responseKind: 'choice-with-text',
            options: [
              { id: 'approve', title: 'Approve' },
              { id: 'request_changes', title: 'Request changes', description: 'Revise it on the next run.' },
            ],
            instanceKey: 'draft-123-v1',
            subjectId: 'draft-123',
            subjectVersion: '1',
            subjectHash: 'sha256:abc123',
          },
        }],
      }],
    }))

    expect(plan.sections).toHaveLength(1)
    const entry = plan.sections[0].entries[0]
    expect(entry.kind).toBe('single')
    if (entry.kind !== 'single') throw new Error('expected single widget')
    expect(entry.widget.kind).toBe('interaction')
    expect(entry.widget.id).toBe('linkedin-draft-review')
    expect(entry.widget.source).toBeUndefined()
    expect(entry.widget.responseKind).toBe('choice-with-text')
    expect(entry.widget.options?.map(option => option.id)).toEqual(['approve', 'request_changes'])
    expect(entry.widget.instanceKey).toBe('draft-123-v1')
    expect(entry.widget.subjectHash).toBe('sha256:abc123')
  })

  it('drops an invalid choice interaction with no options', () => {
    const plan = parseReportPlan(JSON.stringify({
      sections: [{
        heading: 'Review',
        entries: [{
          kind: 'single',
          widget: {
            id: 'invalid-review',
            kind: 'interaction',
            question: 'Choose something',
            responseKind: 'choice',
          },
        }],
      }],
    }))

    expect(plan.sections[0].entries).toEqual([])
  })
})
