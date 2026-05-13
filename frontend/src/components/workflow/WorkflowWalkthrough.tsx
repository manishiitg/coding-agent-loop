import React, { useCallback, useEffect, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, X } from 'lucide-react'
import ModalPortal from '../ui/ModalPortal'

type WalkthroughStep = {
  selector: string
  title: string
  body: string
}

const STEPS: WalkthroughStep[] = [
  {
    selector: '[data-tour="top-mode-switcher"]',
    title: 'Top modes',
    body: 'Use these to move between Workflow, Chat, and Org views. Keyboard shortcuts are Ctrl+1, Ctrl+2, and Ctrl+3.',
  },
  {
    selector: '[data-tour="workflow-add-edit"]',
    title: 'Add or edit workflows',
    body: 'Click the workflow name to switch or add a workflow. Use the gear beside it to edit the selected workflow.',
  },
  {
    selector: '[data-tour="workflow-schedules"]',
    title: 'Schedules',
    body: 'Open scheduled workflow runs from here. It shows how many schedules exist and whether any are running now.',
  },
  {
    selector: '[data-tour="bot-connector"]',
    title: 'Bot connector',
    body: 'Use this to connect a workflow to bot channels like WhatsApp or Slack, and to test bot-driven runs.',
  },
  {
    selector: '[data-tour="workspace-open"]',
    title: 'Workspace files',
    body: 'Open the workspace to inspect files, reports, run outputs, and workflow artifacts. Ctrl+6 toggles it.',
  },
  {
    selector: '[data-tour="chat-input-area"]',
    title: 'Chat input',
    body: 'This is the main control surface in Chat mode. Type a request, attach context, choose tools, then send or queue the message.',
  },
  {
    selector: '[data-tour="chat-input-box"]',
    title: 'Prompt box',
    body: 'Use @ for files, / for commands, # for workflows, ! for skills, and $ for MCP servers.',
  },
  {
    selector: '[data-tour="chat-input-tools"]',
    title: 'Chat tools',
    body: 'Pick servers, skills, browser access, Google Workspace, secrets, and other tool access before sending.',
  },
  {
    selector: '[data-tour="chat-browser-tools"]',
    title: 'Browser access',
    body: 'Enable headless browser, Chrome CDP, or Playwright when the agent needs to inspect or operate web pages.',
  },
  {
    selector: '[data-tour="chat-gws-tools"]',
    title: 'Google Workspace',
    body: 'Enable Google Workspace when the agent needs Drive, Gmail, Calendar, Docs, Sheets, or Slides access.',
  },
  {
    selector: '[data-tour="chat-active-agents"]',
    title: 'Active agents',
    body: 'When background work is running, this shows how many agents are active and lets you inspect the running agent tree.',
  },
  {
    selector: '[data-tour="chat-send-controls"]',
    title: 'Send and attachments',
    body: 'Upload files with the paperclip. Send starts a turn; while a turn is running, new messages are queued or can be steered into the active agent.',
  },
  {
    selector: '[data-tour="workflow-view-switcher"]',
    title: 'Workflow views',
    body: 'Chat is the main place to build and change a workflow. Plan shows the step structure. Report shows the output view.',
  },
  {
    selector: '[data-tour="workflow-chat-pane"]',
    title: 'Chat',
    body: 'Use chat for builder and optimizer work. You can type normally, use slash commands, and continue a running workflow conversation.',
  },
  {
    selector: '[data-tour="workflow-canvas-pane"]',
    title: 'Plan and report',
    body: 'This area shows the plan graph or report preview, so you can inspect the workflow without leaving chat.',
  },
  {
    selector: '[data-tour="workflow-status"]',
    title: 'Status',
    body: 'This tells you whether the current workflow session is idle, busy, stopped, or waiting. Stop only affects this active session.',
  },
  {
    selector: '[data-tour="active-work-switcher"]',
    title: 'Running work',
    body: 'The top activity widget opens active sessions. Ctrl+K opens the full switcher for workflows, chats, active work, and retained events.',
  },
  {
    selector: '[data-tour="workflow-tools"]',
    title: 'Workflow tools',
    body: 'These icons open workflow details such as logs, costs, learnings, database sources, versions, access, and settings.',
  },
  {
    selector: '[data-tour="left-sidebar"]',
    title: 'Left menu',
    body: 'The left menu holds global setup for the app: model configuration, MCP servers, skills, and secrets.',
  },
  {
    selector: '[data-tour="sidebar-llm-settings"]',
    title: 'Model settings',
    body: 'Configure the default LLMs and delegation tiers used by chat, workflow builder, and background agents.',
  },
  {
    selector: '[data-tour="sidebar-mcp-servers"]',
    title: 'MCP servers',
    body: 'Manage connected tool servers. These are the external tools agents can use when a chat or workflow enables them.',
  },
  {
    selector: '[data-tour="sidebar-skills"]',
    title: 'Skills',
    body: 'Skills are reusable instructions that guide agents for specific domains, workflows, or tools.',
  },
  {
    selector: '[data-tour="sidebar-secrets"]',
    title: 'Secrets',
    body: 'Store credentials here, then select only the secrets a chat or workflow should be allowed to use.',
  },
]

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max)
const visibleTargetForSelector = (selector: string): Element | null => {
  const candidates = Array.from(document.querySelectorAll(selector))
  return candidates.find(element => {
    const rect = element.getBoundingClientRect()
    const styles = window.getComputedStyle(element)
    return rect.width > 0 &&
      rect.height > 0 &&
      styles.display !== 'none' &&
      styles.visibility !== 'hidden' &&
      styles.opacity !== '0'
  }) ?? null
}
const visibleStepIndices = () => STEPS
  .map((step, index) => visibleTargetForSelector(step.selector) ? index : -1)
  .filter(index => index >= 0)

