import { useCallback, useEffect, useState } from 'react'
import { BookOpen } from 'lucide-react'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import type { PlanningResponse } from '../../utils/stepConfigMatching'
import { WorkspaceViewHeader } from './WorkspaceViewHeader'
import { WorkspaceViewActions } from './WorkspaceViewActions'
import { getKnowledgeTabAskAIMessage, type KnowledgeTabId } from './workspaceAskAI'
import LearningsView from './LearningsView'
import KnowledgebaseView from './KnowledgebaseView'
import DatabaseView from './DatabaseView'

const KNOWLEDGE_TABS: Array<{ value: KnowledgeTabId; label: string }> = [
  { value: 'learnings', label: 'Learnings' },
  { value: 'knowledgebase', label: 'Knowledgebase' },
  { value: 'database', label: 'Database' },
]

function isKnowledgeTab(value: unknown): value is KnowledgeTabId {
  return value === 'learnings' || value === 'knowledgebase' || value === 'database'
}

interface KnowledgeViewProps {
  workspacePath: string | null
  plan: PlanningResponse | null
}

/**
 * The Knowledge umbrella: learnings, notes, and stored records under one
 * header with tabs. Tab switches write back through openWorkspaceView (hub
 * pattern) so agent deep-links, refresh, and remounts land on the same tab.
 */
export default function KnowledgeView({ workspacePath, plan }: KnowledgeViewProps) {
  const workspaceViewTarget = useWorkflowStore(state => state.workspaceViewTarget)
  const initialTab = workspaceViewTarget?.view === 'knowledge' && isKnowledgeTab(workspaceViewTarget.target)
    ? workspaceViewTarget.target
    : 'learnings'
  const [tab, setTab] = useState<KnowledgeTabId>(initialTab)

  useEffect(() => {
    if (workspaceViewTarget?.view !== 'knowledge') return
    if (isKnowledgeTab(workspaceViewTarget.target)) setTab(workspaceViewTarget.target)
  }, [workspaceViewTarget])

  const selectTab = useCallback((next: KnowledgeTabId) => {
    setTab(next)
    useWorkflowStore.getState().openWorkspaceView('knowledge', next)
  }, [])

  const tabLabel = KNOWLEDGE_TABS.find(option => option.value === tab)?.label ?? 'Knowledge'

  return (
    <div className="flex h-full min-h-0 w-full max-w-none flex-col bg-background">
      <WorkspaceViewHeader
        icon={BookOpen}
        title="Knowledge"
        subtitle="What this workflow has learned, its notes, and its stored records."
        actions={(
          <WorkspaceViewActions
            workspacePath={workspacePath}
            message={getKnowledgeTabAskAIMessage(tab)}
            onRefresh={() => useWorkflowStore.getState().refreshWorkspaceView(tab)}
            refreshLabel={`Refresh ${tabLabel}`}
          />
        )}
        tabs={{ value: tab, onChange: (value: string) => selectTab(value as KnowledgeTabId), options: KNOWLEDGE_TABS, ariaLabel: 'Knowledge' }}
      />
      <div className="min-h-0 flex-1">
        {tab === 'learnings' && <LearningsView workspacePath={workspacePath} plan={plan} hideHeader />}
        {tab === 'knowledgebase' && <KnowledgebaseView workspacePath={workspacePath} hideHeader />}
        {tab === 'database' && <DatabaseView workspacePath={workspacePath} hideHeader />}
      </div>
    </div>
  )
}
