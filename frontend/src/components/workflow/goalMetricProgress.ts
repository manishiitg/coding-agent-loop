import type {
  GoalMetric,
  PulseGoalObservation,
} from "../../services/api-types";

// An environment/window/unit change must never silently become a trend.
export function goalMetricProgress(
  metric: GoalMetric,
  observations: PulseGoalObservation[],
  now = Date.now(),
) {
  const history = observations
    .filter(
      (o) =>
        o.metric === metric.id &&
        o.criterion_id === metric.criterion_id &&
        o.unit === metric.unit &&
        (o.route || "") === (metric.route || "") &&
        (o.environment || "") === (metric.environment || "") &&
        Number.isFinite(Date.parse(o.observed_at)),
    )
    .sort((a, b) => Date.parse(a.observed_at) - Date.parse(b.observed_at));
  const latest = history.at(-1);
  const numeric = history.filter(
    (o) =>
      (!o.status || o.status === "ok") &&
      typeof o.value === "number" &&
      Number.isFinite(o.value),
  );
  const current =
    latest &&
    (!latest.status || latest.status === "ok") &&
    typeof latest.value === "number" &&
    Number.isFinite(latest.value)
      ? latest.value
      : undefined;
  const previous = current !== undefined ? numeric.at(-2)?.value : undefined;
  const delta =
    current !== undefined && previous !== undefined
      ? current - previous
      : undefined;
  const stale =
    !!latest &&
    now - Date.parse(latest.observed_at) > metric.freshness_hours * 3600000;
  const targetMet =
    current !== undefined &&
    metric.target !== undefined &&
    !stale &&
    (metric.direction === "increase"
      ? current >= metric.target
      : metric.direction === "decrease"
        ? current <= metric.target
        : current === metric.target);
  const state = !latest
    ? "Measurement setup needed"
    : current === undefined
      ? "Measurement unavailable"
      : stale
        ? "Measurement stale"
        : targetMet
          ? "Target met"
          : numeric.length < 2
            ? "Baseline collecting"
            : "Tracking progress";
  return { history, numeric, latest, current, delta, stale, targetMet, state };
}
