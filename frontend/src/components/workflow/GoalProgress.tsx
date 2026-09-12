import type {
  GoalMetric,
  PulseGoalObservation,
  PulseImpactLedger,
} from "../../services/api-types";
import { AskAIButton } from "./AskAIButton";
import { TooltipProvider } from "../ui/tooltip";
import {
  goalMetricGroups,
  supportingMetricLabel,
  metricDimensions,
} from "./goalMetricGroups";
import { goalMetricProgress } from "./goalMetricProgress";

const format = (value: number) =>
  value.toLocaleString(undefined, { maximumFractionDigits: 2 });
export const GOAL_SETUP_MESSAGE =
  'Call get_workflow_command_guidance(kind="setup-goals") and follow the shared goal and measurement setup flow for this workflow. Use existing goals and data, preserve constraints and history, and ask only unresolved decisions.';

function MetricCard({
  metric,
  observations,
}: {
  metric: GoalMetric;
  observations: PulseGoalObservation[];
}) {
  const p = goalMetricProgress(metric, observations);
  const primary = metric.role === "primary";
  const values = p.numeric.map((o) => o.value!);
  const low = Math.min(...values),
    high = Math.max(...values);
  const times = p.numeric.map((o) => Date.parse(o.observed_at));
  const start = times[0],
    end = times.at(-1) || start;
  const points = p.numeric
    .map(
      (o) =>
        `${8 + (end === start ? 0.5 : (Date.parse(o.observed_at) - start) / (end - start)) * 584},${high === low ? 55 : 94 - ((o.value! - low) / (high - low)) * 80}`,
    )
    .join(" ");
  return (
    <article
      className={`min-w-0 rounded-xl border p-4 ${primary ? "border-sky-500/25 bg-sky-500/5 sm:p-5" : "bg-background"}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
            {primary ? "Primary metric" : supportingMetricLabel(metric)}
          </p>
          <h3 className="mt-1 text-sm font-medium">{metric.name}</h3>
        </div>
        <span
          className={`rounded-full px-2 py-1 text-[11px] ${p.targetMet ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" : "bg-muted text-muted-foreground"}`}
        >
          {p.state}
        </span>
      </div>
      <div className="mt-3 flex flex-wrap items-baseline gap-2">
        <strong
          className={
            primary
              ? "text-4xl font-semibold tabular-nums"
              : "text-2xl font-semibold tabular-nums"
          }
        >
          {p.current === undefined ? "—" : format(p.current)}
        </strong>
        <span className="text-sm text-muted-foreground">{metric.unit}</span>
      </div>
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span>{metric.window}</span>
        {metricDimensions(metric) && <span>{metricDimensions(metric)}</span>}
        {p.delta !== undefined && (
          <span>
            {p.delta > 0 ? "+" : ""}
            {format(p.delta)} {metric.unit} since previous measurement
          </span>
        )}
        <span>
          {metric.target === undefined
            ? "Target not set"
            : `Target ${metric.direction === "decrease" ? "≤ " : metric.direction === "increase" ? "≥ " : ""}${format(metric.target)} ${metric.unit}${metric.target_date ? ` by ${metric.target_date}` : ""}`}
        </span>
      </div>
      {p.numeric.length > 1 && (
        <svg
          viewBox="0 0 600 110"
          role="img"
          aria-label={`${metric.name} over time; ${p.numeric.length} measurements. Exact values below.`}
          className={`${primary ? "h-36" : "h-20"} mt-3 w-full text-sky-500`}
          preserveAspectRatio="none"
        >
          <title>
            {metric.name}: {format(values[0])} to {format(values.at(-1)!)}{" "}
            {metric.unit}
          </title>
          <polyline
            points={points}
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            vectorEffect="non-scaling-stroke"
          />
        </svg>
      )}
      {p.numeric.length > 1 && (
        <div className="flex justify-between text-[10px] text-muted-foreground">
          <span>{new Date(p.numeric[0].observed_at).toLocaleDateString()}</span>
          <span>
            {new Date(p.numeric.at(-1)!.observed_at).toLocaleDateString()}
          </span>
        </div>
      )}
      <p className="mt-3 text-xs text-muted-foreground">
        {p.latest
          ? `Last observed ${new Date(p.latest.observed_at).toLocaleString()}`
          : "Connect collection to start tracking."}
        {p.current === undefined && p.latest?.status
          ? ` · ${p.latest.status}`
          : ""}
      </p>
      <details className="mt-3 text-xs">
        <summary className="cursor-pointer text-muted-foreground">
          Measurement details and history
        </summary>
        <dl className="mt-3 space-y-2 break-words text-muted-foreground">
          <div>
            <dt className="font-medium text-foreground">Definition</dt>
            <dd>{metric.definition}</dd>
          </div>
          <div>
            <dt className="font-medium text-foreground">Source</dt>
            <dd>{metric.source}</dd>
          </div>
          <div>
            <dt className="font-medium text-foreground">Collection</dt>
            <dd>
              {metric.collection_frequency} · stale after{" "}
              {metric.freshness_hours} hours
            </dd>
          </div>
          {(metric.route || metric.environment) && (
            <div>
              <dt>Scope</dt>
              <dd>
                {[metric.route, metric.environment].filter(Boolean).join(" · ")}
              </dd>
            </div>
          )}
        </dl>
        <div className="mt-3 max-h-56 overflow-auto">
          <table className="w-full text-left">
            <caption className="sr-only">
              Recent comparable measurements
            </caption>
            <thead>
              <tr>
                <th className="py-2">Observed</th>
                <th>Value</th>
                <th>Evidence</th>
              </tr>
            </thead>
            <tbody>
              {[...p.history].reverse().map((o) => (
                <tr key={o.observation_id} className="border-t">
                  <td className="py-2 pr-2">
                    {new Date(o.observed_at).toLocaleString()}
                  </td>
                  <td className="pr-2">
                    {typeof o.value === "number"
                      ? `${format(o.value)} ${metric.unit}`
                      : o.status || "Unavailable"}
                  </td>
                  <td className="break-all">
                    {o.evidence?.join(", ") || "Not recorded"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </article>
  );
}

export function GoalProgress({
  impact,
  workspacePath,
}: {
  impact: PulseImpactLedger;
  workspacePath: string;
}) {
  const metrics = impact.metrics || [];
  const { groups, unassigned } = goalMetricGroups(metrics);
  return (
    <section aria-label="Goal progress" className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">Progress toward goals</h2>
        <TooltipProvider>
          <AskAIButton
            workspacePath={workspacePath}
            label={
              metrics.length ? "Edit goals & metrics" : "Set up goals & metrics"
            }
            message={GOAL_SETUP_MESSAGE}
          />
        </TooltipProvider>
      </div>
      {!metrics.length ? (
        <div className="rounded-xl border border-dashed p-5">
          <p className="text-sm font-medium">Measurement setup needed</p>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">
            Choose primary metrics and their supporting measurements with the
            builder. Existing workflow runs and history are preserved.
          </p>
        </div>
      ) : (
        <>
          {groups.map((group) => (
            <section
              key={group.id}
              aria-label={group.name}
              className="space-y-3"
            >
              <h3 className="text-sm font-semibold">{group.name}</h3>
              {group.primaries.map(({ metric, supporting }) => (
                <section
                  key={metric.id}
                  aria-label={`${metric.name} and supporting measurements`}
                  className="space-y-3"
                >
                  <MetricCard
                    metric={metric}
                    observations={impact.observations}
                  />
                  {supporting.length > 0 && (
                    <div className="grid gap-3 border-l-2 pl-3 md:grid-cols-2">
                      {supporting.map((m) => (
                        <MetricCard
                          key={m.id}
                          metric={m}
                          observations={impact.observations}
                        />
                      ))}
                    </div>
                  )}
                </section>
              ))}
            </section>
          ))}
          {unassigned.length > 0 && (
            <section
              aria-label="Unassigned supporting measurements"
              className="space-y-3"
            >
              <p className="text-xs text-muted-foreground">
                These supporting measurements need a primary metric
                relationship. Edit goals &amp; metrics to assign them.
              </p>
              {unassigned.map((m) => (
                <MetricCard
                  key={m.id}
                  metric={m}
                  observations={impact.observations}
                />
              ))}
            </section>
          )}
        </>
      )}
    </section>
  );
}
