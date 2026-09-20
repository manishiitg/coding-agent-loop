import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { WorkspaceViewActions } from './WorkspaceViewActions'

vi.mock('./AskAIButton', () => ({
  AskAIButton: () => <button type="button" data-testid="ask-ai">Ask AI</button>,
}))

describe('WorkspaceViewActions', () => {
  it('keeps Ask AI left of refresh in every right-pane header', () => {
    const html = renderToStaticMarkup(
      <WorkspaceViewActions workspacePath="Workflow/one" message="help" onRefresh={() => {}} />,
    )
    const askIndex = html.indexOf('data-testid="ask-ai"')
    const refreshIndex = html.indexOf('aria-label="Refresh view"')
    expect(askIndex).toBeGreaterThanOrEqual(0)
    expect(refreshIndex).toBeGreaterThanOrEqual(0)
    expect(askIndex).toBeLessThan(refreshIndex)
  })
})
