import { describe, expect, it } from 'vitest'
import { crewTemplates, getCrewTemplate, parseCrewTemplateSetupState } from './crewTemplates'

describe('Website Growth Crew templates', () => {
  it('makes every planned specialist selectable with its own skill and chat checklist', () => {
    const growth = crewTemplates.filter(item => item.category === 'Website Growth')
    expect(growth).toHaveLength(10)
    expect(new Set(growth.map(item => item.id)).size).toBe(growth.length)
    for (const template of growth) {
      expect(getCrewTemplate(template.id)).toBe(template)
      expect(template.files[`skills/${template.id}/SKILL.md`]).toContain(`# ${template.name}`)
      const setup = parseCrewTemplateSetupState(template.files[template.setupPath], template)
      expect(setup?.checks.length).toBeGreaterThanOrEqual(5)
      expect(setup?.checks.length).toBeLessThanOrEqual(10)
      expect(setup?.completed_steps).toEqual([])
      expect(template.files[template.setupGuidePath]).toContain('setup')
    }
  })
})
