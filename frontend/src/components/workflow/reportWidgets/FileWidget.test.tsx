// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('../../../services/api', () => ({
  agentApi: { getPlannerFileContent: vi.fn() },
  workspaceApi: { get: vi.fn() },
}))
// Use the real browser converter, matching Vite's browser entry resolution.
vi.mock('mammoth', () => ({ default: createRequire(import.meta.url)('mammoth/mammoth.browser.js') }))

import { agentApi, workspaceApi } from '../../../services/api'
import { FilePreviewByPath, FileWidget } from './FileWidget'
import { DocxRenderer } from '../../ui/DocxRenderer'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const fixture = readFileSync(createRequire(import.meta.url).resolve('./__fixtures__/resume.docx'))
const resume = Uint8Array.from(fixture).buffer
const cleanups: (() => Promise<void>)[] = []

afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) await cleanup()
  vi.resetAllMocks()
  vi.restoreAllMocks()
})

async function mount(element: React.ReactNode) {
  const host = document.createElement('div')
  const root = createRoot(host)
  const render = async (next: React.ReactNode) => { await act(async () => root.render(next)) }
  cleanups.push(async () => { await act(async () => root.unmount()) })
  await render(element)
  return { host, render }
}

async function waitForText(host: HTMLElement, text: string) {
  await vi.waitFor(async () => {
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)) })
    expect(host.textContent).toContain(text)
  })
}

describe('DOCX report previews', () => {
  it.each(['modal', 'widget'])('renders actual DOCX text and tables in the %s', async surface => {
    vi.mocked(workspaceApi.get).mockResolvedValue({ data: resume })
    const path = 'Workflow/jobsearch/db/assets/resumes/Example Resume.DOCX'
    const { host } = await mount(surface === 'modal'
      ? <FilePreviewByPath path={path} />
      : <FileWidget workspacePath="Workflow/jobsearch" widget={{ id: 'resume', kind: 'file', path: '', source: 'db/assets/resumes/Example Resume.DOCX' }} />)
    await waitForText(host, 'Example Resume')
    await waitForText(host, 'Software Engineer')
    expect(host.querySelector('strong')?.textContent).toBe('Experience')
    expect(host.querySelector('td')?.textContent).toBe('Software Engineer')
    expect(workspaceApi.get).toHaveBeenCalledWith(`/api/documents/${encodeURIComponent(path)}`, expect.objectContaining({
      params: { download: 'true' }, responseType: 'arraybuffer',
    }))
    expect(agentApi.getPlannerFileContent).not.toHaveBeenCalled()
    expect(host.textContent).not.toContain('No inline preview')
  })

  it('shows download failures', async () => {
    vi.mocked(workspaceApi.get).mockRejectedValue(new Error('File unavailable'))
    const { host } = await mount(<FilePreviewByPath path="Workflow/jobsearch/db/resume.docx" />)
    await waitForText(host, 'Could not load resume.docx.')
    expect(host.textContent).toContain('File unavailable')
  })

  it('shows malformed document errors and recovers when given a valid document', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const { host, render } = await mount(<DocxRenderer data={new ArrayBuffer(3)} />)
    await waitForText(host, 'Failed to render DOCX')
    await render(<DocxRenderer data={resume} />)
    await waitForText(host, 'Software Engineer')
    expect(host.textContent).not.toContain('Failed to render DOCX')
  })

  it('ignores an old download after selecting another file', async () => {
    let resolveOld!: (value: { data: ArrayBuffer }) => void
    vi.mocked(workspaceApi.get)
      .mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce({ data: resume })
    const { host, render } = await mount(<FilePreviewByPath path="Workflow/jobsearch/db/old.docx" />)
    await render(<FilePreviewByPath path="Workflow/jobsearch/db/new.docx" />)
    await waitForText(host, 'Software Engineer')
    await act(async () => resolveOld({ data: new ArrayBuffer(3) }))
    expect(host.textContent).toContain('Software Engineer')
    expect(host.textContent).not.toContain('Failed to render DOCX')
  })
})
