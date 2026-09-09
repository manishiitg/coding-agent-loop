import { useState } from 'react'
import { FileText, GitCompare, Lightbulb, Maximize2, Wrench } from 'lucide-react'
import type { PulseFindingLifecycle, PulseModuleState, PulseReviewAudit, PulseReviewFocus, PulseReviewReport } from '../../services/api-types'
import { normalizePulseWorkspaceModule, pulseFindingReviewAreas, pulseWorkspaceQueueCounts } from './pulseWorkspaceUtils'
import { pulseReviewDate, reviewCoverageForArea, TECHNICAL_REVIEW_AREAS } from './pulseReviewCoverage'

import { PulseReviewReportReader } from './PulseReviewReportReader'
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
  return <>
    <button type="button" aria-haspopup="dialog" onClick={() => setOpen(true)} className="flex w-full items-center justify-between gap-3 rounded-lg border bg-background px-3 py-3 text-left text-xs hover:bg-muted/30">
      <span><span className="inline-flex items-center gap-2 font-medium"><FileText className="h-3.5 w-3.5" />Read report</span>
        <span className="mt-1 block text-muted-foreground">Updated {pulseReviewDate(report.updated_at)}</span></span>
      <Maximize2 className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
    </button>
    {open && <PulseReviewReportReader report={report} title={`${readable(report.module)} report`} onClose={() => setOpen(false)} />}
  </>
}

