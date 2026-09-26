import { describe, expect, it } from 'vitest'
import { crewTemplates, getCrewTemplate, matchesCrewTemplateSearch, parseCrewTemplateSetupState } from './crewTemplates'

describe('GTM Crew templates', () => {
  it('offers strategy and launch roles with pending setup and bounded examples', () => {
    const gtm = crewTemplates.filter(item => item.category === 'GTM')
    expect(gtm.map(item => item.id)).toEqual(['gtm-strategy-analyst', 'launch-coordinator'])
    for (const template of gtm) {
      expect(getCrewTemplate(template.id)).toBe(template)
      const skill = template.files['skills/' + template.id + '/SKILL.md']
      expect(skill).toContain('## Fictional worked example')
      expect(skill).toContain('## Automation handoff')
      const setup = parseCrewTemplateSetupState(template.files[template.setupPath], template)
      expect(setup?.checks.map(check => check.id)).toEqual([
        'identity', 'skill', 'scope', 'access', 'policy', 'first_result', 'review', 'delivery', 'recurrence',
      ])
      expect(setup?.completed_steps).toEqual([])
    }
    expect(matchesCrewTemplateSearch(gtm[0], 'buyer evidence')).toBe(true)
    expect(gtm[1].files['skills/launch-coordinator/SKILL.md']).toContain('A launch asset marked done in a project board is not proof')
  })
})
