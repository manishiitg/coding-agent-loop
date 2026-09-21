import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const read = (path: string) => readFileSync(path, 'utf8')

describe('QuickSwitcher open-path performance contract', () => {
  it('is bundled with the shell and paints cached sessions without a forced refresh', () => {
    const app = read('src/App.tsx')
    const switcher = read('src/components/QuickSwitcher.tsx')

    expect(app).toContain("import QuickSwitcher from './components/QuickSwitcher'")
    expect(app).not.toContain("lazy(() => import('./components/QuickSwitcher'))")
    expect(switcher).toContain('getActiveSessions()')
    expect(switcher).not.toContain('getActiveSessions(true)')
    expect(switcher).toContain('}, [isOpen, initialQuery])')
  })
})
