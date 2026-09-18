// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'

const refreshWorkflows = vi.fn()

vi.mock('../../stores/useWorkflowManifestStore', () => ({
  useWorkflowManifestStore: (selector: (state: unknown) => unknown) => selector({
    workflows: [{
      workspace_path: 'Workflow/release',
      manifest: { label: 'Release', icon: '🚀' },
    }],
    refreshWorkflows,
  }),
}))

import { WorkflowReferenceAccess } from './WorkflowReferenceAccess'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

describe('WorkflowReferenceAccess', () => {
  it('separates workflow and Crew references and renders configured icons', async () => {
    const onChange = vi.fn()
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)

    await act(async () => root.render(<WorkflowReferenceAccess
      selectedPaths={['Workflow/release', 'Work/projects/ops']}
      onChange={onChange}
      excludeWorkspacePath="Work/projects/current"
      additionalReferences={[
        { path: 'Work/projects/current', label: 'Current Crew', icon: '🏠' },
        { path: 'Work/projects/ops', label: 'Operations', icon: '🧭' },
      ]}
      showAdditionalGroup
    />))

    try {
      expect(host.textContent).toContain('Workflows')
      expect(host.textContent).toContain('Crew')
      expect(host.textContent).toContain('🚀')
      expect(host.textContent).toContain('🧭')
      expect(host.textContent).not.toContain('Current Crew')
    } finally {
      await act(async () => root.unmount())
      host.remove()
    }
  })
})
