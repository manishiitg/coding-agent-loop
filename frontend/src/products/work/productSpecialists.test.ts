import { describe, expect, it } from 'vitest'
import { crewTemplates, getCrewTemplate, matchesCrewTemplateSearch, parseCrewTemplateSetupState } from './crewTemplates'

describe('Product Feedback Coordinator', () => {
  it('installs a scoped Product role with independent pending chat setup', () => {
    const template = getCrewTemplate('product-feedback-coordinator')
    expect(crewTemplates.filter(item => item.category === 'Product')).toEqual([template])
    expect(template.version).toBe(2)
    const setup = parseCrewTemplateSetupState(template.files[template.setupPath], template)
    expect(setup?.checks.map(check => check.id)).toEqual([
      'identity', 'skill', 'scope', 'access', 'policy', 'first_result', 'review', 'delivery', 'recurrence',
    ])
    expect(setup?.completed_steps).toEqual([])
    expect(matchesCrewTemplateSearch(template, 'feedback product decision')).toBe(true)
    const skill = template.files['skills/product-feedback-coordinator/SKILL.md']
    expect(skill).toContain('## Fictional worked example')
    expect(skill).toContain('## Released feature decision')
    expect(skill).toContain('Inadequate:')
    expect(skill).toContain('Installation enables no schedule')
  })
})
