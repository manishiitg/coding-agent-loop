import { describe, expect, it, vi } from 'vitest'
import {
  executeAgentworksProductCommand,
  parseAgentworksProductCommands,
  toAgentworksCommandDefinitions,
  type AgentworksProductCommand,
} from './agentworksProductCommands'
import type { CommandContext } from './types'

function command(overrides: Partial<AgentworksProductCommand> = {}): AgentworksProductCommand {
  return {
    name: 'design-plan',
    description: 'Review the plan',
    icon: 'git-branch',
    aliases: [],
    menuHidden: false,
    prompt: 'Call get_workflow_command_guidance(kind="design-plan", focus="{{context}}").',
    ...overrides,
  }
}

function context(overrides: Partial<CommandContext> = {}): CommandContext {
  return { beforeSlash: '', onSubmit: vi.fn(), ...overrides } as unknown as CommandContext
}

describe('parseAgentworksProductCommands', () => {
  it('reads aliases and menu_hidden from the profile response', () => {
    const parsed = parseAgentworksProductCommands({
      commands: [
        { name: 'setup-goals', description: 'Goals', icon: 'target', aliases: ['define-success'], prompt: 'do {{context}}' },
        { name: 'pulse-review-database', description: 'DB', menu_hidden: true, prompt: 'review {{context}}' },
        { name: 'nameless', description: 'no prompt' },
      ],
    })
    expect(parsed).toHaveLength(2)
    expect(parsed[0]).toMatchObject({ name: 'setup-goals', aliases: ['define-success'], menuHidden: false })
    expect(parsed[1]).toMatchObject({ name: 'pulse-review-database', aliases: [], menuHidden: true })
  })

  it('falls back to the terminal icon for unknown names', () => {
    const [definition] = toAgentworksCommandDefinitions([command({ icon: 'not-a-real-icon' })])
    expect(definition.icon).toBeDefined()
  })
})

describe('toAgentworksCommandDefinitions', () => {
  it('owns the workflow-mode semantics every builder command shares', () => {
    const [definition] = toAgentworksCommandDefinitions([command()])
    expect(definition.modes).toEqual(['workflow'])
    expect(definition.requiredWorkflowMode).toBe('plan')
    expect(definition.requiredWorkshopMode).toBe('workshop')
    expect(definition.source).toBe('product')
    expect(definition.menuHidden).toBeUndefined()
  })

  it('keeps aliases and menu hiding without adding menu entries', () => {
    const [aliased, hidden] = toAgentworksCommandDefinitions([
      command({ name: 'setup-goals', aliases: ['define-success'] }),
      command({ name: 'pulse-review-database', menuHidden: true }),
    ])
    expect(aliased.aliases).toEqual(['define-success'])
    expect(hidden.menuHidden).toBe(true)
  })

  it('attaches focus search terms only to pulse-review', () => {
    const [review, other] = toAgentworksCommandDefinitions([
      command({ name: 'pulse-review' }),
      command({ name: 'design-plan' }),
    ])
    expect(review.searchTerms).toContain('pulse-review-database')
    expect(other.searchTerms).toBeUndefined()
  })
})

describe('executeAgentworksProductCommand', () => {
  it('substitutes typed context and trims the submitted prompt', () => {
    const onSubmit = vi.fn()
    executeAgentworksProductCommand(
      command({ prompt: '{{context}}\n\nHelp me with backup.' }),
      undefined,
      context({ beforeSlash: 'nightly runs', onSubmit }),
    )
    expect(onSubmit).toHaveBeenCalledWith('nightly runs\n\nHelp me with backup.')
  })

  it('removes the placeholder and suppresses empty submissions', () => {
    const onSubmit = vi.fn()
    executeAgentworksProductCommand(
      command({ prompt: '  {{context}}  ' }),
      undefined,
      context({ beforeSlash: '', onSubmit }),
    )
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('joins a fixed legacy focus ahead of typed context', () => {
    const onSubmit = vi.fn()
    executeAgentworksProductCommand(
      command({ name: 'pulse-review-database', prompt: 'Review focus={{context}}.' }),
      'Manual Pulse review focus: store_integrity.',
      context({ beforeSlash: 'check the newest spike', onSubmit }),
    )
    expect(onSubmit).toHaveBeenCalledWith(
      'Review focus=Manual Pulse review focus: store_integrity.\n\ncheck the newest spike.',
    )
  })

  it('resolves pulse-review focus from picker selection or first word', () => {
    const prompt = command({ name: 'pulse-review', prompt: 'Review focus={{context}}.' })
    const submit = (beforeSlash: string, pulseReviewFocus?: string) => {
      const onSubmit = vi.fn()
      executeAgentworksProductCommand(prompt, undefined, context({ beforeSlash, pulseReviewFocus, onSubmit }))
      return onSubmit.mock.calls[0][0] as string
    }
    const picked = submit('execution investigate the newest regression', 'execution')
    expect(picked).toBe(submit('execution investigate the newest regression'))
    expect(picked).toContain('Manual Pulse review focus: execution_health')
    expect(picked).toContain('execution investigate the newest regression')
    expect(submit('investigate a custom concern')).toContain('investigate a custom concern')
  })
})
