import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { Activity } from 'lucide-react'
import { WorkspaceViewHeader } from './WorkspaceViewHeader'

describe('WorkspaceViewHeader', () => {
  it('lays out icon, title, context, subtitle, actions, and below in order', () => {
    const html = renderToStaticMarkup(
      <WorkspaceViewHeader
        icon={<span data-testid="icon" />}
        title="Costs"
        context={<span data-testid="context" />}
        subtitle="Where the money goes"
        actions={<><button type="button" data-testid="ask-ai">Ask AI</button><button type="button" data-testid="refresh">Refresh</button></>}
        below={<div data-testid="below" />}
      />,
    )
    const order = ['data-testid="icon"', '>Costs<', 'data-testid="context"', 'Where the money goes', 'data-testid="ask-ai"', 'data-testid="refresh"', 'data-testid="below"']
      .map(token => html.indexOf(token))
    for (const index of order) expect(index).toBeGreaterThanOrEqual(0)
    expect([...order].sort((a, b) => a - b)).toEqual(order)
    expect(html).toContain('<header')
    expect(html).toContain('<h2')
  })

  it('renders the active tab actions left of the base actions', () => {
    const render = (value: 'apps' | 'skills') => renderToStaticMarkup(
      <WorkspaceViewHeader
        title="Integrations"
        actions={<button type="button" data-testid="ask-ai">Ask AI</button>}
        tabs={{ value, onChange: () => {}, options: [{ value: 'apps', label: 'Apps' }, { value: 'skills', label: 'Skills' }], ariaLabel: 'Integrations' }}
        tabActions={{ apps: <button type="button" data-testid="tab-action">Apps action</button> }}
      />,
    )
    const html = render('apps')
    const tabActionIndex = html.indexOf('data-testid="tab-action"')
    const askIndex = html.indexOf('data-testid="ask-ai"')
    expect(tabActionIndex).toBeGreaterThanOrEqual(0)
    expect(tabActionIndex).toBeLessThan(askIndex)
    expect(render('skills')).not.toContain('data-testid="tab-action"')
  })

  it('renders an icon reference in the standard h-9 tile', () => {
    const html = renderToStaticMarkup(<WorkspaceViewHeader icon={Activity} title="Pulse" />)
    expect(html).toContain('h-9 w-9')
    expect(html).toContain('border-primary/25')
  })

  it('renders sticky and bare variants', () => {
    const sticky = renderToStaticMarkup(<WorkspaceViewHeader title="Webhooks" sticky />)
    expect(sticky).toContain('sticky top-0')
    const bare = renderToStaticMarkup(<WorkspaceViewHeader title="Costs" bare />)
    expect(bare).not.toContain('<header')
    expect(bare).toContain('>Costs<')
  })
})
