// @vitest-environment happy-dom
import { act } from 'react'
import type { ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { TooltipProvider } from '../ui/tooltip'
import { AskAIButton } from './AskAIButton'
import { sendWorkspacePaneMessageToChat } from '../../utils/workspacePaneChat'

const mocks = vi.hoisted(() => ({
  addToast: vi.fn(),
}))

vi.mock('../../stores/useChatStore', () => ({
  useChatStore: {
    getState: () => ({ addToast: mocks.addToast }),
  },
}))

vi.mock('../../utils/workspacePaneChat', () => ({
  sendWorkspacePaneMessageToChat: vi.fn().mockResolvedValue(undefined),
}))

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })

let container: HTMLDivElement
let root: Root

const render = async (node: ReactNode) => {
  await act(async () => {
    root.render(<TooltipProvider>{node}</TooltipProvider>)
  })
}

beforeEach(() => {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  vi.mocked(sendWorkspacePaneMessageToChat).mockClear()
  mocks.addToast.mockClear()
})

afterEach(() => {
  act(() => {
    root.unmount()
  })
  document.body.innerHTML = ''
})

it('confirms before sending to a custom chat lane', async () => {
  const onAsk = vi.fn().mockResolvedValue(undefined)
  const message = 'Help me configure schedules for this workflow.'

  await render(<AskAIButton workspacePath="Workflow/demo" message={message} onAsk={onAsk} />)

  await act(async () => {
    document.querySelector('button')?.click()
  })

  expect(document.body.textContent).toContain('Ask AI about this view')
  expect(document.body.textContent).not.toContain(message)
  expect(onAsk).not.toHaveBeenCalled()

  await act(async () => {
    Array.from(document.querySelectorAll('button')).find(button => button.textContent?.includes('Send to chat'))?.click()
  })

  expect(onAsk).toHaveBeenCalledWith(message)
  expect(document.body.textContent).not.toContain('Ask AI about this view')
})

it('does not send when the confirmation is closed', async () => {
  const onAsk = vi.fn().mockResolvedValue(undefined)

  await render(<AskAIButton workspacePath="Workflow/demo" message="Preview only" onAsk={onAsk} />)

  await act(async () => {
    document.querySelector('button')?.click()
  })
  await act(async () => {
    document.querySelector('button[aria-label="Close Ask AI confirmation"]')?.click()
  })

  expect(onAsk).not.toHaveBeenCalled()
  expect(document.body.textContent).not.toContain('Preview only')
})

it('sends the previewed message through the default workspace chat path', async () => {
  const message = 'Help me with backup settings.'

  await render(<AskAIButton workspacePath="Workflow/demo" message={message} />)

  await act(async () => {
    document.querySelector('button')?.click()
  })
  await act(async () => {
    Array.from(document.querySelectorAll('button')).find(button => button.textContent?.includes('Send to chat'))?.click()
  })

  expect(sendWorkspacePaneMessageToChat).toHaveBeenCalledWith({
    workspacePath: 'Workflow/demo',
    message,
  })
})
