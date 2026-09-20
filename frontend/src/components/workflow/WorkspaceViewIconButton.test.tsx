import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { Pause } from 'lucide-react'
import { WorkspaceViewIconButton } from './WorkspaceViewIconButton'

describe('WorkspaceViewIconButton', () => {
  it('renders the standard header action look with label and spin state', () => {
    const html = renderToStaticMarkup(
      <WorkspaceViewIconButton label="Refresh view" onClick={() => {}} spinning />,
    )
    expect(html).toContain('h-8 w-8')
    expect(html).toContain('aria-label="Refresh view"')
    expect(html).toContain('title="Refresh view"')
    expect(html).toContain('animate-spin')
    const disabled = renderToStaticMarkup(
      <WorkspaceViewIconButton label="Refresh view" onClick={() => {}} disabled />,
    )
    expect(disabled).toContain('disabled')
  })

  it('renders a custom icon', () => {
    const html = renderToStaticMarkup(
      <WorkspaceViewIconButton label="Pause all" onClick={() => {}} icon={Pause} />,
    )
    expect(html).toContain('aria-label="Pause all"')
  })
})
