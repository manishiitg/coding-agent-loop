// @vitest-environment happy-dom
import { expect, it, vi } from "vitest";
import {
  getReportCosts,
  getReportEvaluations,
  renderReportCosts,
  renderReportEvaluations,
} from "./reportOperationalMetrics";
import { installReportHost, withReportBootstrap } from "./reportHostRuntime";
import type { ReportDataApi } from "./reportEmbedContext";
import type {
  CostAggregate,
  EvalResultRecord,
} from "../../../services/api-types";
const aggregate = (cost: number): CostAggregate => ({
  total_cost_usd: cost,
  call_count: 3,
  prompt_tokens: 100,
  completion_tokens: 30,
  reasoning_tokens: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
});
const result = (
  overrides: Partial<EvalResultRecord> = {},
): EvalResultRecord => ({
  run_folder: "run-2",
  step_id: "quality",
  title: "Output quality",
  score: 0,
  max_score: 10,
  score_captured: true,
  skipped: false,
  reasoning: "Checked the output",
  evidence: "db/proof.json",
  generated_at: "2026-09-11T10:00:00Z",
  ...overrides,
});
function api(): ReportDataApi {
  return {
    workspacePath: "Workflow/test",
    getEvaluations: vi.fn(async () => ({
      success: true,
      results: [
        result(),
        result({
          run_folder: "run-1",
          score: 9,
          generated_at: "2026-09-10T10:00:00Z",
        }),
      ],
    })),
    getCosts: vi.fn(async () => ({
      success: true,
      runs: [],
      phase_daily_costs: [],
      scoped_costs: {
        total: aggregate(100),
        by_date: { "2026-09-11": aggregate(2), "2026-09-10": aggregate(3) },
        by_scope: { pulse: aggregate(100) },
        by_model: { model: aggregate(5) },
      },
      history: {
        days: 30,
        window_from: "2026-08-13",
        window_to: "2026-09-11",
        has_more: true,
        next_before: "2026-08-13",
      },
    })),
  } as unknown as ReportDataApi;
}
function host() {
  const node = document.createElement("section");
  document.body.replaceChildren(node);
  return node;
}
it("keeps criteria separate, with latest results and explicit history bounds", async () => {
  const data = api();
  data.getEvaluations = async () => ({
    success: true,
    results: [
      result({ step_id: "old", historical: true }),
      result({ score_captured: false }),
      result({
        run_folder: "run-1",
        score: 9,
        generated_at: "2026-09-10T10:00:00Z",
      }),
    ],
  });
  const evaluations = await getReportEvaluations(data);
  expect(evaluations.criteria).toHaveLength(2);
  expect(
    evaluations.criteria.find((c) => c.id === "quality")!.latest.score_captured,
  ).toBe(false);
  expect(evaluations.run_count).toBe(2);
  expect(evaluations.possibly_truncated).toBe(false);
  const container = host();
  await renderReportEvaluations(document, data, container);
  const css = container.shadowRoot!.querySelector("style")!.textContent;
  expect(css).toContain("@container(max-width:800px)");
  expect(css).toContain("min-height:44px");
  expect(container.shadowRoot!.textContent).toContain("Score not captured");
  expect(container.shadowRoot!.textContent).toContain(
    "Previous evaluation criteria",
  );
  expect(container.shadowRoot!.textContent).toContain("db/proof.json");
});
it("shows real zero scores and skips without inventing pass/fail thresholds", async () => {
  const data = api(),
    container = host();
  await renderReportEvaluations(document, data, container);
  expect(container.shadowRoot!.querySelector(".value")!.textContent).toBe(
    "0 / 10",
  );
  data.getEvaluations = async () => ({
    success: true,
    results: [result({ skipped: true })],
  });
  await renderReportEvaluations(document, data, container);
  expect(container.shadowRoot!.querySelector(".value")!.textContent).toBe(
    "Skipped",
  );
  expect(container.shadowRoot!.querySelectorAll("article")).toHaveLength(1);
});
it("distinguishes all-time totals from paged daily totals and preserves cost cursor", async () => {
  const data = api(),
    costs = await getReportCosts(data, { days: 7, before: "2026-09-12" });
  expect(costs.summary!.total.total_cost_usd).toBe(100);
  expect(costs.window_total_usd).toBe(5);
  expect(costs.history!.next_before).toBe("2026-08-13");
  expect(data.getCosts).toHaveBeenCalledWith({ days: 7, before: "2026-09-12" });
  const container = host();
  await renderReportCosts(document, data, container);
  expect(container.shadowRoot!.textContent).toContain("All-time recorded cost");
  expect(container.shadowRoot!.textContent).toContain("$100.00");
  expect(container.shadowRoot!.textContent).toContain("$5.00");
  expect(container.shadowRoot!.querySelector("svg")).not.toBeNull();
});
it("rejects invalid date windows before fetching and distinguishes zero from unavailable", async () => {
  const data = api();
  for (const options of [
    { days: 0 },
    { days: 91 },
    { days: 1.5 },
    { before: "2026-02-30" },
  ])
    await expect(getReportCosts(data, options)).rejects.toThrow();
  expect(data.getCosts).not.toHaveBeenCalled();
  data.getCosts = async () => ({
    success: true,
    runs: [],
    phase_daily_costs: [],
  });
  expect((await getReportCosts(data)).window_total_usd).toBeUndefined();
  const container = host();
  await renderReportCosts(document, data, container);
  expect(container.shadowRoot!.textContent).toContain(
    "Cost ledger unavailable",
  );
});
it("surfaces failures and does not overwrite newer refreshes", async () => {
  const data = api(),
    container = host();
  let release!: (
    v: Awaited<ReturnType<NonNullable<ReportDataApi["getCosts"]>>>,
  ) => void;
  data.getCosts = () =>
    new Promise((resolve) => {
      release = resolve;
    });
  const pending = renderReportCosts(document, data, container);
  await renderReportCosts(document, api(), container);
  release({ success: true, runs: [], phase_daily_costs: [] });
  await pending;
  expect(container.shadowRoot!.textContent).toContain("$100.00");
  data.getCosts = async () => {
    throw new Error("Offline");
  };
  await expect(renderReportCosts(document, data, container)).rejects.toThrow(
    "Offline",
  );
  expect(container.shadowRoot!.textContent).toContain("Costs unavailable");
  expect(container.getAttribute("aria-busy")).toBe("false");
});
it("replays all four report methods called before host injection", async () => {
  const frame = document.createElement("iframe");
  document.body.replaceChildren(frame);
  const doc = frame.contentDocument!,
    win = frame.contentWindow!;
  doc.body.innerHTML =
    '<section id="eval"></section><section id="cost"></section>';
  const stub = withReportBootstrap("").match(
    /<script>([\s\S]*?)<\/script>/,
  )![1];
  new Function("window", "document", stub)(win, doc);
  const report = (
    win as unknown as {
      report: Record<string, (...args: unknown[]) => Promise<unknown>>;
    }
  ).report;
  const calls = [
    report.getCosts({ days: 7 }),
    report.getEvaluations(),
    report.renderCosts("#cost"),
    report.renderEvaluations("#eval"),
  ];
  installReportHost(frame, {
    dataApi: api(),
    title: "Test",
    theme: "light",
    tokenSource: null,
    dispatchData: true,
  });
  await Promise.all(calls);
  expect(doc.querySelector("#cost")!.shadowRoot!.textContent).toContain(
    "$100.00",
  );
  expect(doc.querySelector("#eval")!.shadowRoot!.textContent).toContain(
    "Output quality",
  );
});
