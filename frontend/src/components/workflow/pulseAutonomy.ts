import type { PulseAutonomy } from '../../services/api-types'

// One slider over the three stored permissions. Each stop adds one kind of
// action Pulse may take on its own; everything else it prepares and asks.
export const AUTONOMY_LEVELS: Array<{ label: string; summary: string; value: PulseAutonomy }> = [
  { label: 'Ask first', summary: 'Prepares work and asks before running anything.', value: { run: 'ask', outward: 'ask', change: 'ask' } },
  { label: 'Run steps', summary: 'Runs your workflow steps on its own. Asks before new posts or workflow edits.', value: { run: 'auto', outward: 'ask', change: 'ask' } },
  { label: 'Edit workflow', summary: 'Also edits steps and schedules. Asks before new posts or messages.', value: { run: 'auto', outward: 'ask', change: 'auto' } },
  { label: 'Full', summary: 'Also posts, sends and reaches out on its own, within your rules.', value: { run: 'auto', outward: 'auto', change: 'auto' } },
]

// autonomyLevelIndex maps stored permissions to the highest stop they fully allow.
export function autonomyLevelIndex(autonomy: PulseAutonomy): number {
  let index = 0
  AUTONOMY_LEVELS.forEach((level, i) => {
    const allowed = (Object.keys(level.value) as Array<keyof PulseAutonomy>).every(key => level.value[key] === 'ask' || autonomy[key] === 'auto')
    if (allowed) index = i
  })
  return index
}
