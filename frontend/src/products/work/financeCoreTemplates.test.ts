import { describe, expect, it } from 'vitest'
import { getCrewTemplate, parseCrewTemplateSetupState } from './crewTemplates'

describe('core Finance Crew templates', () => {
  it.each([
    ['billing-operations-coordinator', 'case_mapping', '6,500 minor units remaining'],
    ['revenue-close-analyst', 'reconciliation', 'USD 50.00'],
    ['spend-payables-coordinator', 'duplicate_check', 'approval_state **pending**'],
  ] as const)('keeps %s as a pending, evidence-led install', (id, probeId, exampleResult) => {
    const template = getCrewTemplate(id)
    const skill = template.files[`skills/${id}/SKILL.md`]
    expect(skill).toContain('## Fictional worked example')
    expect(skill).toContain(exampleResult)
    expect(skill).toContain('## Inadequate output to reject')
    expect(skill).toContain('does not complete setup')
    const setup = parseCrewTemplateSetupState(template.files[template.setupPath], template)
    expect(setup?.checks).toHaveLength(9)
    expect(setup?.completed_steps).toEqual([])
    expect((setup?.checks.find(check => check.id === probeId)?.instructions.length ?? 0)).toBeGreaterThan(250)
    expect(template.files[template.setupGuidePath]).toContain('## Source probe and example')
  })
})
