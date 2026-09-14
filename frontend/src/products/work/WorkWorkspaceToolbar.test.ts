import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('WorkWorkspaceToolbar', () => {
  it('keeps Views and Setup mutually exclusive and follows the selected panel', () => {
    const source = readFileSync('src/products/work/WorkWorkspacePane.tsx', 'utf8')

    expect(source).toContain("open={openGroup === 'views'}")
    expect(source).toContain("open={openGroup === 'setup'}")
    expect(source).toContain("onToggle={() => setOpenGroup('views')}")
    expect(source).toContain("onToggle={() => setOpenGroup('setup')}")
    expect(source).toContain("setOpenGroup(SETUP_BUTTONS.some(item => item.id === view) ? 'setup' : 'views')")
  })
})
