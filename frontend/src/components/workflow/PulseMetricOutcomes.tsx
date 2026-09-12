import type {
  PulseImpactAssessment,
  PulseIntervention,
} from "../../services/api-types";

export const verdictLabels: Record<string, string> = {
  improved: "Improved",
  unchanged: "No measured improvement",
  regressed: "Regressed",
  inconclusive: "Not enough evidence",
  confounded: "Effect could not be isolated",
};
export function pulseMetricOutcomes(
  item: PulseIntervention,
  assessments: PulseImpactAssessment[],
) {
  const history = assessments
    .filter((a) => a.intervention_id === item.intervention_id)
    .sort((a, b) => Date.parse(b.assessed_at) - Date.parse(a.assessed_at));
  return [
    { metric: item.metric, expected_direction: item.expected_direction },
    ...(item.effects || []),
  ].map((effect) => ({
    ...effect,
    assessment: history.find(
      (a) => (a.metric || item.metric) === effect.metric,
    ),
  }));
}

export function PulseMetricOutcomes({
  item,
  assessments,
}: {
  item: PulseIntervention;
  assessments: PulseImpactAssessment[];
}) {
  return (
    <div className="space-y-2">
      {pulseMetricOutcomes(item, assessments).map(
        ({ metric, expected_direction, assessment }) => (
          <div key={metric} className="border-l-2 pl-2">
            <p>
              {metric.replaceAll("_", " ")} · {expected_direction} ·{" "}
              {assessment
                ? verdictLabels[assessment.verdict] || assessment.verdict
                : "Outcome not established"}
            </p>
            {assessment && (
              <>
                <p>
                  {assessment.before_value ?? "Unknown"} →{" "}
                  {assessment.after_value ?? "Unknown"} ·{" "}
                  {assessment.confidence} confidence
                </p>
                <p>
                  Measured {assessment.assessed_at} · {assessment.before_window}{" "}
                  → {assessment.after_window}
                </p>
                {(assessment.evidence || []).map((evidence, index) => (
                  <p key={index} className="break-words">
                    {evidence}
                  </p>
                ))}
                {assessment.confounders?.length ? (
                  <p>Other influences: {assessment.confounders.join("; ")}</p>
                ) : null}
                {assessment.next_checkpoint && (
                  <p>Next assessment: {assessment.next_checkpoint}</p>
                )}
              </>
            )}
          </div>
        ),
      )}
    </div>
  );
}
