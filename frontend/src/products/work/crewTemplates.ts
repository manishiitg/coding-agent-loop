import financeAnalystSkill from './templates/finance-analyst/SKILL.md?raw'
import financeAnalystSetup from './templates/finance-analyst/TEMPLATE_SETUP.md?raw'
import financeAnalystSetupState from './templates/finance-analyst/TEMPLATE_SETUP.json?raw'
import taxExportSkill from './templates/tax-export/SKILL.md?raw'
import taxExportSetup from './templates/tax-export/SETUP.md?raw'
import taxExportSetupState from './templates/tax-export/TEMPLATE_SETUP.json?raw'
import websiteGrowthSkill from './templates/website-growth-starter/SKILL.md?raw'
import websiteGrowthSetup from './templates/website-growth-starter/SETUP.md?raw'
import websiteGrowthSetupState from './templates/website-growth-starter/TEMPLATE_SETUP.json?raw'
import { websiteGrowthSpecialists, type WebsiteGrowthSpecialistId } from './websiteGrowthSpecialists'

export type CrewTemplateId = 'finance-analyst' | 'tax-export' | 'website-growth-starter' | WebsiteGrowthSpecialistId

export type CrewTemplateSetupCheck = {
  id: string
  title: string
  instructions: string
  optional?: boolean
}

export type CrewTemplateSetupState = {
  schema_version: 1
  template_id: string
  template_version: number
  checks: CrewTemplateSetupCheck[]
  completed_steps: string[]
}

export type CrewTemplate = {
  id: CrewTemplateId
  version: number
  category: string
  name: string
  icon: string
  role: string
  purpose: string
  firstResult: string
  minimumInput: string
  optionalConnections: string
  exampleRequests: readonly string[]
  selectedSkills: readonly [string, ...string[]]
  setupPath: string
  setupGuidePath: string
  requiredFiles: readonly [string, ...string[]]
  files: Readonly<Record<string, string>>
}

