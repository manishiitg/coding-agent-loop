import type { ModeCategory } from '../stores/useModeStore'
import type { CommandDefinition } from './types'
import { builtinCommands } from './builtin-commands'

let userCommands: CommandDefinition[] = []

function matchesMode(cmd: CommandDefinition, mode?: ModeCategory): boolean {
  if (cmd.hidden) return false
  if (mode === undefined || mode === null) return true

  if (mode === 'workflow') {
    return cmd.modes?.includes('workflow') ?? false
  }

  if (mode === 'multi-agent') {
    return cmd.modes?.includes('multi-agent') ?? cmd.modes === undefined
  }

  return true
}

export function setUserCommands(cmds: CommandDefinition[]) {
  userCommands = cmds
}

export function getCommands(mode?: ModeCategory): CommandDefinition[] {
  return [...builtinCommands, ...userCommands].filter(cmd => matchesMode(cmd, mode))
}

export function findCommand(name: string, mode?: ModeCategory): CommandDefinition | undefined {
  return [...builtinCommands, ...userCommands].find(cmd =>
    cmd.command === name && matchesMode(cmd, mode)
  )
}

export function findCommandAnyMode(name: string): CommandDefinition | undefined {
  return builtinCommands.find(c => c.command === name)
    ?? userCommands.find(c => c.command === name)
}
