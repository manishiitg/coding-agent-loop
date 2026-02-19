import type { ModeCategory } from '../stores/useModeStore'
import type { CommandDefinition } from './types'
import { builtinCommands } from './builtin-commands'

let userCommands: CommandDefinition[] = []

export function setUserCommands(cmds: CommandDefinition[]) {
  userCommands = cmds
}

export function getCommands(mode?: ModeCategory): CommandDefinition[] {
  return [...builtinCommands, ...userCommands].filter(cmd => {
    if (cmd.hidden) return false
    if (mode !== undefined && cmd.modes && cmd.modes.length > 0 && !cmd.modes.includes(mode)) return false
    return true
  })
}

export function findCommand(name: string): CommandDefinition | undefined {
  return builtinCommands.find(c => c.command === name)
    ?? userCommands.find(c => c.command === name)
}
