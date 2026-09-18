import { describe, expect, it } from 'vitest'
import { WORKSPACE_VIEWS } from './workspaceViews'
import { getWorkspaceAskAIMessage, WORKSPACE_ASK_AI_MESSAGE } from './workspaceAskAI'

describe('workspace Ask AI', () => {
  it('provides guided help for every registered right-side view', () => {
    for (const view of WORKSPACE_VIEWS) {
      expect(WORKSPACE_ASK_AI_MESSAGE[view.id]).toBeTruthy()
      const message = getWorkspaceAskAIMessage(view.id)
      expect(message).toContain('read_skill')
      expect(message).toContain('references/workflow-guide.md')
      expect(message).toContain(`${view.label} view`)
      expect(message).toContain(`use the ${view.label} view as context`)
      expect(message).not.toMatch(/help me with the .* view\. Help me/i)
    }
  })
})
