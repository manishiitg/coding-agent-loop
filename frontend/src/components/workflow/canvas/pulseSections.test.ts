import { describe, expect, it } from 'vitest'
import { PULSE_FIXED_COMMANDS, PULSE_FOOTER_COMMAND_IDS, PULSE_HISTORY_ITEMS, PULSE_MODULE_COMMANDS, PULSE_SECTIONS } from './pulseSections'

describe('Pulse section design', () => {
  it('assigns every module and final command to exactly one section', () => {
    const moduleIds = PULSE_SECTIONS.flatMap(section => section.moduleIds)
    const commandIds = [
      ...PULSE_SECTIONS.flatMap(section => section.commandIds),
      ...PULSE_FOOTER_COMMAND_IDS,
    ]

    expect(new Set(moduleIds).size).toBe(moduleIds.length)
    expect(new Set(commandIds).size).toBe(commandIds.length)
    expect([...moduleIds].sort()).toEqual(PULSE_MODULE_COMMANDS.map(command => command.id).sort())
    expect([...commandIds].sort()).toEqual(PULSE_FIXED_COMMANDS.map(command => command.id).sort())
  })

  it('shows Pulse Fixer history under Improvements without counting it as a scheduled module', () => {
    const historyIds = PULSE_SECTIONS.flatMap(section => section.historyIds)

    expect(historyIds).toEqual(PULSE_HISTORY_ITEMS.map(item => item.id))
    expect(PULSE_SECTIONS.find(section => section.id === 'improvements')?.historyIds).toEqual(['pulse_fixer'])
    expect(PULSE_MODULE_COMMANDS.some(command => command.id === 'pulse_fixer')).toBe(false)
  })

  it('keeps the user-facing order goal, signals, reflection, improvements', () => {
    expect(PULSE_SECTIONS.map(section => section.id)).toEqual([
      'goal',
      'signals',
      'reflection',
      'improvements',
    ])
  })
})
