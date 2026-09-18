import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { WorkflowIcon } from './WorkflowIcon'

describe('WorkflowIcon', () => {
  it('renders the configured workflow symbol', () => {
    expect(renderToStaticMarkup(<WorkflowIcon icon="📊" label="Daily review" />)).toContain('📊')
  })

  it('gives existing workflows a stable initial without changing their manifests', () => {
    expect(renderToStaticMarkup(<WorkflowIcon label="Revenue review" />)).toContain('R')
  })
})
