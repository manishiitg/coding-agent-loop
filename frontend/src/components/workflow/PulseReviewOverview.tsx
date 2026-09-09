import { lazy, Suspense, useEffect, useState } from 'react'
import { FileText, GitCompare, Lightbulb, Loader2, Wrench } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PulseFindingLifecycle, PulseModuleState, PulseReviewAudit, PulseReviewFocus, PulseReviewReport } from '../../services/api-types'
import { normalizePulseWorkspaceModule } from './pulseWorkspaceUtils'
import { pulseReviewDate, reviewCoverageForArea, TECHNICAL_REVIEW_AREAS } from './pulseReviewCoverage'

const MarkdownRenderer = lazy(() => import('../ui/MarkdownRenderer').then(module => ({ default: module.MarkdownRenderer })))
const labels: Record<string, string> = { technical_review: 'Technical review', strategic_review: 'Strategic review', plan_drift_review: 'Drift check', done: 'Completed', changed: 'Changes made', timed_out: 'Timed out' }
const readable = (text?: string) => labels[text || ''] || (text ? text.charAt(0).toUpperCase() + text.slice(1).replaceAll('_', ' ') : 'Recorded')

function Evidence({ items, title = 'Evidence' }: { items?: string[]; title?: string }) {
  if (!items?.length) return null
  return <div className="mt-3"><h5 className="font-medium text-foreground">{title}</h5>
    <ul className="mt-1 list-disc space-y-1 break-words pl-4">{items.map((item, i) => <li key={i}>{item}</li>)}</ul>
  </div>
}

function ReviewReport({ report }: { report: PulseReviewReport }) {
  const [open, setOpen] = useState(false)
  const [content, setContent] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!open) return
    let current = true
    setError(''); setContent(null)
    void agentApi.getPlannerFileContent(report.path).then(result => {
      if (!current) return
      if (!result.success || result.data?.content == null) throw new Error('This report could not be read.')
      setContent(result.data.content)
    }).catch(err => { if (current) setError(err instanceof Error ? err.message : 'This report could not be read.') })
    return () => { current = false }
  }, [open, report.path, retry])
  return <details className="rounded-lg border bg-background" onToggle={event => setOpen(event.currentTarget.open)}>
    <summary className="cursor-pointer px-3 py-2 text-xs text-foreground">
      <span className="inline-flex items-center gap-2"><FileText className="h-3.5 w-3.5" />Read report</span>
      <span className="ml-2 text-muted-foreground">Updated {pulseReviewDate(report.updated_at)}</span>
    </summary>
    {open && <div className="border-t p-3"><p className="mb-3 break-all text-[11px] text-muted-foreground">{report.path}</p>
      {error ? <p className="text-xs text-red-600">{error} <button className="underline" onClick={() => setRetry(value => value + 1)}>Retry</button></p>
        : content === null ? <p className="text-xs text-muted-foreground">Loading report…</p>
          : <div className="max-h-[65vh] overflow-auto"><Suspense fallback={<Loader2 className="h-4 w-4 animate-spin" />}><MarkdownRenderer content={content} basePath={report.path.substring(0, report.path.lastIndexOf('/'))} /></Suspense></div>}
    </div>}
  </details>
}

function Reports({ reports }: { reports: PulseReviewReport[] }) {
  const [all, setAll] = useState(false)
  if (!reports.length) return <p className="text-xs text-muted-foreground">No saved review reports found.</p>
  return <div className="space-y-2">
    {(all ? reports : reports.slice(0, 3)).map(report => <ReviewReport key={report.path} report={report} />)}
    {reports.length > 3 && <button type="button" onClick={() => setAll(value => !value)} className="text-xs font-medium text-primary hover:underline">
      {all ? 'Show fewer reports' : `Show all ${reports.length} reports`}
    </button>}
  </div>
}