function Reports({ reports }: { reports: PulseReviewReport[] }) {
  const [all, setAll] = useState(false)
  if (!reports.length) return <p className="text-xs text-muted-foreground">No saved review reports found.</p>
  return <div className="space-y-2">
    {(all ? reports : reports.slice(0, 1)).map(report => <ReviewReport key={report.path} report={report} />)}
    {reports.length > 1 && <button type="button" aria-expanded={all} onClick={() => setAll(value => !value)} className="text-xs font-medium text-primary hover:underline">
      {all ? 'Hide report history' : `View report history (${reports.length - 1})`}
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

export function PulseReviewOverview({ moduleStates, coverage, audits, reports, findings, moduleFilter, onSelectModule, reviewFocusSelections = [] }: {
  moduleStates: PulseModuleState[]; coverage: PulseReviewFocus[]; audits: PulseReviewAudit[];
  reports: PulseReviewReport[]; findings: PulseFindingLifecycle[]; moduleFilter: string | null;
  onSelectModule: (module: string) => void; reviewFocusSelections?: PulseReviewFocus[];
}) {
  const areas = [
    { id: 'plan_drift_review', label: 'Drift check', description: 'Changes to the plan and their follow-up checks.', Icon: GitCompare },
    { id: 'technical_review', label: 'Technical review', description: 'Execution, learnings, knowledge, and quality.', Icon: Wrench },
    { id: 'strategic_review', label: 'Strategic review', description: 'Progress toward the goal and recommendations.', Icon: Lightbulb },
  ]
  const selected = areas.find(area => area.id === moduleFilter)
  const latestAuditFor = (module: string) => audits.find(item => normalizePulseWorkspaceModule(item.module) === module && item.result !== 'skipped')
  const stateFor = (module: string) => moduleStates.find(item => normalizePulseWorkspaceModule(item.module) === module)
  const coverageFor = (module: string) => coverage.filter(item => normalizePulseWorkspaceModule(item.module) === module)
  const latestCoverageFor = (module: string) => [...coverageFor(module)].sort((a, b) => (b.last_reviewed_at || '').localeCompare(a.last_reviewed_at || ''))[0]
  const drift = stateFor('plan_drift_review')
  const driftAudit = latestAuditFor('plan_drift_review')
  const pendingDrift = drift?.last_gate_decision === 'due' && drift.last_pulse_run_id !== driftAudit?.pulse_run_id
  const driftStatus = pendingDrift ? 'Pending check' : driftAudit
    ? `${['done', 'changed'].includes(driftAudit.result) ? 'Checked' : readable(driftAudit.result)} · ${pulseReviewDate(driftAudit.recorded_at)}`
    : 'No completed check recorded'
  const selectedAudit = selected && latestAuditFor(selected.id)
  const selectedState = selected && stateFor(selected.id)
  const selectedCoverage = selected ? coverageFor(selected.id) : []
  const latestCoverage = selected && latestCoverageFor(selected.id)
  const selectedReports = reports.filter(item => normalizePulseWorkspaceModule(item.module) === moduleFilter)
  const visibleAudits = audits.filter(item => item.result !== 'skipped' && (!moduleFilter || normalizePulseWorkspaceModule(item.module) === moduleFilter))
  const driftCounts = pulseWorkspaceQueueCounts(findings.filter(item => pulseFindingReviewAreas(item, reviewFocusSelections).includes('plan_drift_review')))
  return <section className="space-y-4" aria-label="Pulse reviews">
    <div><h3 className="text-sm font-semibold">Work areas</h3><p className="mt-1 text-xs text-muted-foreground">Choose an area to see its review, reports, checks, and findings below.</p></div>
    <nav className="grid gap-2 md:grid-cols-3" aria-label="Pulse work areas">
      {areas.map(area => {
        const audit = latestAuditFor(area.id)
        const lastReviewed = audit?.recorded_at || latestCoverageFor(area.id)?.last_reviewed_at
        const active = moduleFilter === area.id
        return <button key={area.id} type="button" aria-pressed={active} onClick={() => onSelectModule(area.id)}
          className={`rounded-xl border p-3 text-left transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary ${active ? 'border-primary/40 bg-primary/10' : 'bg-background hover:bg-muted/40'}`}>
          <span className="flex items-center gap-2 text-xs font-semibold"><area.Icon className="h-4 w-4 shrink-0" />{area.label}</span>
          <span className="mt-2 block text-[11px] leading-5 text-muted-foreground">{area.id === 'plan_drift_review' ? driftStatus : lastReviewed ? `Last reviewed ${pulseReviewDate(lastReviewed)}` : 'No review recorded'}</span>
        </button>
      })}
    </nav>
    {selected && <section key={selected.id} aria-label={`${selected.label} content`} className="space-y-4 rounded-xl border bg-background p-4">
      <div><h4 className="text-sm font-semibold">{selected.label}</h4><p className="mt-1 text-xs text-muted-foreground">{selected.description}</p></div>
      {selected.id === 'plan_drift_review' ? <div className="space-y-2 text-xs leading-5 text-muted-foreground">
        <p className="font-medium text-foreground">{driftStatus}</p>
        <p>{driftCounts.all ? `${driftCounts.all} current drift finding${driftCounts.all === 1 ? '' : 's'}.` : 'No current drift findings.'}
          {driftCounts.resolved > 0 && ` ${driftCounts.resolved} resolved finding${driftCounts.resolved === 1 ? '' : 's'} available below.`}</p>
        <details><summary className="cursor-pointer">Check details</summary><p className="mt-2 break-words">{drift?.last_reason || driftAudit?.reason || 'No drift-check details recorded.'}</p>
          {pendingDrift && driftAudit && <p className="mt-2">Previous check: {pulseReviewDate(driftAudit.recorded_at)} · {readable(driftAudit.result)}</p>}
        </details>
      </div> : <div className="text-xs leading-5 text-muted-foreground">
        <p className="font-medium text-foreground">{selectedAudit?.reason || latestCoverage?.last_verdict || 'No review outcome recorded yet.'}</p>
        {selectedState?.last_gate_decision === 'skipped' && <details className="mt-2"><summary className="cursor-pointer">Why no new review ran</summary><p className="mt-2">{selectedState.last_reason}</p></details>}
      </div>}
      {selected.id === 'technical_review' && <div><h5 className="mb-2 text-xs font-semibold">Review coverage</h5><div className="divide-y rounded-lg border">
        {TECHNICAL_REVIEW_AREAS.map(area => {
          const items = reviewCoverageForArea(area, selectedCoverage)
          return <details key={area.key} className="px-3 py-2.5">
            <summary className="cursor-pointer text-xs"><span className="font-medium text-foreground">{area.label}</span>
              <span className={`mt-1 block pl-4 ${items[0] ? 'text-muted-foreground' : 'text-amber-600 dark:text-amber-300'}`}>
                {items[0] ? `Last reviewed ${pulseReviewDate(items[0].last_reviewed_at)}` : 'No specific review recorded'}</span></summary>
            <div className="mt-3 border-t pt-3 text-xs leading-5 text-muted-foreground"><FocusDetails items={items} reports={selectedReports} findings={findings} /></div>
          </details>
        })}
      </div></div>}
      <div><h5 className="mb-2 text-xs font-semibold">Review reports <span className="font-normal text-muted-foreground">({selectedReports.length})</span></h5><Reports reports={selectedReports} /></div>
    </section>}
    <ReviewChecks key={`checks:${moduleFilter || 'all'}`} audits={visibleAudits} reports={reports} label={selected?.label} />
  </section>
}


function ReviewChecks({ audits, reports, label }: { audits: PulseReviewAudit[]; reports: PulseReviewReport[]; label?: string }) {
  const [history, setHistory] = useState(false)
  const [limit, setLimit] = useState(10)
  const renderCheck = (item: PulseReviewAudit, latest: boolean) => <details key={`${item.module}:${item.pulse_run_id}`} className="px-4 py-3">
    <summary className="cursor-pointer text-xs">{latest ? 'Latest check' : readable(item.module)} · {readable(item.result)} · {pulseReviewDate(item.recorded_at)}</summary>
    <div className="mt-3 text-xs leading-5 text-muted-foreground">
      <p>{item.reason}</p><Evidence title="Recorded checks" items={item.verification} />
      {!item.verification?.length && <p className="mt-2">No test results attached to this review.</p>}
      <Evidence title="Changed files" items={item.changed_files} /><Evidence items={item.evidence} />
      <div className="mt-3"><Reports reports={reports.filter(report => normalizePulseWorkspaceModule(report.module) === normalizePulseWorkspaceModule(item.module) && report.pulse_run_id === item.pulse_run_id)} /></div>
    </div>
  </details>
  return <section className="overflow-hidden rounded-xl border bg-background" aria-label={label ? `${label} checks and results` : 'All checks and results'}>
    {audits.length ? renderCheck(audits[0], true) : <p className="p-4 text-xs text-muted-foreground">No review results recorded yet.</p>}
    {audits.length > 1 && <div className="border-t">
      <button type="button" aria-expanded={history} onClick={() => { setHistory(value => !value); setLimit(10) }} className="px-4 py-3 text-xs font-medium text-primary hover:underline">
        {history ? 'Hide review history' : `View review history (${audits.length - 1})`}
      </button>
      {history && <div className="divide-y border-t" aria-label="Review history">
        {audits.slice(1, limit + 1).map(item => renderCheck(item, false))}
        {audits.length > limit + 1 && <button type="button" onClick={() => setLimit(value => value + 10)} className="px-4 py-3 text-xs font-medium text-primary hover:underline">Show more reviews</button>}
      </div>}
    </div>}
  </section>
}
