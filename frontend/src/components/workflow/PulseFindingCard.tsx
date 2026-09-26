import { useMemo, useState } from 'react'
import {
  AlertTriangle,
  Bug,
  CheckCircle2,
  Circle,
  Clock3,
  FileCheck2,
  History,
  RotateCcw,
  Terminal,
  Wrench,
  XCircle,
} from 'lucide-react'
import type {
  PulseFindingLifecycle,
  PulseFindingVerification,
} from '../../services/api-types'
import { pulseIssueForFinding } from './pulseModuleInspectorUtils'
import { WorkspacePanelGuideButton } from './WorkspacePanelGuideButton'
import {
  pulseFindingImpact,
  pulseFindingPresentation,
  pulseFindingProgress,
  pulseFindingReporter,
  pulseFixAttemptIsIncomplete,
  pulseVerificationLevel,
  type PulseFindingTone,
} from './pulseFindingPresentation'

type DetailTab = 'fix' | 'verification' | 'activity'

function formatDate(value?: string): string {
  if (!value) return 'Not recorded'
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
}

function readable(value?: string): string {
  return (value || '').trim().replaceAll('_', ' ') || 'Not recorded'
}

const STATUS_TONES: Record<PulseFindingTone, string> = {
  danger: 'border-red-500/25 bg-red-500/10 text-red-700 dark:text-red-300',
  warning: 'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300',
  info: 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300',
  success: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
  decision: 'border-fuchsia-500/25 bg-fuchsia-500/10 text-fuchsia-700 dark:text-fuchsia-300',
  neutral: 'border-border bg-muted text-muted-foreground',
}

function priorityTone(priority: string): string {
  if (priority === 'urgent') return 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300'
  if (priority === 'high') return 'border-orange-500/30 bg-orange-500/10 text-orange-700 dark:text-orange-300'
  if (priority === 'medium') return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
  if (priority === 'low') return 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300'
  return 'border-border bg-muted text-muted-foreground'
}

function VerificationIcon({ verification }: { verification: PulseFindingVerification }) {
  if (verification.verdict === 'passed') return <CheckCircle2 className="h-4 w-4 text-emerald-500" />
  if (verification.verdict === 'failed') return <XCircle className="h-4 w-4 text-red-500" />
  return <Clock3 className="h-4 w-4 text-amber-500" />
}

