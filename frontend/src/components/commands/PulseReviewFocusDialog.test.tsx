// @vitest-environment happy-dom
import React, { act, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PulseReviewFocusDialog } from './PulseReviewFocusDialog'
import CommandSelectionDialog from '../CommandSelectionDialog'
import { findCommand } from '../../commands/registry'
import type { CommandContext } from '../../commands/types'

vi.mock('../../commands/user-commands', () => ({ loadAndRegisterUserCommands: vi.fn().mockResolvedValue(undefined) }))

describe('Pulse review focus picker', () => {
  let host: HTMLDivElement
  let root: Root
  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    host = document.createElement('div')
    document.body.append(host)
    root = createRoot(host)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    host.remove()
  })

  const form = () => document.querySelector<HTMLFormElement>('[role="dialog"]')!
  const select = () => form().querySelector<HTMLSelectElement>('select')!
  const button = (label: string) => [...form().querySelectorAll('button')].find(node => node.textContent === label)!

  it('offers automatic selection and all nine focuses without starting work on open', async () => {
    const onStart = vi.fn()
    await act(async () => root.render(<PulseReviewFocusDialog initialContext="a specific concern" onClose={vi.fn()} onStart={onStart} />))
    expect(select().options).toHaveLength(10)
    expect(select().value).toBe('auto')
    expect(document.activeElement).toBe(select())
    expect(form().querySelector('textarea')!.value).toBe('a specific concern')
    expect(onStart).not.toHaveBeenCalled()
    await act(async () => form().dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
    await act(async () => form().dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
    expect(onStart).toHaveBeenCalledExactlyOnceWith('auto', 'a specific concern')
  })

  it('submits a chosen prompt review with context and its specialist review/fix instructions', async () => {
    const onSubmit = vi.fn()
    const onStart = (focusId: string, context: string) => findCommand('pulse-review', 'workflow', 'workshop')!.execute({
      beforeSlash: context, pulseReviewFocus: focusId, onSubmit, workshopMode: 'workshop',
      getWorkflowStore: () => ({ selectedRunFolder: 'iteration-8/default' }),
    } as unknown as CommandContext)
    await act(async () => root.render(<PulseReviewFocusDialog initialContext="check the report step" onClose={vi.fn()} onStart={onStart} />))
    await act(async () => {
      select().value = 'prompts'
      select().dispatchEvent(new Event('change', { bubbles: true }))
    })
    expect(form().textContent).toContain('Step instructions, clarity, duplication')
    await act(async () => form().dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
    expect(onSubmit).toHaveBeenCalledTimes(1)
    const prompt = onSubmit.mock.calls[0][0] as string
    expect(prompt).toContain('get_plan_prompt_health')
    expect(prompt).toContain('references/step-description.md')
    expect(prompt).toContain('check the report step')
    expect(prompt).toContain('iteration-8/default')
    expect(prompt).toContain('message_sequence=')
  })

  it('cancels without submitting and keeps keyboard focus inside the dialog', async () => {
    const onStart = vi.fn()
    const onClose = vi.fn()
    function Harness() {
      const [open, setOpen] = useState(true)
      return open ? <PulseReviewFocusDialog initialContext="draft to preserve" onStart={onStart} onClose={() => { onClose(); setOpen(false) }} /> : null
    }
    await act(async () => root.render(<Harness />))
    const last = button('Start review and fixes')
    last.focus()
    await act(async () => last.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })))
    expect(document.activeElement).toBe(form().querySelector('button'))
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(onStart).not.toHaveBeenCalled()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })

  it('finds the consolidated picker by focus and preserves exact legacy keyboard shortcuts', async () => {
    const onSelect = vi.fn()
    const render = (search: string, canWriteWorkflow = true) => root.render(<CommandSelectionDialog isOpen onClose={vi.fn()}
      onSelectCommand={onSelect} searchQuery={search} position={{ bottom: 0, left: 0 }} modeCategory="workflow"
      workshopMode="workshop" canWriteWorkflow={canWriteWorkflow} />)
    await act(async () => render('database'))
    expect(host.textContent).toContain('/pulse-review')
    expect(host.textContent).not.toContain('/pulse-review-database')
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))
    expect(onSelect).toHaveBeenLastCalledWith('pulse-review')
    await act(async () => render('pulse-review-database'))
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))
    expect(onSelect).toHaveBeenLastCalledWith('pulse-review-database')
    onSelect.mockClear()
    await act(async () => render('pulse-review-database', false))
    await act(async () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })))
    expect(onSelect).not.toHaveBeenCalled()
  })
})
