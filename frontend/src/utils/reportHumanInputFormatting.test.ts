import { describe, expect, it } from 'vitest'
import { parseReportHumanInputContext } from './reportHumanInputFormatting'

describe('report human input context formatting', () => {
  it('turns compact proposal text into readable sections and a numbered list', () => {
    const sections = parseReportHumanInputContext(
      'Proposal: Align the objective. Exact intended edits if approved: (1) Update the objective. (2) Update success criteria. Rationale: The workflow already runs hourly. Expected impact: Less drift. Risk: Wording may weaken a constraint.',
    )

    expect(sections.map(section => section.label)).toEqual([
      'Proposal',
      'Intended edits',
      'Rationale',
      'Expected impact',
      'Risk',
    ])
    expect(sections[1].items).toEqual(['Update the objective.', 'Update success criteria.'])
  })

  it('keeps unstructured context as escaped plain text', () => {
    expect(parseReportHumanInputContext('A short explanation.')).toEqual([
      { label: '', body: 'A short explanation.', items: [] },
    ])
  })
})
