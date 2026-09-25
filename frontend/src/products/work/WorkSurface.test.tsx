// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CreateWorkProjectDialog } from './CreateWorkProjectDialog'
import { crewTemplates, type CrewTemplateId } from './crewTemplates'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

describe('CreateWorkProjectDialog', () => {
  let container: HTMLDivElement | null = null

  afterEach(() => {
    container?.remove()
    container = null
  })

  it('requires a name and submits trimmed project details', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onCreate = vi.fn()

    await act(async () => {
      root.render(<CreateWorkProjectDialog onClose={() => {}} onCreate={onCreate} submitting={false} error={null} />)
    })

    const submit = container.querySelector('[data-testid="work-create-project-submit"]') as HTMLButtonElement
    expect(container.textContent).toContain('Create a Crew member')
    expect(submit.textContent).toContain('Create Crew member')
    expect(submit.disabled).toBe(true)

    const name = container.querySelector('[data-testid="work-create-project-name-input"]') as HTMLInputElement
    const icon = container.querySelector('[data-testid="work-create-project-icon-input"]') as HTMLInputElement
    const description = container.querySelector('textarea') as HTMLTextAreaElement
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(icon, '🚀')
      icon.dispatchEvent(new Event('input', { bubbles: true }))
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(name, '  Customer portal  ')
      name.dispatchEvent(new Event('input', { bubbles: true }))
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(description, '  Build and maintain the portal.  ')
      description.dispatchEvent(new Event('input', { bubbles: true }))
    })

    expect(submit.disabled).toBe(false)
    await act(async () => { submit.click() })
    expect(onCreate).toHaveBeenCalledWith('Customer portal', 'Build and maintain the portal.', '🚀', undefined)

    await act(async () => { root.unmount() })
  })

  it('previews the Finance Analyst template and submits its identity', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onCreate = vi.fn()
    await act(async () => {
      root.render(<CreateWorkProjectDialog onClose={() => {}} onCreate={onCreate} submitting={false} error={null} />)
    })

    await act(async () => {
      (container!.querySelector('[data-testid="work-template-finance-analyst"]') as HTMLInputElement).click()
    })
    expect((container.querySelector('[data-testid="work-create-project-name-input"]') as HTMLInputElement).value).toBe('Finance Analyst')
    expect(container.textContent).toContain('Connections, schedules, triggers, functions, and Automations are not activated.')
    await act(async () => { (container!.querySelector('[data-testid="work-create-project-submit"]') as HTMLButtonElement).click() })
    expect(onCreate).toHaveBeenCalledWith(
      'Finance Analyst',
      expect.stringContaining('Analyze authorized finance records'),
      '📊',
      'finance-analyst',
    )
    await act(async () => { root.unmount() })
  })

  it('keeps a large catalog browsable without losing the selected template', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const templates = Array.from({ length: 100 }, (_, index) => ({
      ...crewTemplates[0],
      id: `catalog-${index}` as CrewTemplateId,
      name: `Template ${String(index).padStart(3, '0')}`,
      category: index < 40 ? 'Finance' : index < 65 ? 'Operations' : index < 80 ? 'Sales' : index < 90 ? 'Marketing' : index < 95 ? 'Customer Support' : 'Engineering',
    }))

    await act(async () => {
      root.render(<CreateWorkProjectDialog onClose={() => {}} onCreate={() => {}} submitting={false} error={null} templates={templates} />)
    })
    expect(container.textContent).toContain('100 available')
    expect(container.querySelectorAll('[data-testid^="work-template-"]')).toHaveLength(12)

    await act(async () => {
      (Array.from(container!.querySelectorAll('button')).find(button => button.textContent?.startsWith('Show more')) as HTMLButtonElement).click()
    })
    expect(container.querySelectorAll('[data-testid^="work-template-"]')).toHaveLength(24)

    await act(async () => {
      const category = container!.querySelector('[aria-label="Filter template category"]') as HTMLSelectElement
      category.value = 'Finance'
      category.dispatchEvent(new Event('change', { bubbles: true }))
    })
    expect(container.textContent).toContain('40 results')
    expect(container.querySelectorAll('[data-testid^="work-template-"]')).toHaveLength(12)

    await act(async () => {
      (container!.querySelector('[data-testid="work-template-catalog-0"]') as HTMLInputElement).click()
    })
    const search = container.querySelector('[aria-label="Search Crew templates"]') as HTMLInputElement
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(search, 'nothing matches this')
      search.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.textContent).toContain('No results')
    expect(container.textContent).toContain('Selected: Template 000')
    expect((container.querySelector('[data-testid="work-create-project-name-input"]') as HTMLInputElement).value).toBe('Template 000')

    await act(async () => { root.unmount() })
  })
})
