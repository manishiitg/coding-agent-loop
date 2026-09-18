import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Work Dashboard', () => {
  it('reuses the shared dashboard and database views with project-scoped data', () => {
    const source = readFileSync('src/products/work/WorkWorkspacePane.tsx', 'utf8')

    expect(source).toContain("{ id: 'dashboard', label: 'Dashboard'")
    expect(source).toContain("{ id: 'database', label: 'Database'")
    expect(source).toContain('<ReportDocumentSwitcher workspacePath={workspacePath}')
    expect(source).not.toContain('documentPath="db/reports/index.html"')
    expect(source).toContain("view === 'database'")
    expect(source).toContain('sendChatMessage={async (message)')
    expect(source).toContain("From this project's dashboard:")
    expect(source).toContain('emptyIdentity={{ icon: projectIdentity?.icon, name: projectIdentity?.name || projectTitle, projectName: projectTitle }}')
    expect(source).toContain('selectedGlobalSecrets={selectedGlobalSecrets}')
    expect(source).toContain('persistExplicitGlobalSelection')
    expect(source).toContain('allowGlobalPromotion')
  })
})
