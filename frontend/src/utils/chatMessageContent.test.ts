import { describe, expect, it } from 'vitest'
import { getDisplaySafeUserMessageContent } from './chatMessageContent'

describe('getDisplaySafeUserMessageContent', () => {
  it('keeps typed text and hides attached-file transport context', () => {
    expect(getDisplaySafeUserMessageContent(
      'check this file\n\n📁 Files in context: Chats/Work/project/uploads/report.zip',
    )).toBe('check this file')
  })

  it('hides restored-conversation transport context', () => {
    expect(getDisplaySafeUserMessageContent(
      'continue here\n\nPrevious workflow-builder conversation file: Chats/Work/history.json',
    )).toBe('continue here')
  })

  it('keeps ordinary user text unchanged', () => {
    expect(getDisplaySafeUserMessageContent('Explain the files in context')).toBe('Explain the files in context')
  })
})
