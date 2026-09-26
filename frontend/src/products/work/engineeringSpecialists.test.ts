import { describe, expect, it } from 'vitest'
import { crewTemplates, getCrewTemplate, matchesCrewTemplateSearch, parseCrewTemplateSetupState } from './crewTemplates'

describe('Engineering Crew templates', () => {
  it('offers cross-tool roles with pending, source-verified chat setup', () => {
    const engineering = crewTemplates.filter(item => item.category === 'Engineering')
    expect(engineering.map(item => item.id)).toEqual([
      'incident-investigator',
      'engineering-delivery-coordinator',
      'performance-investigator',
      'cloud-cost-analyst',
    ])
    for (const template of engineering) {
      expect(getCrewTemplate(template.id)).toBe(template)
      expect(template.files[`skills/${template.id}/SKILL.md`]).toContain('## Automation handoff')
      expect(template.files[`skills/${template.id}/SKILL.md`]).toContain('## Follow-through')
      const setup = parseCrewTemplateSetupState(template.files[template.setupPath], template)
      expect(setup?.checks.map(check => check.id)).toEqual([
        'identity', 'skill', 'scope', 'access', 'join_rules', 'first_result', 'review', 'delivery', 'recurrence',
      ])
      expect(setup?.completed_steps).toEqual([])
    }
    expect(matchesCrewTemplateSearch(engineering[1], 'blocked CI')).toBe(true)
    expect(engineering.some(item => /code review|PR Review Assistant/i.test(item.name))).toBe(false)
  })
})
