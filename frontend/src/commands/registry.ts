import type { ModeCategory } from '../stores/useModeStore'
import type { CommandDefinition, WorkshopMode } from './types'
import { builtinCommands } from './builtin-commands'

let userCommands: CommandDefinition[] = []

function matchesMode(cmd: CommandDefinition, mode?: ModeCategory, workshopMode?: WorkshopMode): boolean {
  if (cmd.hidden) return false
  if (mode === undefined || mode === null) return true

  if (mode === 'workflow') {
    if (!(cmd.modes?.includes('workflow') ?? false)) return false
    // Filter by workshop mode if set
    if (workshopMode && cmd.requiredWorkshopMode) {
      const allowed = Array.isArray(cmd.requiredWorkshopMode)
        ? cmd.requiredWorkshopMode
        : [cmd.requiredWorkshopMode]
      return allowed.includes(workshopMode)
    }
    return true
  }

  if (mode === 'multi-agent') {
    return cmd.modes?.includes('multi-agent') ?? cmd.modes === undefined
  }

  return true
}

export function setUserCommands(cmds: CommandDefinition[]) {
  userCommands = cmds
}

export function getCommands(mode?: ModeCategory, workshopMode?: WorkshopMode): CommandDefinition[] {
  return [...builtinCommands, ...userCommands].filter(cmd => matchesMode(cmd, mode, workshopMode))
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
