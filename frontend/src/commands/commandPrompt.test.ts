import { describe, expect, it } from 'vitest'
import { renderCommandPrompt } from './commandPrompt'

describe('renderCommandPrompt', () => {
  const daily = 'Update the Daily page.\n4. Confirm before writing.\n\nAdditional context from me: {{context}}'

  it('drops a dangling context label when nothing was typed', () => {
    expect(renderCommandPrompt(daily, '')).toBe('Update the Daily page.\n4. Confirm before writing.')
    expect(renderCommandPrompt('Do it.\n\nAdditional context from me (e.g. a time window):\n{{context}}', '  ')).toBe('Do it.')
  })

  it('fills the label when context was typed', () => {
    expect(renderCommandPrompt(daily, ' shipped WEB-12 ')).toMatch(/Additional context from me: shipped WEB-12$/)
  })

  it('appends typed text to a command without the placeholder', () => {
    expect(renderCommandPrompt('Sync the sprint.', 'only WEB-12')).toBe('Sync the sprint.\n\nonly WEB-12')
    expect(renderCommandPrompt('Sync the sprint.', '')).toBe('Sync the sprint.')
  })

  it('keeps inline placeholders as empty values', () => {
    expect(renderCommandPrompt('Call guidance(kind="design-plan", focus="{{context}}") now.', ''))
      .toBe('Call guidance(kind="design-plan", focus="") now.')
  })
})
