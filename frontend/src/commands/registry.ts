import type { ModeCategory } from '../stores/useModeStore'
import type { CommandDefinition, WorkshopMode } from './types'
import { builtinCommands } from './builtin-commands'

let userCommands: CommandDefinition[] = []
let productCommands: CommandDefinition[] = []

function matchesMode(cmd: CommandDefinition, mode?: ModeCategory, workshopMode?: WorkshopMode, canWriteWorkflow = true): boolean {
  if (cmd.hidden) return false
  if (mode === undefined || mode === null) return true

  if (mode === 'workflow') {
    if (!(cmd.modes?.includes('workflow') ?? false)) return false
    // Readers remain constrained to Run even if a restored tab says Builder.
    // A slash command must not silently escape the current chat's tool policy.
    const permittedMode = canWriteWorkflow ? workshopMode : 'run'
    if (permittedMode && cmd.requiredWorkshopMode) {
      const allowed = Array.isArray(cmd.requiredWorkshopMode)
        ? cmd.requiredWorkshopMode
        : [cmd.requiredWorkshopMode]
      return allowed.includes(permittedMode)
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

// Registered when a product's profile loads. Cleared by passing an empty list
// so switching products cannot leave the previous product's commands in the
// menu, offering flows the current agent has no skills for.
export function setProductCommands(cmds: CommandDefinition[]) {
  productCommands = cmds
}

export function getCommands(mode?: ModeCategory, workshopMode?: WorkshopMode, canWriteWorkflow = true): CommandDefinition[] {
  return [...productCommands, ...builtinCommands, ...userCommands].filter(cmd => !cmd.menuHidden && matchesMode(cmd, mode, workshopMode, canWriteWorkflow))
}

export function findCommand(name: string, mode?: ModeCategory, workshopMode?: WorkshopMode, canWriteWorkflow = true): CommandDefinition | undefined {
  return [...productCommands, ...builtinCommands, ...userCommands].find(cmd =>
    matchesName(cmd, name) && matchesMode(cmd, mode, workshopMode, canWriteWorkflow)
  )
}

function matchesName(cmd: CommandDefinition, name: string): boolean {
  return cmd.command === name || (cmd.aliases?.includes(name) ?? false)
}

export function findCommandAnyMode(name: string): CommandDefinition | undefined {
  return productCommands.find(c => matchesName(c, name))
    ?? builtinCommands.find(c => matchesName(c, name))
    ?? userCommands.find(c => matchesName(c, name))
}
