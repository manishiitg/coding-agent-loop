import React, { useState } from 'react'
import { ScrollText, GitCommitVertical } from 'lucide-react'
import { LogViewer } from './LogViewer'
import { PlanChangelogFeed } from './PlanChangelogFeed'

interface PulseViewProps {
  workspacePath: string
}

type PulseTab = 'timeline' | 'plan-edits'

// PulseView is the workflow's Pulse surface — the durable "what happened & why"
// record the Pulse post-run pass writes to. It hosts two complementary lenses:
//   • Timeline   — the authored builder/improve.html narrative (verdicts, applied
//                  fixes, backup commits, monitor notes), rendered by LogViewer.
//   • Plan edits — the granular planning/changelog audit trail with per-field
//                  diffs, rendered by PlanChangelogFeed.
export function PulseView({ workspacePath }: PulseViewProps) {
  const [tab, setTab] = useState<PulseTab>('timeline')

  const tabCls = (active: boolean) =>
    `inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
      active ? 'bg-muted text-foreground' : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
    }`

  return (
    <div className="flex h-full w-full flex-col">
      <div className="flex items-center gap-1 border-b border-border bg-background px-3 py-1.5">
        <button type="button" onClick={() => setTab('timeline')} className={tabCls(tab === 'timeline')}>
          <ScrollText className="h-3.5 w-3.5" /> Timeline
        </button>
        <button type="button" onClick={() => setTab('plan-edits')} className={tabCls(tab === 'plan-edits')}>
          <GitCommitVertical className="h-3.5 w-3.5" /> Plan edits
        </button>
      </div>
      <div className="min-h-0 flex-1">
        {tab === 'timeline'
          ? <LogViewer workspacePath={workspacePath} />
          : <PlanChangelogFeed workspacePath={workspacePath} />}
      </div>
    </div>
  )
}

export default PulseView
