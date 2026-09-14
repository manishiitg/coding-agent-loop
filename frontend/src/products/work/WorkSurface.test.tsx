// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CreateWorkProjectDialog } from './CreateWorkProjectDialog'

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
    expect(submit.disabled).toBe(true)

    const name = container.querySelector('[data-testid="work-create-project-name-input"]') as HTMLInputElement
    const description = container.querySelector('textarea') as HTMLTextAreaElement
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(name, '  Customer portal  ')
      name.dispatchEvent(new Event('input', { bubbles: true }))
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(description, '  Build and maintain the portal.  ')
      description.dispatchEvent(new Event('input', { bubbles: true }))
    })

    expect(submit.disabled).toBe(false)
    await act(async () => { submit.click() })
    expect(onCreate).toHaveBeenCalledWith('Customer portal', 'Build and maintain the portal.')

    await act(async () => { root.unmount() })
  })
})
