import { describe, expect, it } from 'vitest'
import { crewTemplates, getCrewTemplate, parseCrewTemplateSetupState } from './crewTemplates'

describe('Sales Crew templates', () => {
  it('offers three distinct reusable roles with local skills and chat setup checks', () => {
    const sales = crewTemplates.filter(item => item.category === 'Sales')
    expect(sales.map(item => item.id)).toEqual(['lead-intake-qualifier', 'account-researcher', 'sales-followup-coordinator'])
    for (const template of sales) {
      expect(getCrewTemplate(template.id)).toBe(template)
      expect(template.files[`skills/${template.id}/SKILL.md`]).toContain(`# ${template.name}`)
      expect(template.files[`skills/${template.id}/SKILL.md`]).toContain('## Automation handoff')
      const setup = parseCrewTemplateSetupState(template.files[template.setupPath], template)
      expect(setup?.checks).toHaveLength(9)
      expect(setup?.completed_steps).toEqual([])
      expect(template.files[template.setupGuidePath]).toContain('setup')
    }
  })
})
