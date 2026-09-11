import { describe, expect, it } from 'vitest'
import { extractWorkflowGoalSections, extractWorkflowSoulSummary } from './soulSummaryUtils'

describe('workflow soul summary', () => {
  it('extracts the objective paragraph and first concrete numbered criterion', () => {
    const summary = extractWorkflowSoulSummary(`# Example

## Objective

Continuously monitor the complete experience.

Additional objective detail.

## Success Criteria

### Primary signal

A run is successful when ALL checks pass:

1. Voice latency is decomposed by layer with current evidence.
2. Regressions are flagged.

## Constraints

- Never mutate production infrastructure.
`)

    expect(summary).toEqual({
      goal: 'Continuously monitor the complete experience.',
      success: 'Voice latency is decomposed by layer with current evidence.',
      constraints: 'Never mutate production infrastructure.',
    })
  })

  it('supports Goal, Success, and Guardrails headings with bullet criteria', () => {
    const summary = extractWorkflowSoulSummary(`## Goal
Publish useful recommendations.

## Success
- Every recommendation includes reproducible proof.

## Guardrails
- Do not fabricate unavailable evidence.
`)

    expect(summary).toEqual({
      goal: 'Publish useful recommendations.',
      success: 'Every recommendation includes reproducible proof.',
      constraints: 'Do not fabricate unavailable evidence.',
    })
  })
})

describe('outcome goal priorities', () => {
  it('groups goals independently of document order and leads summaries with primary outcomes', () => {
    const md = `## Objective
Keep all agreed commitments.
### Secondary goals
- Make the learner experience ready quickly.
### Primary goals
- Make conversations feel immediate.
- Keep conversations natural.
#### Scope
Across supported learner routes.
## Success Criteria
- Measurement targets are met.
## Constraints
- Preserve privacy.
`
    const sections = extractWorkflowGoalSections(md)
    expect(sections.primaryGoals).toContain('- Make conversations feel immediate.')
    expect(sections.primaryGoals).toContain('#### Scope\nAcross supported learner routes.')
    expect(sections.secondaryGoals).toBe('- Make the learner experience ready quickly.')
    expect(sections.otherGoals).toBe('Keep all agreed commitments.')
    expect(sections.acceptance).toBe('- Measurement targets are met.')
    expect(sections.boundaries).toBe('- Preserve privacy.')
    expect(extractWorkflowSoulSummary(md).goal).toBe('Make conversations feel immediate. Keep conversations natural.')
  })

  it('preserves legacy prose and bullets without assigning priorities', () => {
    for (const goal of ['- Grow the audience.\n- Retain readers.', 'Grow the audience.\n\nRetain readers.']) {
      const sections = extractWorkflowGoalSections(`## Objective\n${goal}`)
      expect(sections.primaryGoals).toBe('')
      expect(sections.secondaryGoals).toBe('')
      expect(sections.otherGoals).toBe(goal)
      expect(sections.goal).toContain('Grow the audience.')
      expect(sections.goal).toContain('Retain readers.')
    }
  })

  it('accepts outcome headings and preserves unknown sections and repeated groups', () => {
    const sections = extractWorkflowGoalSections(`## Goal
### PRIMARY OUTCOMES ###
- Grow the audience.
### Other commitments
- Keep all existing channels supported.
### Primary goal
- Retain readers.
### Secondary outcome
- Improve audience understanding.
## Success
- Evidence is verified.`)
    expect(sections.primaryGoals).toBe('- Grow the audience.\n- Retain readers.')
    expect(sections.secondaryGoals).toBe('- Improve audience understanding.')
    expect(sections.otherGoals).toContain('Keep all existing channels supported.')
    expect(sections.primaryGoals).not.toContain('Other commitments')
  })

  it('does not classify example headings inside code fences as priorities', () => {
    const sections = extractWorkflowGoalSections('## Objective\n### Primary goals\n- Improve clarity.\n```markdown\n### Secondary goals\nExample heading only\n```')
    expect(sections.primaryGoals).toContain('Example heading only')
    expect(sections.secondaryGoals).toBe('')
  })
})
