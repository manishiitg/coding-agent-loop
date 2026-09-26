import { describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({ getApiBaseUrl: () => 'http://host', getAuthToken: () => 'tok' }))

import { liveFeedWorkflowRoot } from './liveFeed'

describe('liveFeedWorkflowRoot', () => {
  it('matches the server: workflow and shared crew roots only', () => {
    expect(liveFeedWorkflowRoot('Workflow/sales/db/db.sqlite')).toBe('Workflow/sales')
    expect(liveFeedWorkflowRoot('/Crew/sde-1a2b/db/reports/index.html')).toBe('Crew/sde-1a2b')
    expect(liveFeedWorkflowRoot('Chats/Work/projects/sde')).toBeNull()
    expect(liveFeedWorkflowRoot('Crew')).toBeNull()
    expect(liveFeedWorkflowRoot(null)).toBeNull()
  })
})
