import { describe, expect, it } from 'vitest'
import { referenceKind, referenceTag, removeReferenceTags, textMentionsReference, workflowContextRefs } from './referenceTags'

const crew = { presetId: 'crew:beta-123', label: 'Beta Bot', workspacePath: 'Chats/Work/projects/beta' }
const workflow = { presetId: 'wf-reports', label: 'Reports', workspacePath: 'Workflow/reports' }

describe('reference tags', () => {
  it('types crew and workflow tags so they never read as Slack channels', () => {
    expect(referenceKind(crew)).toBe('crew')
    expect(referenceKind(workflow)).toBe('workflow')
    expect(referenceTag(crew)).toBe('#crew:Beta Bot')
    expect(referenceTag(workflow)).toBe('#workflow:Reports')
  })

  it('detects and removes typed and legacy bare tags', () => {
    expect(textMentionsReference('ask #crew:Beta Bot to help', crew)).toBe(true)
    expect(textMentionsReference('old draft #Beta Bot', crew)).toBe(true)
    expect(textMentionsReference('nothing here', crew)).toBe(false)
    expect(removeReferenceTags('ask #crew:Beta Bot  to help', crew)).toBe('ask to help')
  })

  it('sends path, label and kind for every reference', () => {
    expect(workflowContextRefs([crew, workflow])).toEqual([
      { path: 'Chats/Work/projects/beta', label: 'Beta Bot', kind: 'crew' },
      { path: 'Workflow/reports', label: 'Reports', kind: 'workflow' },
    ])
  })
})
