import { describe, expect, it } from 'vitest'
import { crewTemplates, getCrewTemplate, matchesCrewTemplateSearch, parseCrewTemplateSetupState } from './crewTemplates'

describe('Customer Support Crew templates', () => {
  it('offers case and feedback roles with pending source-verified setup', () => {
    const support = crewTemplates.filter(item => item.category === 'Customer Support')
    expect(support.map(item => item.id)).toEqual([
      'support-triage-assistant',
      'support-reply-drafter',
      'escalation-coordinator',
      'feedback-review-analyst',
    ])
    for (const template of support) {
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
    expect(matchesCrewTemplateSearch(support[1], 'approved help content')).toBe(true)
    expect(support[1].files['skills/support-reply-drafter/SKILL.md']).toContain('send_state=not_sent')
    expect(support[2].files['skills/escalation-coordinator/SKILL.md']).toContain('a posted message alone is not an accepted owner assignment')
  })
})