function FindingProgress({ finding }: { finding: PulseFindingLifecycle }) {
  const steps = pulseFindingProgress(finding)
  return (
    <div>
      <div className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Resolution progress</div>
      <div className="mt-2 grid grid-cols-5 gap-1" aria-label="Finding resolution progress">
        {steps.map((step, index) => (
          <div key={step.label} className="min-w-0">
            <div className="flex items-center">
              <span className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-full border ${
                step.state === 'done'
                  ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-300'
                  : step.state === 'current'
                    ? 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-300'
                    : 'border-border bg-muted/40 text-muted-foreground/40'
              }`}>
                {step.state === 'done' ? <CheckCircle2 className="h-3 w-3" /> : <Circle className="h-2.5 w-2.5" />}
              </span>
              {index < steps.length - 1 && (
                <span className={`h-px flex-1 ${step.state === 'done' ? 'bg-emerald-500/35' : 'bg-border'}`} />
              )}
            </div>
            <div className={`mt-1 truncate pr-1 text-[9px] ${
              step.state === 'pending' ? 'text-muted-foreground/50' : 'font-medium text-foreground'
            }`}>
              {step.label}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

export function PulseFindingCard({
  finding,
  moduleLabel,
  reviewFocus,
  expanded,
  onToggle,
  onOpenModule,
}: {
  finding: PulseFindingLifecycle
  moduleLabel?: string
  reviewFocus?: { label: string; relatedCount: number }
  expanded: boolean
  onToggle: () => void
  onOpenModule?: () => void
}) {
  const [tab, setTab] = useState<DetailTab>('verification')
  const [showAllActivity, setShowAllActivity] = useState(false)
  const issue = pulseIssueForFinding(finding)
  const presentation = pulseFindingPresentation(finding)
  const reporter = pulseFindingReporter(finding, moduleLabel)
  const impact = pulseFindingImpact(finding)
  const problem = issue.description?.trim() || finding.text.trim() || issue.title
  const attempts = useMemo(
    () => [...finding.fix_attempts].sort((a, b) => b.started_at.localeCompare(a.started_at)),
    [finding.fix_attempts],
  )
  const events = useMemo(
    () => [...finding.events].sort((a, b) => b.recorded_at.localeCompare(a.recorded_at)),
    [finding.events],
  )
  const passedChecks = finding.verifications.filter((verification) => verification.verdict === 'passed').length
  const failedChecks = finding.verifications.filter((verification) => verification.verdict === 'failed').length
  const pendingChecks = finding.verifications.length - passedChecks - failedChecks
  const showProgress = ['needs_action', 'waiting_proof', 'resolved'].includes(presentation.queue)
  const harnessDetails = finding.details?.issue_kind === 'harness_issue' ? finding.details : null

  const toggle = () => {
    if ((window.getSelection()?.toString() || '').length > 0) return
    onToggle()
  }

  return (
    <article className={`overflow-hidden rounded-lg border bg-background transition-colors ${expanded ? 'border-primary/30 shadow-sm' : 'hover:border-border/80'}`}>
      <div
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        onClick={toggle}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' && event.key !== ' ') return
          event.preventDefault()
          onToggle()
        }}
        className="cursor-pointer select-text px-3 py-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary/40 sm:px-4"
      >
        <div className="flex min-w-0 items-start gap-3">
          <div className="mt-0.5 flex max-w-36 shrink-0 flex-col items-start gap-1">
            <span className={`rounded border px-1.5 py-0.5 text-[9px] font-semibold uppercase ${priorityTone(issue.priority)}`}>
              {issue.priority === 'none' ? 'Issue' : issue.priority}
            </span>
            <code
              className="max-w-full truncate rounded bg-muted px-1.5 py-0.5 text-[9px] font-semibold text-muted-foreground"
              title={`Issue ID: ${issue.id}`}
              aria-label={`Issue ID ${issue.id}`}
            >
              {issue.id}
            </code>
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <h4 className="min-w-0 flex-1 text-xs font-semibold leading-5 text-foreground">{issue.title}</h4>
              <span className={`shrink-0 rounded-full border px-2 py-0.5 text-[9px] font-semibold ${STATUS_TONES[presentation.tone]}`}>
                {presentation.label}
              </span>
            </div>
            {expanded && (
              <div className="mt-2 grid gap-2 lg:grid-cols-[minmax(0,1fr)_minmax(240px,0.7fr)]">
                <div className="rounded-md border bg-muted/20 px-2.5 py-2">
                  <div className="text-[9px] font-semibold uppercase tracking-wide text-muted-foreground">Problem and impact</div>
                  <p className="mt-1 whitespace-pre-wrap text-[10px] leading-4 text-foreground">{problem}</p>
                  {impact && impact !== problem && (
                    <p className="mt-1 text-[10px] leading-4 text-muted-foreground"><b className="text-foreground">Impact:</b> {impact}</p>
                  )}
                </div>
                <div className="rounded-md border bg-muted/20 px-2.5 py-2">
                  <div className="flex items-center gap-1.5 text-[9px] font-semibold uppercase tracking-wide text-muted-foreground">
                    {presentation.queue === 'waiting_proof'
                      ? <Clock3 className="h-3 w-3 shrink-0 text-amber-500" />
                      : presentation.queue === 'resolved'
                        ? <CheckCircle2 className="h-3 w-3 shrink-0 text-emerald-500" />
                        : <AlertTriangle className="h-3 w-3 shrink-0" />}
                    What happens next
                  </div>
                  <p className="mt-1 text-[10px] leading-4 text-foreground">{presentation.nextAction}</p>
                  {(finding.external_owner || finding.reopen_condition) && (
                    <p className="mt-1 text-[9px] leading-4 text-muted-foreground">
                      {finding.external_owner && <>Owner: {readable(finding.external_owner)}. </>}
                      {finding.reopen_condition && <>Reopen when: {finding.reopen_condition}</>}
                    </p>
                  )}
                </div>
              </div>
            )}
            <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[9px] text-muted-foreground">
              <span><b className="font-semibold text-foreground/80">Reported by</b> {reporter}</span>
              {reviewFocus && (
                <>
                  <span>·</span>
                  <span className="rounded bg-muted px-1.5 py-0.5 font-medium text-foreground" title="Deep review subcategory">
                    {reviewFocus.label}{reviewFocus.relatedCount > 0 ? ` +${reviewFocus.relatedCount}` : ''}
                  </span>
                </>
              )}
              <span>·</span>
              <span>{formatDate(issue.updated_at || finding.last_seen_at)}</span>
              {issue.seen_count > 1 && (
                <span className="inline-flex items-center gap-1 text-amber-700 dark:text-amber-300">
                  <RotateCcw className="h-2.5 w-2.5" /> Recurred {issue.seen_count}×
                </span>
              )}
            </div>
          </div>
        </div>
      </div>

      {expanded && (
        <div className="border-t bg-muted/10 px-3 py-3 sm:px-4" onClick={(event) => event.stopPropagation()}>
          {showProgress && (
            <div className="rounded-lg border bg-background p-3">
              <FindingProgress finding={finding} />
            </div>
          )}

          <div className="mt-3 overflow-hidden rounded-lg border bg-background">
            <div className="flex items-center justify-between gap-2 border-b bg-muted/20 p-1">
              <div className="flex min-w-0 items-center gap-1 overflow-x-auto" role="tablist" aria-label="Issue details">
                {([
                  ['fix', 'Fix', attempts.length, Wrench],
                  ['verification', 'Verification', finding.verifications.length, FileCheck2],
                  ['activity', 'Activity', events.length, History],
                ] as const).map(([id, label, count, Icon]) => (
                  <button
                    key={id}
                    type="button"
                    role="tab"
                    aria-selected={tab === id}
                    onClick={() => setTab(id)}
                    className={`inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-[10px] font-medium transition-colors ${
                      tab === id ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    <Icon className="h-3 w-3" />
                    {label}
                    <span className="rounded-full bg-muted px-1.5 py-0.5 text-[9px] tabular-nums">{count}</span>
                  </button>
                ))}
              </div>
              <WorkspacePanelGuideButton topic={`Pulse · Issue ${tab === 'fix' ? 'Fix' : tab === 'verification' ? 'Verification' : 'Activity'}`} />
            </div>

            {tab === 'fix' && (
              <div className="space-y-2 p-3">
                {attempts.length === 0 ? (
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Wrench className="h-3.5 w-3.5" /> No repair attempt has been recorded yet.
                  </div>
                ) : attempts.map((attempt, index) => {
                  const issueID = finding.issue?.id || finding.finding_id
                  const reference = attempt.findings?.find((candidate) => (
                    issueID && candidate.finding_id === issueID
                  ))
                  const incomplete = pulseFixAttemptIsIncomplete(finding, attempt)
                  return (
                    <div key={attempt.attempt_id} className="rounded-md border bg-muted/10 p-3">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <span className="text-[10px] font-semibold text-foreground">{index === 0 ? 'Latest attempt' : `Previous attempt ${index}`}</span>
                        <span className="rounded-full border bg-background px-2 py-0.5 text-[9px] font-medium text-muted-foreground">
                          {readable(reference?.disposition || attempt.status)}
                        </span>
                      </div>
                      <p className="mt-1.5 text-xs leading-5 text-foreground">{reference?.summary || attempt.summary || 'No summary recorded.'}</p>
                      {incomplete && (
                        <div className="mt-2 flex items-start gap-2 rounded-md border border-red-500/20 bg-red-500/5 px-2.5 py-2 text-[10px] leading-4 text-red-700 dark:text-red-300">
                          <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" />
                          <span><b>Fix record incomplete.</b> The issue moved forward, but this attempt has no completed timestamp.</span>
                        </div>
                      )}
                      {attempt.changed_files.length > 0 && (
                        <div className="mt-2">
                          <div className="text-[9px] font-semibold uppercase tracking-wide text-muted-foreground">Changed files</div>
                          <div className="mt-1 flex flex-wrap gap-1">
                            {attempt.changed_files.map((file) => (
                              <code key={file} className="rounded bg-muted px-1.5 py-1 text-[9px] text-muted-foreground">{file}</code>
                            ))}
                          </div>
                        </div>
                      )}
                      <div className="mt-2 text-[9px] text-muted-foreground">
                        Started {formatDate(attempt.started_at)}{attempt.completed_at ? ` · completed ${formatDate(attempt.completed_at)}` : ''}
                      </div>
                    </div>
                  )
                })}
              </div>
            )}

            {tab === 'verification' && (
              <div className="p-3">
                <div className="mb-2 flex flex-wrap items-center gap-2 text-[10px] text-muted-foreground">
                  <span className="text-emerald-700 dark:text-emerald-300">{passedChecks} passed</span>
                  <span>·</span>
                  <span className="text-red-700 dark:text-red-300">{failedChecks} failed</span>
                  <span>·</span>
                  <span className="text-amber-700 dark:text-amber-300">{pendingChecks} inconclusive</span>
                </div>
                {finding.verifications.length === 0 ? (
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Clock3 className="h-3.5 w-3.5" /> No verification check has been recorded yet.
                  </div>
                ) : (
                  <div className="space-y-2">
                    {finding.verifications.map((verification, index) => (
                      <div key={`${verification.check}-${index}`} className="rounded-md border bg-muted/10 p-3">
                        <div className="flex items-start gap-2">
                          <VerificationIcon verification={verification} />
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="text-xs font-medium text-foreground">{verification.check}</span>
                              <span className="rounded-full border bg-background px-1.5 py-0.5 text-[9px] text-muted-foreground">
                                {pulseVerificationLevel(verification)}
                              </span>
                            </div>
                            {(verification.expected || verification.observed) && (
                              <div className="mt-1.5 space-y-1 text-[10px] leading-4 text-muted-foreground">
                                {verification.expected && <div><b className="text-foreground">Expected:</b> {verification.expected}</div>}
                                {verification.observed && <div><b className="text-foreground">Observed:</b> {verification.observed}</div>}
                              </div>
                            )}
                            {(verification.evidence || []).length > 0 && (
                              <details className="mt-2 text-[10px] text-muted-foreground">
                                <summary className="cursor-pointer font-medium text-foreground">Evidence ({verification.evidence?.length})</summary>
                                <div className="mt-1 break-words font-mono text-[9px] leading-4">{verification.evidence?.join(' · ')}</div>
                              </details>
                            )}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}

            {tab === 'activity' && (
              <div className="p-3">
                {events.length === 0 ? (
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <History className="h-3.5 w-3.5" /> No lifecycle activity has been recorded.
                  </div>
                ) : (
                  <div className="space-y-2">
                    {(showAllActivity ? events : events.slice(0, 3)).map((event, index) => (
                      <div key={`${event.event_type}-${event.recorded_at}-${index}`} className="flex items-start gap-2 text-[10px] leading-4">
                        <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-muted-foreground/60" />
                        <div className="min-w-0">
                          <span className="font-semibold capitalize text-foreground">{readable(event.event_type)}</span>
                          {event.summary && event.summary !== finding.text && <span className="text-muted-foreground"> · {event.summary}</span>}
                          <div className="mt-0.5 text-[9px] text-muted-foreground">{formatDate(event.recorded_at)}</div>
                        </div>
                      </div>
                    ))}
                    {events.length > 3 && (
                      <button type="button" onClick={() => setShowAllActivity((value) => !value)} className="text-[10px] font-medium text-primary hover:underline">
                        {showAllActivity ? 'Show recent activity only' : `Show full history (${events.length})`}
                      </button>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>

          {(harnessDetails || finding.details?.evidence?.length || finding.details?.workaround) && (
            <details className="mt-3 rounded-lg border bg-background p-3 text-[10px] text-muted-foreground">
              <summary className="flex cursor-pointer list-none items-center gap-2 font-semibold text-foreground">
                {harnessDetails ? <Bug className="h-3.5 w-3.5 text-orange-500" /> : <Terminal className="h-3.5 w-3.5" />}
                Technical evidence and reproduction
              </summary>
              {finding.details?.evidence && finding.details.evidence.length > 0 && (
                <ul className="mt-2 space-y-1">
                  {finding.details.evidence.map((evidence, index) => <li key={`${evidence}-${index}`} className="break-words">• {evidence}</li>)}
                </ul>
              )}
              {finding.details?.workaround && <p className="mt-2"><b className="text-foreground">Workaround:</b> {finding.details.workaround}</p>}
              {harnessDetails?.reproduction && (
                <div className="mt-2 space-y-1">
                  {harnessDetails.reproduction.setup && <p><b className="text-foreground">Setup:</b> {harnessDetails.reproduction.setup}</p>}
                  {harnessDetails.reproduction.action && <pre className="whitespace-pre-wrap break-words rounded bg-muted p-2 font-mono text-[9px] text-foreground">{harnessDetails.reproduction.action}</pre>}
                  {harnessDetails.reproduction.limitations && <p className="text-amber-700 dark:text-amber-300">{harnessDetails.reproduction.limitations}</p>}
                </div>
              )}
            </details>
          )}

          <div className="mt-3 flex flex-wrap items-center gap-2 text-[9px] text-muted-foreground">
            {onOpenModule && (
              <button type="button" onClick={onOpenModule} className="rounded border px-2 py-1 font-semibold text-foreground hover:bg-muted">
                Show issues in {moduleLabel || finding.module || 'review area'}
              </button>
            )}
            <span>First seen {formatDate(issue.created_at)}</span>
          </div>
        </div>
      )}
    </article>
  )
}

export default PulseFindingCard
