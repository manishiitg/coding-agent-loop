import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('WorkflowLayout focus pane', () => {
  it("keeps the canvas pane flex when focusedPane is 'chat'", () => {
    // Right-pane sends (Ask AI) set focusedPane='chat'. The canvas pane div
    // also carries the host's `flex flex-col`, and its scroll region depends
    // on flex-1. Restoring visibility at md+ with md:block would override
    // flex (responsive variants win over base utilities), collapse the
    // content to its own height, clip the overflow, and freeze right-pane
    // scrolling until the focus flips back to 'preview'.
    const layout = readFileSync('src/components/workflow/workspaceLayoutResolver.ts', 'utf8')
    const branch = layout.split('\n').find(line => line.includes("focusedPane === 'chat'") && line.includes('hidden md:'))
    expect(branch).toBeDefined()
    expect(branch).toContain('hidden md:flex')
    expect(branch).not.toContain('md:block')
  })
})
