export type WorkflowSoulSummary = {
  goal: string
  success: string
  constraints: string
}

type MarkdownSection = {
  level: number
  lines: string[]
}

function normalizeHeading(value: string): string {
  return value
    .replace(/\s+#+\s*$/, '')
    .trim()
    .toLowerCase()
}

function sectionFor(markdown: string, names: string[]): MarkdownSection | null {
  const accepted = new Set(names.map(normalizeHeading))
  const lines = markdown.split(/\r?\n/)
  let section: MarkdownSection | null = null

  for (const line of lines) {
    const heading = line.match(/^(#{1,6})\s+(.+?)\s*$/)
    if (heading) {
      const level = heading[1].length
      if (!section && accepted.has(normalizeHeading(heading[2]))) {
        section = { level, lines: [] }
        continue
      }
      if (section && level <= section.level) break
    }
    if (section) section.lines.push(line)
  }
  return section
}

function plainLine(value: string): string {
  return value
    .replace(/^\s*(?:[-*+]|\d+[a-z]?[.)])\s+/i, '')
    .replace(/!\[([^\]]*)]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)]\([^)]*\)/g, '$1')
    .replace(/[*_~`>#]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

function firstParagraph(section: MarkdownSection | null): string {
  if (!section) return ''
  const paragraph: string[] = []
  for (const raw of section.lines) {
    if (/^#{1,6}\s+/.test(raw)) {
      if (paragraph.length > 0) break
      continue
    }
    const line = plainLine(raw)
    if (!line) {
      if (paragraph.length > 0) break
      continue
    }
    paragraph.push(line)
  }
  return paragraph.join(' ')
}

function firstConcreteCriterion(section: MarkdownSection | null): string {
  if (!section) return ''
  const listItem = section.lines.find((line) => (
    /^\s*(?:[-*+]|\d+[a-z]?[.)])\s+\S/i.test(line)
  ))
  if (listItem) return plainLine(listItem)

  for (const raw of section.lines) {
    if (/^#{1,6}\s+/.test(raw)) continue
    const line = plainLine(raw)
    if (!line || /^a (?:run|workflow|result) is successful when\b/i.test(line)) continue
    return line
  }
  return ''
}

export function extractWorkflowSoulSummary(markdown: string): WorkflowSoulSummary {
  const sections = extractWorkflowGoalSections(markdown)
  return {
    goal: firstParagraph(sections.primaryGoals ? { level: 3, lines: sections.primaryGoals.split('\n') } : sectionFor(markdown, ['Objective', 'Goal'])),
    success: firstConcreteCriterion(sectionFor(markdown, ['Success Criteria', 'Success'])),
    constraints: firstConcreteCriterion(sectionFor(markdown, ['Constraints', 'Guardrails'])),
  }
}

export function extractWorkflowGoalSections(markdown: string) {
  const objective = sectionFor(markdown, ['Objective', 'Goal'])?.lines.join('\n').trim() || ''
  const groups = { primary: [] as string[], secondary: [] as string[], other: [] as string[] }
  let group: keyof typeof groups = 'other'
  let fence: string | undefined
  for (const line of objective.split('\n')) {
    // Examples inside fenced code are content, never priority declarations.
    const marker = line.match(/^\s{0,3}(`{3,}|~{3,})/)?.[1]
    if (marker) {
      if (!fence) fence = marker
      else if (marker[0] === fence[0] && marker.length >= fence.length) fence = undefined
      groups[group].push(line)
      continue
    }
    const heading = !fence && line.match(/^###\s+(.+?)\s*$/)
    if (heading) {
      const name = normalizeHeading(heading[1])
      if (/^primary (?:goals?|outcomes?)$/.test(name)) { group = 'primary'; continue }
      if (/^secondary (?:goals?|outcomes?)$/.test(name)) { group = 'secondary'; continue }
      group = 'other'
    }
    groups[group].push(line)
  }
  // Keep unclassified legacy goals and extra context intact. Order alone never
  // makes an outcome primary, and unknown headings must not discard commitments.
  const primaryGoals = groups.primary.join('\n').trim()
  const secondaryGoals = groups.secondary.join('\n').trim()
  const otherGoals = groups.other.join('\n').trim()
  const goal = /^(?:\s*(?:[-*+]|\d+[.)])\s|#{1,6}\s)/m.test(objective) ? objective
    : objective.split(/\n\s*\n/).filter(Boolean).map(p => `- ${p.replace(/\n/g, ' ')}`).join('\n')
  return { goal, primaryGoals, secondaryGoals, otherGoals, acceptance: sectionFor(markdown, ['Success Criteria', 'Success'])?.lines.join('\n').trim() || '', boundaries: sectionFor(markdown, ['Constraints', 'Guardrails'])?.lines.join('\n').trim() || '' }
}