export const crewTemplates: readonly CrewTemplate[] = [{
  id: 'finance-analyst',
  version: 1,
  category: 'Finance',
  name: 'Finance Analyst',
  icon: '📊',
  role: 'Finance analyst for this business',
  purpose: 'Analyze authorized finance records, explain revenue and expense changes, and prepare sourced finance briefs with transparent calculations.',
  firstResult: 'A sourced finance brief with calculations and questions to verify.',
  minimumInput: 'Upload a statement or finance export, then provide the reporting period and metric definitions.',
  optionalConnections: 'Spreadsheet skill or accounting MCP for connected data; Slack or email for delivery.',
  exampleRequests: [
    'Prepare a finance brief for last month from this statement. Show your calculations.',
    'What changed in expenses between these two periods, and which items need review?',
  ],
  selectedSkills: ['finance-analyst'],
  setupPath: 'TEMPLATE_SETUP.json',
  setupGuidePath: 'TEMPLATE_SETUP.md',
  requiredFiles: ['skills/finance-analyst/SKILL.md', 'TEMPLATE_SETUP.md', 'TEMPLATE_SETUP.json'],
  files: {
    'skills/finance-analyst/SKILL.md': financeAnalystSkill,
    'TEMPLATE_SETUP.md': financeAnalystSetup,
    'TEMPLATE_SETUP.json': financeAnalystSetupState,
  },
}, {
  id: 'tax-export',
  version: 1,
  category: 'Finance',
  name: 'Tax Export Preparer',
  icon: '🧾',
  role: 'Tax records preparer for this business',
  purpose: 'Organize authorized transaction records into a traceable export for review by the business and its tax professional.',
  firstResult: 'A reconciled transaction export with source references and an exceptions list.',
  minimumInput: 'Provide the reporting period, jurisdiction, and authorized transaction records.',
  optionalConnections: 'Accounting MCP or spreadsheet skill for connected records; secure delivery after approval.',
  exampleRequests: [
    'Prepare a transaction export for my accountant for this period, with an exceptions list.',
    'Reconcile these invoices and payments before the tax handoff.',
  ],
  selectedSkills: ['tax-export'],
  setupPath: 'templates/tax-export/TEMPLATE_SETUP.json',
  setupGuidePath: 'templates/tax-export/SETUP.md',
  requiredFiles: ['skills/tax-export/SKILL.md', 'templates/tax-export/SETUP.md', 'templates/tax-export/TEMPLATE_SETUP.json'],
  files: {
    'skills/tax-export/SKILL.md': taxExportSkill,
    'templates/tax-export/SETUP.md': taxExportSetup,
    'templates/tax-export/TEMPLATE_SETUP.json': taxExportSetupState,
  },
}, {
  id: 'website-growth-starter',
  version: 1,
  category: 'Website Growth',
  name: 'Website Growth Starter',
  icon: '🌱',
  role: 'Website growth strategist for this business',
  purpose: 'Audit the business website, find evidence-backed opportunities for relevant traffic, and guide a measurable 30-day growth plan.',
  firstResult: 'A sourced website audit and prioritized 30-day traffic growth plan.',
  minimumInput: 'Public website URL or page export, target audience, and primary visitor action.',
  optionalConnections: 'Search Console and analytics for measurement; a repository or CMS for reviewed page edits.',
  exampleRequests: [
    'Audit my newly launched website and propose the first five actions to attract relevant visitors.',
    'Review our homepage and product page. What is blocking discovery, and what should we measure next?',
  ],
  selectedSkills: ['website-growth-starter'],
  setupPath: 'templates/website-growth-starter/TEMPLATE_SETUP.json',
  setupGuidePath: 'templates/website-growth-starter/SETUP.md',
  requiredFiles: ['skills/website-growth-starter/SKILL.md', 'templates/website-growth-starter/SETUP.md', 'templates/website-growth-starter/TEMPLATE_SETUP.json'],
  files: {
    'skills/website-growth-starter/SKILL.md': websiteGrowthSkill,
    'templates/website-growth-starter/SETUP.md': websiteGrowthSetup,
    'templates/website-growth-starter/TEMPLATE_SETUP.json': websiteGrowthSetupState,
  },
}, ...websiteGrowthSpecialists]

export function parseCrewTemplateSetupState(content: string, template: CrewTemplate): CrewTemplateSetupState | null {
  try {
    const raw = JSON.parse(content) as Partial<CrewTemplateSetupState>
    if (raw.schema_version !== 1 || raw.template_id !== template.id || raw.template_version !== template.version || !Array.isArray(raw.checks) || !Array.isArray(raw.completed_steps)) return null
    if (raw.checks.length < 5 || raw.checks.length > 10) return null
    const ids = new Set<string>()
    for (const check of raw.checks) {
      if (!check || typeof check.id !== 'string' || !check.id || typeof check.title !== 'string' || !check.title || typeof check.instructions !== 'string' || !check.instructions || ids.has(check.id)) return null
      ids.add(check.id)
    }
    // Saved progress cannot remove requirements from the installed template.
    const canonical = JSON.parse(template.files[template.setupPath]) as CrewTemplateSetupState
    if (!Array.isArray(canonical.checks) || canonical.checks.length !== ids.size || canonical.checks.some(check => !ids.has(check.id))) return null
    if (raw.completed_steps.some(id => typeof id !== 'string' || !ids.has(id))) return null
    return raw as CrewTemplateSetupState
  } catch {
    return null
  }
}

export function getCrewTemplate(id: CrewTemplateId): CrewTemplate {
  const template = crewTemplates.find(item => item.id === id)
  if (!template) throw new Error(`Unknown Crew template: ${id}`)
  if (!parseCrewTemplateSetupState(template.files[template.setupPath] || '', template)) {
    throw new Error(`Invalid setup checklist for Crew template: ${id}`)
  }
  if (template.requiredFiles.some(path => !(path in template.files))) {
    throw new Error(`Missing required file in Crew template: ${id}`)
  }
  return template
}
