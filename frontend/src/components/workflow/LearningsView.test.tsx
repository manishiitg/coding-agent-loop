// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { beforeEach, expect, it, vi } from 'vitest'
import { agentApi } from '../../services/api'
import LearningsView from './LearningsView'

type MarkdownRendererProps = {
  content: string
  onWorkspaceLinkResolve?: (filepath: string, displayPath: string) => { filepath: string; displayPath: string } | null | undefined
  onWorkspaceLinkClick?: (filepath: string, displayPath: string) => boolean | void
}

vi.mock('../../services/api', () => ({
  agentApi: {
    getPlannerFiles: vi.fn(),
    getPlannerFileContent: vi.fn(),
  },
}))

vi.mock('../ui/MarkdownRenderer', () => ({
  MarkdownRenderer: (props: MarkdownRendererProps) => (
    <div data-testid="markdown-renderer">
      <span>{props.content}</span>
      {props.content === 'Global skill' && (
        <>
          <button
            data-testid="open-root-link"
            onClick={() => {
              const target = props.onWorkspaceLinkResolve?.(
                'Workflow/demo/learnings/_global/guide.md',
                'guide.md',
              )
              if (target) props.onWorkspaceLinkClick?.(target.filepath, target.displayPath)
            }}
          >
            Open root guide
          </button>
          <button
            data-testid="open-nested-link"
            onClick={() => {
              const target = props.onWorkspaceLinkResolve?.(
                'Workflow/demo/learnings/references/details.md',
                '../references/details.md',
              )
              if (target) props.onWorkspaceLinkClick?.(target.filepath, target.displayPath)
            }}
          >
            Open nested details
          </button>
        </>
      )}
    </div>
  ),
}))

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

const plannerFiles = [
  {
    name: '_global',
    filepath: 'Workflow/demo/learnings/_global',
    type: 'folder',
    children: [
      { name: 'SKILL.md', filepath: 'Workflow/demo/learnings/_global/SKILL.md', type: 'file' },
      { name: 'guide.md', filepath: 'Workflow/demo/learnings/_global/guide.md', type: 'file' },
      { name: '_freshness.json', filepath: 'Workflow/demo/learnings/_global/_freshness.json', type: 'file' },
    ],
  },
  {
    name: 'references',
    filepath: 'Workflow/demo/learnings/references',
    type: 'folder',
    children: [
      { name: 'details.md', filepath: 'Workflow/demo/learnings/references/details.md', type: 'file' },
    ],
  },
]

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(agentApi.getPlannerFiles).mockResolvedValue(plannerFiles as never)
  vi.mocked(agentApi.getPlannerFileContent).mockImplementation(async filepath => ({
    success: true,
    data: {
      content: filepath.endsWith('SKILL.md')
        ? 'Global skill'
        : filepath.endsWith('_freshness.json')
          ? '{"items":{}}'
          : `Content for ${filepath}`,
    },
  }) as never)
})

it('keeps freshness metadata out of the visible learning files', async () => {
  const container = document.createElement('div')
  const root = createRoot(container)
  try {
    await act(async () => root.render(<LearningsView workspacePath="Workflow/demo" plan={null} />))
    expect(container.textContent).toContain('Additional files (2)')
    expect(container.textContent).toContain('guide.md')
    expect(container.textContent).toContain('details.md')
    expect(container.textContent).not.toContain('_freshness.json')
  } finally {
    await act(async () => root.unmount())
  }
})

it('opens root and nested learning links inline', async () => {
  const container = document.createElement('div')
  const root = createRoot(container)
  try {
    await act(async () => root.render(<LearningsView workspacePath="Workflow/demo" plan={null} />))

    const rootLink = container.querySelector<HTMLButtonElement>('[data-testid="open-root-link"]')
    await act(async () => rootLink?.click())
    expect(container.textContent).toContain('Content for Workflow/demo/learnings/_global/guide.md')

    const nestedLink = container.querySelector<HTMLButtonElement>('[data-testid="open-nested-link"]')
    await act(async () => nestedLink?.click())
    expect(container.textContent).toContain('Content for Workflow/demo/learnings/references/details.md')
  } finally {
    await act(async () => root.unmount())
  }
})
