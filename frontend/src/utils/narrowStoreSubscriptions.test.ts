import { readFileSync } from 'node:fs'
import ts from 'typescript'
import { describe, expect, it } from 'vitest'

const CRITICAL_COMPONENTS = [
  'src/App.tsx',
  'src/components/ChatInput.tsx',
  'src/components/ChatTabs.tsx',
  'src/components/ModePresetBar.tsx',
  'src/components/Workspace.tsx',
  'src/components/workflow/WorkflowLayout.tsx',
]

const PROTECTED_STORES = new Set([
  'useAppStore',
  'useChatStore',
  'useGlobalPresetStore',
  'useModeStore',
  'useWorkspaceStore',
])

function bareStoreSubscriptions(filepath: string): string[] {
  const sourceText = readFileSync(filepath, 'utf8')
  const source = ts.createSourceFile(filepath, sourceText, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  const findings: string[] = []

  function visit(node: ts.Node): void {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      PROTECTED_STORES.has(node.expression.text) &&
      node.arguments.length === 0
    ) {
      const position = source.getLineAndCharacterOfPosition(node.getStart(source))
      findings.push(`${filepath}:${position.line + 1} ${node.expression.text}()`)
    }
    ts.forEachChild(node, visit)
  }

  visit(source)
  return findings
}

describe('critical frontend store subscriptions', () => {
  it('never subscribes performance-critical roots to an entire Zustand store', () => {
    const findings = CRITICAL_COMPONENTS.flatMap(bareStoreSubscriptions)
    expect(findings, findings.join('\n')).toEqual([])
  })
})
