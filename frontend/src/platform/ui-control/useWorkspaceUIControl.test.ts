import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { UI_CONTROL_BACKUP_POLL_MS } from './useWorkspaceUIControl'

describe('workspace UI control transport', () => {
  it('uses SSE and state changes as primary wake-ups with a ten-second backup poll', () => {
    expect(UI_CONTROL_BACKUP_POLL_MS).toBe(10_000)

    const source = readFileSync('src/platform/ui-control/useWorkspaceUIControl.ts', 'utf8')
    expect(source).toContain('useEffect(() => { if (latestAction) wake.current?.() }, [latestAction])')
    expect(source).toContain('useEffect(() => { wake.current?.() }, [adapter])')
    expect(source).toContain('useWorkflowStore.subscribe')
    expect(source).toContain("setInterval(() => { void sync('poll') }, UI_CONTROL_BACKUP_POLL_MS)")
    expect(source).not.toContain('setInterval(() => { void sync() }, 3000)')
  })

  it('queues an SSE or state wake-up that arrives during an active sync', () => {
    const source = readFileSync('src/platform/ui-control/useWorkspaceUIControl.ts', 'utf8')
    expect(source).toContain('syncQueued = true')
    expect(source).toContain('if (syncQueued && !stopped)')
    expect(source).toContain('queueMicrotask(() => { void sync(next) })')
  })
})