interface WorkflowWalkthroughProps {
  isOpen: boolean
  onClose: () => void
  openToken?: number
}

export const WorkflowWalkthrough: React.FC<WorkflowWalkthroughProps> = ({ isOpen, onClose, openToken = 0 }) => {
  const [stepIndex, setStepIndex] = useState(0)
  const [targetRect, setTargetRect] = useState<DOMRect | null>(null)
  const wasOpenRef = useRef(false)

  const findStep = useCallback((startIndex: number, direction: 1 | -1) => {
    if (direction === 1) {
      for (let index = Math.max(0, startIndex); index < STEPS.length; index += 1) {
        if (visibleTargetForSelector(STEPS[index].selector)) return index
      }
      return -1
    }
    for (let index = Math.min(STEPS.length - 1, startIndex); index >= 0; index -= 1) {
      if (visibleTargetForSelector(STEPS[index].selector)) {
        return index
      }
    }
    return -1
  }, [])

  const goToStep = useCallback((direction: 1 | -1) => {
    const nextIndex = findStep(stepIndex + direction, direction)
    if (nextIndex >= 0) setStepIndex(nextIndex)
  }, [findStep, stepIndex])

  useEffect(() => {
    if (isOpen && !wasOpenRef.current) {
      const firstIndex = findStep(0, 1)
      if (firstIndex >= 0) setStepIndex(firstIndex)
    }
    wasOpenRef.current = isOpen
  }, [findStep, isOpen])

  useEffect(() => {
    if (!isOpen) return
    const firstIndex = findStep(0, 1)
    if (firstIndex >= 0) setStepIndex(firstIndex)
  }, [findStep, isOpen, openToken])

  const updateTarget = useCallback(() => {
    const target = visibleTargetForSelector(STEPS[stepIndex].selector)
    if (target) {
      setTargetRect(target.getBoundingClientRect())
      return
    }
    const nextIndex = findStep(stepIndex + 1, 1)
    if (nextIndex >= 0 && nextIndex !== stepIndex) {
      setStepIndex(nextIndex)
    } else {
      const previousIndex = findStep(stepIndex - 1, -1)
      if (previousIndex >= 0 && previousIndex !== stepIndex) {
        setStepIndex(previousIndex)
        return
      }
      const firstIndex = findStep(0, 1)
      if (firstIndex >= 0 && firstIndex !== stepIndex) {
        setStepIndex(firstIndex)
        return
      }
      setTargetRect(null)
    }
  }, [findStep, stepIndex])

  useEffect(() => {
    if (!isOpen) return
    if (!visibleTargetForSelector(STEPS[stepIndex].selector)) {
      const firstIndex = findStep(0, 1)
      if (firstIndex >= 0) {
        setStepIndex(firstIndex)
      }
    }
  }, [findStep, isOpen, stepIndex])

  useEffect(() => {
    if (!isOpen) return
    updateTarget()
    window.addEventListener('resize', updateTarget)
    window.addEventListener('scroll', updateTarget, true)
    return () => {
      window.removeEventListener('resize', updateTarget)
      window.removeEventListener('scroll', updateTarget, true)
    }
  }, [isOpen, updateTarget])

  useEffect(() => {
    if (!isOpen) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
      if (event.key === 'ArrowLeft') goToStep(-1)
      if (event.key === 'ArrowRight') goToStep(1)
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [goToStep, isOpen, onClose])

  if (!isOpen) return null

  const step = STEPS[stepIndex]
  const visibleIndices = visibleStepIndices()
  const visiblePosition = visibleIndices.indexOf(stepIndex)
  const stepNumber = visiblePosition >= 0 ? visiblePosition + 1 : stepIndex + 1
  const stepTotal = visibleIndices.length || STEPS.length
  const isFirstVisibleStep = visiblePosition <= 0
  const isLastVisibleStep = visiblePosition >= 0 && visiblePosition === visibleIndices.length - 1
  const panelWidth = Math.min(360, Math.max(288, window.innerWidth - 24))
  const panelHeight = 188
  const panelLeft = targetRect
    ? clamp(targetRect.left, 12, window.innerWidth - panelWidth - 12)
    : clamp((window.innerWidth - panelWidth) / 2, 12, window.innerWidth - panelWidth - 12)
  const panelTop = targetRect
    ? targetRect.bottom + panelHeight + 16 > window.innerHeight
      ? clamp(targetRect.top - panelHeight - 14, 12, window.innerHeight - panelHeight - 12)
      : clamp(targetRect.bottom + 14, 12, window.innerHeight - panelHeight - 12)
    : clamp((window.innerHeight - panelHeight) / 2, 12, window.innerHeight - panelHeight - 12)

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-[10000] pointer-events-none">
        {targetRect && (
          <div
            className="fixed rounded-lg border-2 border-sky-400 transition-all duration-150"
            style={{
              left: targetRect.left - 6,
              top: targetRect.top - 6,
              width: targetRect.width + 12,
              height: targetRect.height + 12,
              boxShadow: '0 0 0 9999px rgba(0, 0, 0, 0.48), 0 0 0 1px rgba(14, 165, 233, 0.35)',
            }}
          />
        )}
        <div
          className="fixed pointer-events-auto rounded-lg border border-border bg-popover p-4 text-popover-foreground shadow-2xl"
          style={{ left: panelLeft, top: panelTop, width: panelWidth }}
          role="dialog"
          aria-modal="true"
          aria-label="Workflow walkthrough"
          data-testid="workflow-walkthrough-dialog"
        >
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">
                Walkthrough {stepNumber} / {stepTotal}
              </div>
              <h3 className="mt-1 text-sm font-semibold text-foreground">{step.title}</h3>
            </div>
            <button
              onClick={onClose}
              className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              aria-label="Close walkthrough"
              data-testid="workflow-walkthrough-close"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <p className="mt-2 text-sm leading-5 text-muted-foreground">{step.body}</p>
          <div className="mt-4 flex items-center justify-end gap-2">
            <button
              onClick={() => goToStep(-1)}
              disabled={isFirstVisibleStep}
              data-testid="workflow-walkthrough-back"
              className="inline-flex items-center gap-1 rounded-md border border-border bg-background px-2.5 py-1.5 text-xs font-medium text-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Back
            </button>
            {isLastVisibleStep ? (
              <button
                onClick={onClose}
                data-testid="workflow-walkthrough-done"
                className="rounded-md bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90"
              >
                Done
              </button>
            ) : (
              <button
                onClick={() => goToStep(1)}
                data-testid="workflow-walkthrough-next"
                className="inline-flex items-center gap-1 rounded-md bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90"
              >
                Next
                <ArrowRight className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}

export default WorkflowWalkthrough
