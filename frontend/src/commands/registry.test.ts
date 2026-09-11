import { describe, expect, it, vi } from 'vitest'
import { findCommand, findCommandAnyMode, getCommands, setUserCommands } from './registry'
import type { CommandContext, CommandDefinition } from './types'
import { pulseReviewFocuses } from './pulse-review-focus'

const { runPulseMock } = vi.hoisted(() => ({ runPulseMock: vi.fn() }))
vi.mock('../api/scheduler', () => ({ schedulerApi: { runPulse: runPulseMock } }))

describe('Pulse slash commands', () => {
  it('uses one setup flow for new and existing goal setup names', () => {
    const setup = findCommand('setup-goals', 'workflow', 'workshop')
    expect(setup).toBeDefined()
    expect(findCommand('define-success', 'workflow', 'workshop')).toBe(setup)
    expect(findCommand('setup-goals', 'multi-agent')).toBeUndefined()
    const onSubmit = vi.fn()
    setup?.execute({ beforeSlash: 'Keep my existing targets', onSubmit } as unknown as CommandContext)
    expect(onSubmit).toHaveBeenCalledWith(expect.stringContaining('kind="setup-goals"'))
    expect(onSubmit).toHaveBeenCalledWith(expect.stringContaining('Keep my existing targets'))
  })

  it('runs the complete manual Pulse lifecycle through the scheduler backend', async () => {
    runPulseMock.mockResolvedValueOnce({ run_id: 'manual-pulse-1' })
    const addToast = vi.fn()
    const command = findCommand('pulse', 'workflow')

    await command?.execute({
      beforeSlash: '',
      onSubmit: vi.fn(),
      workshopMode: 'workshop',
      getWorkspaceStore: () => ({ activeFolder: 'Workflow/social-media' }),
      addToast,
    } as unknown as CommandContext)

    expect(runPulseMock).toHaveBeenCalledWith('Workflow/social-media')
    expect(addToast).toHaveBeenCalledWith('Pulse started', 'success')
  })

  it('has no slash command for recurring Pulse setup — it is a toolbar/popup toggle', () => {
    const workflowCommand = findCommand('pulse-setup', 'workflow')
    const orgCommand = findCommand('pulse-setup', 'multi-agent')

    expect(workflowCommand).toBeUndefined()
    expect(orgCommand).toBeUndefined()
  })

  it('exposes manual Pulse modules in workflow workshop mode', () => {
    const workflowCommands = getCommands('workflow', 'workshop').map(command => command.command)
    const orgCommands = getCommands('multi-agent').map(command => command.command)

    for (const command of [
      'pulse', 'pulse-merge', 'pulse-review', 'pulse-fixer', 'strategy-auditor',
    ]) {
      expect(workflowCommands).toContain(command)
      expect(orgCommands).not.toContain(command)
    }
    for (const retiredCommand of ['bug-review', 'review-speed', 'review-cost', 'llm-ops-review', 'ops-review', 'engineering-review', 'specialize-advisors', 'pulse-setup', 'improve-knowledge', 'improve-learnings', 'improve-database', 'improve-report', 'improve-evaluation', 'pulse-review-stores', 'pulse-review-report', 'pulse-review-evaluation', 'pulse-backlog']) {
      expect(workflowCommands).not.toContain(retiredCommand)
    }
  })

  it('consolidates focused menu entries while preserving every shortcut and its access restrictions', () => {
    const menu = getCommands('workflow', 'workshop').map(command => command.command)
    expect(menu).toContain('pulse-review')
    for (const focus of pulseReviewFocuses) {
      expect(menu).not.toContain(focus.legacyCommand)
      expect(findCommand(focus.legacyCommand, 'workflow', 'workshop')).toBeDefined()
      expect(findCommandAnyMode(focus.legacyCommand)).toBeDefined()
      expect(findCommand(focus.legacyCommand, 'workflow', 'run')).toBeUndefined()
      expect(findCommand(focus.legacyCommand, 'workflow', 'workshop', false)).toBeUndefined()
      expect(findCommand(focus.legacyCommand, 'multi-agent')).toBeUndefined()
    }
  })

  it('uses identical specialist instructions for picker selections, typed focuses, and retained shortcuts', () => {
    const submit = (command: string, context: string, selectedFocus?: string) => {
      let result = ''
      findCommand(command, 'workflow', 'workshop')!.execute({
        beforeSlash: context,
        pulseReviewFocus: selectedFocus,
        onSubmit: (message: string) => { result = message },
        workshopMode: 'workshop',
        getWorkflowStore: () => ({ selectedRunFolder: 'iteration-9/default' }),
      } as CommandContext)
      return result
    }
    for (const focus of pulseReviewFocuses) {
      const context = `${focus.id} investigate the newest regression`
      const picked = submit('pulse-review', context, focus.id)
      expect(picked).toBe(submit('pulse-review', context))
      expect(picked).toBe(submit(focus.legacyCommand, context))
      expect(picked).toContain('iteration-9/default')
      const sequenceJSON = picked.match(/, message_sequence=(\[.*?\]), completion_mode=/)?.[1]
      const [fix] = JSON.parse(sequenceJSON!)
      const serializedFocus = fix.message.match(/focus=("(?:\\.|[^"\\])*")/)?.[1]
      const dispatchedFocus = JSON.parse(serializedFocus!)
      expect(dispatchedFocus).toContain(focus.instructions)
      expect(dispatchedFocus).toContain(context)
    }
    expect(submit('pulse-review', 'database concerns, but investigate freely', 'auto')).not.toContain('improve-database')
    expect(submit('pulse-review', 'investigate a custom concern')).toContain('investigate a custom concern')
  })

  it('hides Builder reviews from Run and rejects direct command lookup there', () => {
    const runCommands = getCommands('workflow', 'run').map(command => command.command)

    for (const command of ['pulse', 'pulse-review', 'pulse-fixer', 'strategy-auditor', 'goal-advisor', 'design-plan', 'review-code', 'review-artifact-drift', 'pulse-review-execution-health', 'plan-prompt-bloat', 'pulse-review-validation-contract', 'backup', 'publish', 'notify']) {
      expect(runCommands).not.toContain(command)
      expect(findCommand(command, 'workflow', 'run', true)).toBeUndefined()
      expect(findCommand(command, 'workflow', 'workshop', true)).toBeDefined()
    }
  })

  it('enforces read-only workflow access even when a restored chat says Builder', () => {
    for (const mode of ['run', 'workshop', undefined] as const) {
      const commands = getCommands('workflow', mode, false).map(command => command.command)
      for (const command of getCommands('workflow', 'workshop', true)) {
        expect(commands).not.toContain(command.command)
        expect(findCommand(command.command, 'workflow', mode, false)).toBeUndefined()
      }
    }
  })

  it('keeps commands that explicitly support Run available to readers', () => {
    const runCommand: CommandDefinition = {
      command: 'inspect-output', description: 'Inspect the current output', icon: null,
      modes: ['workflow'], requiredWorkshopMode: 'run', source: 'user', execute: vi.fn(),
    }
    const builderCommand: CommandDefinition = {
      ...runCommand, command: 'edit-output', requiredWorkshopMode: 'workshop',
    }
    setUserCommands([runCommand, builderCommand])
    try {
      expect(getCommands('workflow', 'run', false)).toEqual([runCommand])
      expect(findCommand('inspect-output', 'workflow', 'run', false)).toBe(runCommand)
      expect(findCommand('edit-output', 'workflow', 'run', false)).toBeUndefined()
    } finally {
      setUserCommands([])
    }
  })

  it('makes prompt-bloat review discoverable when searching Pulse commands', () => {
    const promptBloat = findCommand('plan-prompt-bloat', 'workflow')

    expect(promptBloat?.description.toLowerCase()).toContain('pulse review')
  })

  it('runs backlog consolidation through typed Pulse lifecycle tools only', () => {
    const command = findCommand('pulse-merge', 'workflow')
    let submitted = ''
    command?.execute({
      beforeSlash: 'focus on repeated database tool symptoms',
      onSubmit: (message: string) => { submitted = message },
      workshopMode: 'workshop',
    } as CommandContext)

    expect(submitted).toContain('get_pulse_state(view="backlog", detail="compact")')
    expect(submitted).toContain('detail="full" only for the bounded issue_ids')
    expect(submitted).toContain('merge_pulse_issues')
    expect(submitted).toContain('do not edit workflow artifacts')
  })

  it('routes Pulse Review through one retained review and fix task', () => {
    const command = findCommand('pulse-review', 'workflow')
    let submitted = ''

    command?.execute({
      beforeSlash: 'prioritize failed evaluation writes',
      onSubmit: (message: string) => { submitted = message },
      workshopMode: 'workshop',
      getWorkflowStore: () => ({ selectedRunFolder: 'iteration-9/default' }),
    } as CommandContext)

    expect(submitted).toContain('kind=\\"engineering-review\\"')
    expect(submitted).toContain('Run /pulse-review as a BACKGROUND task')
    expect(submitted).toContain('BACKGROUND task')
    expect(submitted).toContain('completion_mode="present_result"')
		expect(submitted).not.toContain('required_pulse_review_modules')
		expect(submitted).toContain('Do not call tools, reload state, or independently revalidate')
    expect(submitted).toContain('iteration-9/default')
    expect(submitted).toContain('prioritize failed evaluation writes')
    const sequenceJSON = submitted.match(/, message_sequence=(\[.*?\]), completion_mode=/)?.[1]
    expect(sequenceJSON).toBeDefined()
    const sequence = JSON.parse(sequenceJSON!)
    expect(sequence).toHaveLength(1)
    expect(sequence[0].id).toBe('fix')
    expect(sequence[0].message).toContain('kind="pulse-fixer"')
    expect(sequence[0].message).toContain('if review failed or is incomplete')
    expect(sequence[0].message).toContain('run_folder="iteration-9/default"')
    expect(sequence[0].message).toContain('prioritize failed evaluation writes')
  })

  it('routes Pulse Fixer to a separate background agent after review', () => {
    const command = findCommand('pulse-fixer', 'workflow')
    let submitted = ''

    command?.execute({
      beforeSlash: 'repair the highest-impact canonical issue',
      onSubmit: (message: string) => { submitted = message },
      workshopMode: 'workshop',
      getWorkflowStore: () => ({ selectedRunFolder: 'iteration-9/default' }),
    } as CommandContext)

    expect(submitted).toContain('kind=\\"pulse-fixer\\"')
    expect(submitted).toContain('BACKGROUND task')
		expect(submitted).toContain('completion_mode="present_result"')
    expect(submitted).toContain('iteration-9/default')
  })

  it('routes a manual technical focus through retained Technical Review and Fix', () => {
    const technical = findCommand('pulse-review-execution-health', 'workflow')
    let submitted = ''

    technical?.execute({
      beforeSlash: 'check the newest retry spike',
      onSubmit: (message: string) => { submitted = message },
      workshopMode: 'workshop',
      getWorkflowStore: () => ({ selectedRunFolder: 'iteration-9/default' }),
    } as CommandContext)
    expect(submitted).toContain('kind=\\"engineering-review\\"')
    expect(submitted).toContain('Manual Pulse review focus: execution_health')
    expect(submitted).not.toContain('required_pulse_review_modules')
		expect(submitted).toContain('bounded Review+Fix')
  })

  it('routes store review aliases through store_integrity review and bounded repair', () => {
    for (const commandName of ['pulse-review-knowledge', 'pulse-review-learnings', 'pulse-review-database']) {
      const command = findCommand(commandName, 'workflow')
      let submitted = ''
      command?.execute({
        beforeSlash: 'repair confirmed ownership drift',
        onSubmit: (message: string) => { submitted = message },
        workshopMode: 'workshop',
        getWorkflowStore: () => ({ selectedRunFolder: 'iteration-4/default' }),
      } as CommandContext)

      expect(submitted).toContain('kind=\\"engineering-review\\"')
      expect(submitted).toContain('Manual Pulse review focus: store_integrity')
		expect(submitted).toContain('bounded Review+Fix')
      expect(submitted).toContain('iteration-4/default')
    }
  })

  it('runs Strategy Auditor as a background guided review anchored to the selected run', () => {
    const command = findCommand('strategy-auditor', 'workflow')
    let submitted = ''

    command?.execute({
      beforeSlash: 'focus on repeated targets',
      onSubmit: (message: string) => { submitted = message },
      workshopMode: 'workshop',
      getWorkflowStore: () => ({ selectedRunFolder: 'iteration-7/group-a' }),
    } as CommandContext)

    expect(submitted).toContain('Run the /strategy-auditor review as a BACKGROUND task')
    expect(submitted).toContain('kind=\\"strategy-auditor\\"')
    expect(submitted).not.toContain('required_pulse_review_modules')
    expect(submitted).toContain('iteration-7/group-a')
    expect(submitted).toContain('focus on repeated targets')
    expect(submitted).toContain('Needs your decision proposals')
    expect(submitted).not.toContain('message_sequence=')
    expect(submitted).not.toContain('in severity order')
  })

  it('keeps goal-advisor as a menu-free alias of the same strategic review', () => {
    const command = findCommand('goal-advisor', 'workflow', 'workshop')
    const canonical = findCommand('strategy-auditor', 'workflow', 'workshop')
    expect(command).toBe(canonical)
    expect(findCommandAnyMode('goal-advisor')).toBe(canonical)
    expect(getCommands().map(cmd => cmd.command)).not.toContain('goal-advisor')
    expect(findCommand('goal-advisor', 'multi-agent')).toBeUndefined()
    for (const mode of ['run', 'workshop', undefined] as const) {
      expect(findCommand('goal-advisor', 'workflow', mode, false)).toBeUndefined()
    }
    let submitted = ''

    command?.execute({
      beforeSlash: 'challenge feed concentration',
      onSubmit: (message: string) => { submitted = message },
      workshopMode: 'workshop',
      getWorkflowStore: () => ({ selectedRunFolder: 'iteration-7/group-a' }),
    } as CommandContext)

    expect(submitted).toContain('get_workflow_command_guidance')
    expect(submitted).toContain('Run the /strategy-auditor review as a BACKGROUND task')
    expect(submitted).toContain('iteration-7/group-a')
    expect(submitted).not.toContain('goal-advisor')
    expect(submitted).toContain('challenge feed concentration')
    expect(submitted).toContain('BACKGROUND task')
    expect(submitted).toContain('run_in_background')
    expect(submitted).toContain('Needs your decision proposals')
    expect(submitted).not.toContain('message_sequence=')
  })

  it('uses design-plan as the single comprehensive plan review command', () => {
    const workshopCommands = getCommands('workflow', 'workshop').map(command => command.command)
    const runCommands = getCommands('workflow', 'run').map(command => command.command)

    expect(workshopCommands).toContain('design-plan')
    expect(runCommands).not.toContain('design-plan')
    expect(findCommand('design-plan', 'workflow')?.requiredWorkshopMode).toBe('workshop')
    expect(workshopCommands).not.toContain('review-plan')
    expect(runCommands).not.toContain('review-plan')
  })

  it('keeps design-plan coordination in the main conversation', () => {
    const command = findCommand('design-plan', 'workflow')
    let submitted = ''

    command?.execute({
      beforeSlash: '',
      onSubmit: (message: string) => { submitted = message },
    } as CommandContext)

    expect(submitted).toContain('get_workflow_command_guidance')
    expect(submitted).not.toContain('Run the /design-plan review as a BACKGROUND task')
  })

  it('preserves Plan Drift repair authority in the background wrapper', () => {
    const command = findCommand('review-artifact-drift', 'workflow')
    let submitted = ''

    command?.execute({
      beforeSlash: '',
      onSubmit: (message: string) => { submitted = message },
      workshopMode: 'workshop',
    } as CommandContext)

    expect(submitted).toContain('Part 1 may apply bounded safe compatibility and prompt repairs')
    expect(submitted).toContain('Part 2 remains read-only')
    expect(submitted).not.toContain('do not modify implementation files')
    expect(submitted).toContain('lifecycle outcomes')
  })

  it('routes saved-code review to an existing read-only technical flow', () => {
    let submitted = ''
    findCommand('review-code', 'workflow')?.execute({
      beforeSlash: 'check the report exporter',
      onSubmit: (message: string) => { submitted = message },
      workshopMode: 'workshop',
      getWorkflowStore: () => ({ selectedRunFolder: 'iteration-2/dev' }),
    } as CommandContext)

    expect(submitted).toContain('kind=\\"engineering-review\\"')
    expect(submitted).not.toContain('kind=\\"review-code\\"')
    expect(submitted).toContain('workflow.json.code_layout_version')
    expect(submitted).toContain('do not apply changes in this review')
    expect(submitted).not.toContain('message_sequence=')
    expect(submitted).toContain('check the report exporter')
  })

  it('keeps workflow configuration actions out of the generic chat slash menu', () => {
    const chatCommands = getCommands('multi-agent').map(command => command.command)

    for (const command of ['build-skill', 'add-skill', 'mcp', 'mcp-add', 'models']) {
      expect(chatCommands).not.toContain(command)
    }
  })
})

describe('Product commands are scoped to their own surface', () => {
  it('shows a product\'s own commands once registered, and clears them when unregistered', async () => {
    // Product commands live in product.yaml and are delivered by the owning
    // product surface, then cleared on unmount. A profile-less chat does
    // not inherit those commands.
    const { setProductCommands } = await import('./registry')
    setProductCommands([{
      command: 'production', description: 'Start a video production', icon: null,
      modes: ['multi-agent'], source: 'product', execute: () => {},
    } as unknown as import('./types').CommandDefinition])
    try {
      const commands = getCommands('multi-agent').map(command => command.command)
      expect(commands).toContain('production')
    } finally {
      setProductCommands([])
    }
    expect(getCommands('multi-agent').map(command => command.command)).not.toContain('production')
  })
})
