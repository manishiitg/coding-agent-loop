import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { LogsHeader } from './LogsHeader'

describe('LogsHeader', () => {
  it('keeps Ask AI left of refresh', () => {
    const html = renderToStaticMarkup(
      <LogsHeader
        runFolderOptions={[]}
        runFolderInfos={[]}
        selectedRunFolder=""
        setSelectedRunFolder={() => {}}
        loading={false}
        loadLogs={() => {}}
        headerAction={<button type="button" data-testid="ask-ai">Ask AI</button>}
      />,
    )
    const askIndex = html.indexOf('data-testid="ask-ai"')
    const refreshIndex = html.indexOf('aria-label="Refresh logs and run-folder list"')
    expect(askIndex).toBeGreaterThanOrEqual(0)
    expect(refreshIndex).toBeGreaterThanOrEqual(0)
    expect(askIndex).toBeLessThan(refreshIndex)
  })

  it('renders the run picker below the header buttons', () => {
    const html = renderToStaticMarkup(
      <LogsHeader
        runFolderOptions={['run-1', 'run-2']}
        runFolderInfos={[]}
        selectedRunFolder="run-1"
        setSelectedRunFolder={() => {}}
        loading={false}
        loadLogs={() => {}}
        headerAction={<button type="button" data-testid="ask-ai">Ask AI</button>}
      />,
    )
    const refreshIndex = html.indexOf('aria-label="Refresh logs and run-folder list"')
    const pickerIndex = html.indexOf('aria-label="Execution run"')
    expect(refreshIndex).toBeGreaterThanOrEqual(0)
    expect(pickerIndex).toBeGreaterThan(refreshIndex)
  })
})
