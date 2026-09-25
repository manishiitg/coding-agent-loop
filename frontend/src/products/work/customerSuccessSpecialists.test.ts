import { describe, expect, it } from 'vitest'
import { crewTemplates, getCrewTemplate, parseCrewTemplateSetupState } from './crewTemplates'

describe('Customer Success Crew templates', () => {
  it('offers three reusable roles with local skills and pending chat setup', () => {
    const success = crewTemplates.filter(item => item.category === 'Customer Success')
    expect(success.map(item => item.id)).toEqual(['customer-onboarding-coordinator', 'product-adoption-analyst', 'customer-health-coordinator'])
    for (const template of success) {
      expect(getCrewTemplate(template.id)).toBe(template)
      expect(template.files[`skills/${template.id}/SKILL.md`]).toContain('## Automation handoff')
      const setup = parseCrewTemplateSetupState(template.files[template.setupPath], template)
      expect(setup?.checks).toHaveLength(9)
      expect(setup?.completed_steps).toEqual([])
      expect(template.files[template.setupGuidePath]).toContain('Source and connection choice')
    }
  })
})
