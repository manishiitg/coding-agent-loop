import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { PulseImprovements } from "./PulseImprovements";
import type { PulseImpactLedger } from "../../services/api-types";

it("distinguishes approval, application and measured outcome without inventing impact", () => {
  const impact: PulseImpactLedger = {
    observations: [],
    assessments: [],
    interventions: [
      {
        intervention_id: "one",
        title: "Simpler prompts",
        kind: "architecture_improvement",
        criterion_id: "sc-1",
        metric: "latency",
        impact_type: "reliability",
        expected_direction: "decrease",
        minimum_evidence_runs: 1,
        status: "approved",
      },
    ],
  };
  let html = renderToStaticMarkup(<PulseImprovements impact={impact} />);
  expect(html).toContain("Approved · awaiting application");
  expect(html).toContain("Outcome not established");
  expect(html).not.toContain("Applied ·");
  impact.interventions[0].status = "running";
  html = renderToStaticMarkup(<PulseImprovements impact={impact} />);
  expect(html).toContain("Applied · awaiting outcomes");
  expect(html).toContain("Outcome not established");
  impact.assessments.push({
    assessment_id: "proof",
    intervention_id: "one",
    verdict: "inconclusive",
    before_window: "before",
    after_window: "after",
    confidence: "low",
    assessed_at: "2026-09-10",
    evidence: ["Need comparable samples"],
  });
  html = renderToStaticMarkup(<PulseImprovements impact={impact} />);
  expect(html).toContain("Not enough evidence");
  expect(html).not.toContain(" · Improved");
  expect(html).toContain("Unknown → Unknown");
});

it("shows a cost regression and missing security evidence beside a latency improvement", () => {
  const impact: PulseImpactLedger = {
    observations: [],
    assessments: [],
    interventions: [
      {
        intervention_id: "multi",
        title: "Faster responses",
        criterion_id: "speed",
        metric: "latency",
        expected_direction: "decrease",
        impact_type: "direct_goal",
        minimum_evidence_runs: 1,
        status: "measuring",
        effects: [
          { metric: "cost", expected_direction: "maintain" },
          { metric: "security", expected_direction: "maintain" },
        ],
      },
    ],
  };
  impact.assessments = [
    {
      assessment_id: "cost",
      intervention_id: "multi",
      metric: "cost",
      verdict: "regressed",
      before_window: "before",
      after_window: "after",
      confidence: "high",
      assessed_at: "2026-09-11T00:00:00Z",
      before_value: 10,
      after_value: 20,
    },
    {
      assessment_id: "speed",
      intervention_id: "multi",
      metric: "latency",
      verdict: "improved",
      before_window: "before",
      after_window: "after",
      confidence: "high",
      assessed_at: "2026-09-12T00:00:00Z",
      before_value: 100,
      after_value: 80,
    },
  ];
  const html = renderToStaticMarkup(<PulseImprovements impact={impact} />);
  expect(html).toContain("latency: Improved");
  expect(html).toContain("cost: Regressed");
  expect(html).toContain("security: Outcome not established");
  expect(html).toContain("10 → 20");
});

it("can isolate Strategic Review proposals from platform improvements", () => {
  const impact: PulseImpactLedger = {
    observations: [],
    assessments: [],
    interventions: [
      {
        intervention_id: "strategy",
        title: "Test a concierge onboarding path",
        kind: "strategy_experiment",
        criterion_id: "activation",
        metric: "activation_rate",
        impact_type: "direct_goal",
        expected_direction: "increase",
        minimum_evidence_runs: 2,
        status: "proposed",
      },
      {
        intervention_id: "architecture",
        title: "Consolidate duplicated prompts",
        kind: "architecture_improvement",
        criterion_id: "latency",
        metric: "latency",
        impact_type: "reliability",
        expected_direction: "decrease",
        minimum_evidence_runs: 2,
        status: "approved",
      },
    ],
  };
  const strategy = renderToStaticMarkup(<PulseImprovements impact={impact} kinds={["strategy_experiment"]}
    title="Strategic proposals" description="Strategy only" emptyMessage="Nothing proposed" />);
  expect(strategy).toContain("Strategic proposals");
  expect(strategy).toContain("Test a concierge onboarding path");
  expect(strategy).not.toContain("Consolidate duplicated prompts");

  const platform = renderToStaticMarkup(<PulseImprovements impact={impact} kinds={["fix_bundle", "architecture_improvement"]}
    title="Platform improvements" />);
  expect(platform).toContain("Consolidate duplicated prompts");
  expect(platform).not.toContain("Test a concierge onboarding path");
});
