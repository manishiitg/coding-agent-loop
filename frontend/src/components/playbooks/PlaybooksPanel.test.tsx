// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import PlaybooksPanel from './PlaybooksPanel'

vi.mock('../../api/playbooks', () => ({
  playbooksApi: {
    list: vi.fn().mockResolvedValue([{
      id: 'basic-browser-setup', title: 'Basic Browser Setup', description: 'Configure browser access.', version: '0.6.0',
      category: 'Browser QA', order: 1, inputCount: 2, toolCount: 1,
      setupInputs: [
        { id: 'environment', label: 'Application and environment', required: true },
        { id: 'reporting', label: 'Reporting preference', required: false },
      ],
      requiredCapabilities: ['browser_test_execution', 'workflow_reporting'],
      recommendedTools: [{ id: 'playwright', name: 'Playwright', type: 'cli', purpose: 'Run repeatable browser tests', capability: 'browser_test_execution', optional: true }],
      outputs: ['Reusable browser foundation', 'Live workflow report'],
    }]),
    listInstalled: vi.fn().mockResolvedValue([]),
    install: vi.fn().mockResolvedValue({
      id: 'basic-browser-setup', title: 'Basic Browser Setup', version: '0.6.0',
      category: 'Browser QA', skill_name: 'agentworks-playbook-basic-browser-setup', source_hash: 'sha256:test',
      status: 'draft', installed_at: '2026-09-13T00:00:00Z',
    }),
  },
}))

vi.mock('../../hooks/useCanWriteWorkflow', () => ({
  useCanWriteWorkflow: () => true,
  READ_ONLY_TITLE: 'Read only',
}))

vi.mock('../workflow/AskAIButton', () => ({
  AskAIButton: ({ label }: { label: string }) => <button type="button">{label}</button>,
}))

vi.mock('../../stores/useWorkflowManifestStore', () => ({
  useWorkflowManifestStore: { getState: () => ({ refreshWorkflows: vi.fn().mockResolvedValue(undefined) }) },
}))

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const click = async (element: Element | null) => {
  if (!(element instanceof HTMLElement)) throw new Error('Expected a clickable element')
  await act(async () => element.click())
}

it('opens a catalog playbook and installs it for Builder setup', async () => {
  const container = document.createElement('div')
  const root = createRoot(container)
  try {
    await act(async () => {
      root.render(<PlaybooksPanel workspacePath="Workflow/demo" />)
      await Promise.resolve()
    })
    expect(container.textContent).toContain('Catalog 1')
    expect(container.textContent).toContain('Basic Browser Setup')
    expect(container.textContent).toContain('AgentWorks')
    expect(container.textContent).toContain('Agentic Engineering Platform 1 playbooks')
    expect(container.textContent).toContain('Browser QA1')
    expect(container.querySelector('[aria-label="Playbook catalog hierarchy"]')).not.toBeNull()

    await click([...container.querySelectorAll('button')].find(button => button.textContent?.includes('Basic Browser Setup')) || null)
    expect(container.textContent).toContain('Setup with Builder')
    expect(container.textContent).toContain('2 inputs guide adaptation')
    expect(container.textContent).toContain('What this playbook covers')
    expect(container.textContent).toContain('Application and environment')
    expect(container.textContent).toContain('Reporting preference(optional)')
    expect(container.textContent).toContain('browser test execution')
    expect(container.textContent).toContain('What you’ll get')
    expect(container.textContent).toContain('Reusable browser foundation')
    expect(container.textContent).toContain('Run repeatable browser tests')

    await click([...container.querySelectorAll('button')].find(button => button.textContent?.includes('Use playbook')) || null)
    expect(container.textContent).toContain('Installed · draft')
    expect(container.textContent).toContain('Continue setup in Builder')

    await click([...container.querySelectorAll('button')].find(button => button.textContent?.includes('Back to catalog')) || null)
    await click([...container.querySelectorAll('[role="tab"]')].find(button => button.textContent?.includes('Installed')) || null)
    expect(container.textContent).toContain('Basic Browser Setup')
    expect(container.textContent).toContain('Browser QA · v0.6.0 · draft')
  } finally {
    await act(async () => root.unmount())
  }
})
