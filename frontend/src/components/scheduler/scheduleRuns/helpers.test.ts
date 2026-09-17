import { describe, expect, it } from 'vitest'
import type { ScheduledJob } from '../../../services/api-types'
import { defaultSchedulePanelView, jobMatchesWorkflowScope } from './helpers'

describe('defaultSchedulePanelView', () => {
  it('opens global views on the per-workflow grouping', () => {
    expect(defaultSchedulePanelView(false)).toBe('by-workflow')
  })

  it('opens a workflow-scoped view on the schedule list', () => {
    expect(defaultSchedulePanelView(true)).toBe('schedules')
  })
})

describe('jobMatchesWorkflowScope', () => {
  it('matches a product schedule by stable project identity across public and user-scoped paths', () => {
    const job = {
      entity_type: 'product',
      workflow_id: 'project-1',
      workspace_path: '_users/user-1/Chats/Work/projects/project-1',
    } as ScheduledJob

    expect(jobMatchesWorkflowScope(job, {
      workflowId: 'project-1',
      workspacePath: 'Chats/Work/projects/project-1',
    }, new Map())).toBe(true)
  })

  it('does not expose another project schedule from the same user', () => {
    const job = {
      entity_type: 'product',
      workflow_id: 'project-2',
      workspace_path: '_users/user-1/Chats/Work/projects/project-2',
    } as ScheduledJob

    expect(jobMatchesWorkflowScope(job, {
      workflowId: 'project-1',
      workspacePath: 'Chats/Work/projects/project-1',
    }, new Map())).toBe(false)
  })
})