function FocusDetails({ items, reports, findings }: { items: PulseReviewFocus[]; reports: PulseReviewReport[]; findings: PulseFindingLifecycle[] }) {
  return <div className="space-y-4">
    {!items.length && <p>No review recorded for this scope. A Pulse tick or general store check does not establish coverage.</p>}
    {items.map(item => <div key={`${item.focus_key}:${item.route_scope}:${item.last_pulse_run_id}`}>
      <p className="font-medium text-foreground">{item.route_scope || 'General review'} · {pulseReviewDate(item.last_reviewed_at)}</p>
      <p className="mt-1">{item.last_verdict || item.last_selection_reason || 'Review recorded without a summary.'}</p>
      <Evidence items={item.evidence} />
      {findings.filter(finding => item.issue_ids?.includes(finding.finding_id || '')).map(issue => <div key={issue.finding_id || issue.text} className="mt-3 border-l-2 pl-3">
        <p className="font-medium text-foreground">{issue.text}</p>
        {issue.fix_attempts.map(attempt => <p key={attempt.attempt_id} className="mt-1">{attempt.summary} · {readable(attempt.status)}</p>)}
        {issue.verifications.map((check, index) => <div key={index} className="mt-2"><p>{check.check} · <strong>{readable(check.verdict)}</strong></p>
          {check.observed && <p>{check.observed}</p>}<Evidence items={check.evidence} /></div>)}
      </div>)}
      <div className="mt-3"><Reports reports={reports.filter(report => report.pulse_run_id === item.last_pulse_run_id)} /></div>
    </div>)}
  </div>
}

