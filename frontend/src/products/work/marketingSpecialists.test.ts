import { describe, expect, it } from 'vitest'
import { crewTemplates, getCrewTemplate, matchesCrewTemplateSearch, parseCrewTemplateSetupState } from './crewTemplates'

describe('Marketing Crew templates', () => {
  it('offers three distinct jobs with evidence-backed pending setup', () => {
    const marketing = crewTemplates.filter(item => item.category === 'Marketing')
    expect(marketing.map(item => item.id)).toEqual([
      'competitor-intelligence-analyst',
      'campaign-performance-analyst',
      'growth-experiment-planner',
    ])
    for (const template of marketing) {
      expect(getCrewTemplate(template.id)).toBe(template)
      const skill = template.files['skills/' + template.id + '/SKILL.md']
      expect(skill).toContain('## Fictional worked example')
      expect(skill).toContain('## Automation handoff')
      expect(skill).toContain('Installation enables no schedule')
      const setup = parseCrewTemplateSetupState(template.files[template.setupPath], template)
      expect(setup?.checks.map(check => check.id)).toEqual([
        'identity', 'skill', 'scope', 'access', 'policy', 'first_result', 'review', 'delivery', 'recurrence',
      ])
      expect(setup?.completed_steps).toEqual([])
    }
    expect(matchesCrewTemplateSearch(marketing[0], 'competitor positioning')).toBe(true)
    expect(marketing[1].files['skills/campaign-performance-analyst/SKILL.md']).toContain('causal')
    expect(marketing[2].files['skills/growth-experiment-planner/SKILL.md']).toContain('stop rule')
  })
})
