import { useState } from "react";
import {
  PulseMetricOutcomes,
  pulseMetricOutcomes,
  verdictLabels,
} from "./PulseMetricOutcomes";
import type { PulseImpactLedger } from "../../services/api-types";

const statusLabels: Record<string, string> = {
  proposed: "Proposed",
  approved: "Approved · awaiting application",
  running: "Applied · awaiting outcomes",
  measuring: "Measuring outcomes",
  adopted: "Adopted",
  deferred: "Deferred",
  rejected: "Rejected",
  retired: "Retired",
  awaiting_evidence: "Awaiting evidence",
  assessed: "Assessed",
  blocked: "Blocked",
};
export function PulseImprovements({ impact }: { impact: PulseImpactLedger }) {
  const [all, setAll] = useState(false);
  const items = [...impact.interventions].sort((a, b) =>
    (b.updated_at || "").localeCompare(a.updated_at || ""),
  );
  if (!items.length) return null;
  return (
    <section
      aria-label="Workflow improvements"
      className="rounded-xl border bg-background p-4"
    >
      <h3 className="text-sm font-semibold">Improvements</h3>
      <p className="mt-1 text-xs text-muted-foreground">
        From proposal to applied change and observed outcome.
      </p>
      <div className="mt-3 divide-y">
        {(all ? items : items.slice(0, 3)).map((item) => {
          const outcomes = pulseMetricOutcomes(item, impact.assessments);
          const assessment = outcomes[0]?.assessment;
          const outcomeSummary =
            outcomes.length === 1
              ? assessment
                ? verdictLabels[assessment.verdict] || assessment.verdict
                : "Outcome not established"
              : outcomes
                  .map(
                    (o) =>
                      `${o.metric.replaceAll("_", " ")}: ${o.assessment ? verdictLabels[o.assessment.verdict] || o.assessment.verdict : "Outcome not established"}`,
                  )
                  .join(" · ");
          return (
            <details key={item.intervention_id} className="py-3 text-xs">
              <summary className="cursor-pointer">
                <span className="font-medium">{item.title}</span>
                <span className="mt-1 block text-muted-foreground">
                  {statusLabels[item.status] || item.status}
                  {` · ${outcomeSummary}`}
                </span>
              </summary>
              <div className="mt-3 space-y-2 leading-5 text-muted-foreground">
                <p>
                  Measure: {item.metric.replaceAll("_", " ")} ·{" "}
                  {item.expected_direction}
                </p>
                <p>Baseline: {item.baseline_window || "Not recorded"}</p>
                <p>
                  Next assessment:{" "}
                  {assessment?.next_checkpoint ||
                    item.checkpoint ||
                    "Not recorded"}
                </p>
                {item.guardrails?.length ? (
                  <p>Guardrails: {item.guardrails.join("; ")}</p>
                ) : null}
                {item.rollback_condition && (
                  <p>Reconsider or revert when: {item.rollback_condition}</p>
                )}
                <PulseMetricOutcomes
                  item={item}
                  assessments={impact.assessments}
                />
                {item.provenance && (
                  <p className="break-words">
                    Application/evidence: {item.provenance}
                  </p>
                )}
              </div>
            </details>
          );
        })}
      </div>
      {items.length > 3 && (
        <button
          type="button"
          aria-expanded={all}
          onClick={() => setAll((value) => !value)}
          className="mt-2 text-xs font-medium text-primary"
        >
          {all
            ? "Show fewer improvements"
            : `View all ${items.length} improvements`}
        </button>
      )}
    </section>
  );
}