export function PulseReviewOverview({ moduleStates, coverage, audits, reports, findings, moduleFilter, onSelectModule }: {
  moduleStates: PulseModuleState[]; coverage: PulseReviewFocus[]; audits: PulseReviewAudit[];
  reports: PulseReviewReport[]; findings: PulseFindingLifecycle[]; moduleFilter: string | null;
  onSelectModule: (module: string) => void;
}) {
  const drift = moduleStates.find(item => normalizePulseWorkspaceModule(item.module) === 'plan_drift_review')
  const driftAudit = audits.find(item => item.module === 'plan_drift_review' && item.result !== 'skipped')
  const pendingDrift = drift?.last_gate_decision === 'due' && drift.last_pulse_run_id !== driftAudit?.pulse_run_id
  const visibleAudits = audits.filter(item => item.result !== 'skipped' && (!moduleFilter || item.module === moduleFilter))
  return <section className="space-y-4" aria-label="Pulse reviews">
    <h3 className="text-sm font-semibold">Work areas</h3>
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-muted/20 px-4 py-3">
      <div className="flex items-center gap-2 text-xs"><GitCompare className="h-4 w-4 text-muted-foreground" /><span className="font-medium">Drift check</span>
        <span className="text-muted-foreground">{pendingDrift ? 'Pending check' : driftAudit
          ? `${['done', 'changed'].includes(driftAudit.result) ? 'Checked' : readable(driftAudit.result)} · ${pulseReviewDate(driftAudit.recorded_at)}`
          : 'No completed check recorded'}</span></div>
      <details className="text-xs text-muted-foreground"><summary className="cursor-pointer">Details</summary>
        <div className="mt-2 max-w-xl space-y-2"><p>{drift?.last_reason || driftAudit?.reason || 'No drift-check details recorded.'}</p>
          {pendingDrift && driftAudit && <p>Previous check: {pulseReviewDate(driftAudit.recorded_at)} · {readable(driftAudit.result)}</p>}
          <button type="button" className="text-primary hover:underline" onClick={() => onSelectModule('plan_drift_review')}>View drift findings</button></div>
      </details>
    </div>
    <div className="grid items-start gap-4 xl:grid-cols-2">
      {(['technical_review', 'strategic_review'] as const).map(module => {
        const technical = module === 'technical_review'
        const Icon = technical ? Wrench : Lightbulb
        const state = moduleStates.find(item => normalizePulseWorkspaceModule(item.module) === module)
        const latestAudit = audits.find(item => item.module === module && item.result !== 'skipped')
        const scoped = coverage.filter(item => normalizePulseWorkspaceModule(item.module) === module)
        const latest = [...scoped].sort((a, b) => (b.last_reviewed_at || '').localeCompare(a.last_reviewed_at || ''))[0]
        const moduleReports = reports.filter(item => item.module === module)
        return <section key={module} className="overflow-hidden rounded-xl border bg-background">
          <button type="button" aria-pressed={moduleFilter === module} onClick={() => onSelectModule(module)} className="flex w-full items-start gap-2.5 p-4 text-left hover:bg-muted/30">
            <Icon className={`mt-0.5 h-4 w-4 shrink-0 ${technical ? 'text-sky-500' : 'text-amber-500'}`} />
            <div><h4 className="text-sm font-semibold">{technical ? 'Technical review' : 'Strategic review'}</h4>
              <p className="mt-1 text-xs text-muted-foreground">{technical ? 'What was checked, what changed, and the evidence.' : 'Progress toward the goal, recommendations, and saved reports.'}</p></div>
          </button>
          <div className="space-y-4 px-4 pb-4">
            {!technical && <div className="text-xs leading-5 text-muted-foreground">
              <p className="font-medium text-foreground">{latestAudit?.reason || latest?.last_verdict || 'No review outcome recorded yet.'}</p>
              <p>Last reviewed: {pulseReviewDate(latestAudit?.recorded_at || latest?.last_reviewed_at)}</p>
              {state?.last_gate_decision === 'skipped' && <p className="mt-2">No new review this run. {state.last_reason}</p>}
            </div>}
            {technical && <div className="divide-y rounded-lg border">
              {TECHNICAL_REVIEW_AREAS.map(area => {
                const items = reviewCoverageForArea(area, scoped)
                return <details key={area.key} className="px-3 py-2.5">
                  <summary className="cursor-pointer text-xs"><span className="font-medium text-foreground">{area.label}</span>
                    <span className={`mt-1 block pl-4 ${items[0] ? 'text-muted-foreground' : 'text-amber-600 dark:text-amber-300'}`}>
                      {items[0] ? `Last reviewed ${pulseReviewDate(items[0].last_reviewed_at)}` : 'No specific review recorded'}</span></summary>
                  <div className="mt-3 border-t pt-3 text-xs leading-5 text-muted-foreground"><FocusDetails items={items} reports={moduleReports} findings={findings} /></div>
                </details>
              })}
            </div>}
            <div><h5 className="mb-2 text-xs font-semibold">Review reports <span className="font-normal text-muted-foreground">({moduleReports.length})</span></h5><Reports reports={moduleReports} /></div>
          </div>
        </section>
      })}
    </div>
    <section className="overflow-hidden rounded-xl border bg-background">
      <div className="px-4 py-3"><h4 className="text-sm font-semibold">Checks and results</h4><p className="mt-1 text-xs text-muted-foreground">Recorded review actions and outcomes. Expand an entry for tests, changed files, and evidence.</p></div>
      <div className="divide-y">{visibleAudits.slice(0, 20).map(item => <details key={`${item.module}:${item.pulse_run_id}`} className="px-4 py-3">
        <summary className="cursor-pointer text-xs">{readable(item.module)} · {readable(item.result)} · {pulseReviewDate(item.recorded_at)}<span className="mt-1 block pl-4 text-muted-foreground">{item.reason}</span></summary>
        <div className="mt-3 text-xs leading-5 text-muted-foreground"><Evidence title="Recorded checks" items={item.verification} />
          {!item.verification?.length && <p>No test results attached to this review.</p>}
          <Evidence title="Changed files" items={item.changed_files} /><Evidence items={item.evidence} />
          <div className="mt-3"><Reports reports={reports.filter(report => report.module === item.module && report.pulse_run_id === item.pulse_run_id)} /></div></div>
      </details>)}
      {!visibleAudits.length && <p className="px-4 pb-4 text-xs text-muted-foreground">No review results recorded yet.</p>}</div>
    </section>
  </section>
}
