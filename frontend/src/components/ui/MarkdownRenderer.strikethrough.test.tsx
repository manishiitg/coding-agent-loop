// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { MarkdownRenderer } from './MarkdownRenderer'

vi.mock('../../stores/useWorkspaceStore', () => ({
  useWorkspaceStore: (selector: (state: Record<string, unknown>) => unknown) => selector({}),
}))
vi.mock('../../stores/useAppStore', () => ({
  useAppStore: (selector: (state: Record<string, unknown>) => unknown) => selector({}),
}))
vi.mock('../../stores/useModeStore', () => ({
  useModeStore: { getState: () => ({ selectedModeCategory: null }) },
}))
vi.mock('../../stores/useWorkflowStore', () => ({
  useWorkflowStore: { getState: () => ({}) },
}))
vi.mock('../../stores/useProductSurfaceStore', () => ({
  useProductSurfaceStore: { getState: () => ({ productSurface: 'agentworks' }) },
}))
vi.mock('../../stores/useGlobalPresetStore', () => ({
  useGlobalPresetStore: { getState: () => ({}) },
}))
vi.mock('../../services/api', () => ({
  workspaceApi: {},
  agentApi: {},
  getApiBaseUrl: () => '',
}))

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

async function renderContent(content: string): Promise<HTMLElement> {
  const container = document.createElement('div')
  const root = createRoot(container)
  await act(async () => root.render(<MarkdownRenderer content={content} />))
  return container
}

it('leaves single-tilde approximates alone instead of striking them', async () => {
  // Model finance text writes ~ for "approximately"; pairing those tildes
  // into <del> spans struck whole paragraphs.
  const container = await renderContent('gave credit (~2.37L of buys) and (~1.19L in September)')
  expect(container.querySelector('del')).toBeNull()
  expect(container.textContent).toContain('(~2.37L of buys)')
})

it('still renders double-tilde strikethrough like GitHub', async () => {
  const container = await renderContent('this is ~~gone~~ here')
  expect(container.querySelector('del')?.textContent).toBe('gone')
})
