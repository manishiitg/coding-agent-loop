import { renderToStaticMarkup } from "react-dom/server";
import { expect, it, vi } from "vitest";
import { GoalProgress } from "./GoalProgress";
vi.mock("./AskAIButton", () => ({
  AskAIButton: ({ label }: { label: string }) => <button>{label}</button>,
}));
it("leads with the primary metric, a real value, target and observation date", () => {
  const html = renderToStaticMarkup(
    <GoalProgress
      workspacePath="Workflow/example"
      impact={{
        interventions: [],
        assessments: [],
        metrics: [
          {
            id: "followers",
            criterion_id: "audience",
            name: "Followers",
            role: "primary",
            unit: "followers",
            direction: "increase",
            definition: "Total followers",
            source: "daily_metrics",
            window: "instant",
            route: "",
            environment: "",
            collection_frequency: "daily",
            freshness_hours: 48,
            target: 1000,
          },
        ],
        observations: [
          {
            observation_id: "1",
            criterion_id: "audience",
            metric: "followers",
            unit: "followers",
            run_id: "run-1",
            value: 290,
            observed_at: "2026-09-09T00:00:00Z",
          },
        ],
      }}
    />,
  );
  expect(html).toContain("Primary metric");
  expect(html).toContain("290");
  expect(html).toContain("1,000");
  expect(html).toContain("Last observed");
  expect(html).toContain("Measurement details and history");
});
it("shows setup without inventing a score for legacy workflows", () => {
  const html = renderToStaticMarkup(
    <GoalProgress
      workspacePath="Workflow/example"
      impact={{ interventions: [], observations: [], assessments: [] }}
    />,
  );
  expect(html).toContain("Measurement setup needed");
  expect(html).toContain("Set up goals &amp; metrics");
  expect(html).not.toContain("0%");
});
