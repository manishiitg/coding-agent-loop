import type {
  CostSummary,
  EvalResultRecord,
  WorkflowCostsResponse,
} from "../../../services/api-types";
import type { ReportCostOptions, ReportDataApi } from "./reportEmbedContext";

export interface ReportEvaluations {
  results: EvalResultRecord[];
  criteria: Array<{
    id: string;
    title: string;
    historical: boolean;
    latest: EvalResultRecord;
    history: EvalResultRecord[];
  }>;
  run_count: number;
  result_limit: number;
  possibly_truncated: boolean;
}
export async function getReportEvaluations(
  api: ReportDataApi,
): Promise<ReportEvaluations> {
  if (!api.getEvaluations)
    throw new Error("Evaluation data is unavailable in this report host");
  const response = await api.getEvaluations();
  if (!response.success || !Array.isArray(response.results))
    throw new Error(response.error || "Evaluation data unavailable");
  const results = [...response.results].sort(
    (a, b) =>
      (Date.parse(b.generated_at) || 0) - (Date.parse(a.generated_at) || 0) ||
      b.run_folder.localeCompare(a.run_folder),
  );
  const grouped = new Map<string, ReportEvaluations["criteria"][number]>();
  for (const result of results) {
    let criterion = grouped.get(result.step_id);
    if (!criterion) {
      criterion = {
        id: result.step_id,
        title: result.title || result.step_id,
        historical: !!result.historical,
        latest: result,
        history: [],
      };
      grouped.set(result.step_id, criterion);
    }
    criterion.history.push(result);
  }
  return {
    results,
    criteria: [...grouped.values()],
    run_count: new Set(results.map((r) => r.run_folder)).size,
    result_limit: 200,
    possibly_truncated: results.length >= 200,
  };
}

export interface ReportCosts {
  summary: CostSummary | null;
  history: WorkflowCostsResponse["history"];
  window_total_usd: number | undefined;
  state: "available" | "unavailable";
}
export function normalizeReportCostOptions(
  options: ReportCostOptions = {},
): Required<ReportCostOptions> {
  const days = options.days ?? 30,
    before = options.before ?? "";
  if (!Number.isInteger(days) || days < 1 || days > 90)
    throw new Error("Cost days must be an integer between 1 and 90");
  if (
    before &&
    (!/^\d{4}-\d{2}-\d{2}$/.test(before) ||
      !Number.isFinite(Date.parse(before)) ||
      new Date(before).toISOString().slice(0, 10) !== before)
  )
    throw new Error("Cost before must be a valid YYYY-MM-DD date");
  return { days, before };
}
export async function getReportCosts(
  api: ReportDataApi,
  options?: ReportCostOptions,
): Promise<ReportCosts> {
  const normalized = normalizeReportCostOptions(options);
  if (!api.getCosts)
    throw new Error("Cost data is unavailable in this report host");
  const response = await api.getCosts(normalized);
  if (!response.success) throw new Error("Cost data unavailable");
  const summary = response.scoped_costs || null;
  // The canonical endpoint deliberately returns ALL-TIME total/scope and
  // only the requested page of by_date/by_model. Never label its headline a period total.
  return {
    summary,
    history: response.history,
    window_total_usd: summary
      ? Object.values(summary.by_date || {}).reduce(
          (sum, day) => sum + day.total_cost_usd,
          0,
        )
      : undefined,
    state: summary ? "available" : "unavailable",
  };
}

const styles = `
:host{display:block;container-type:inline-size;font:14px/1.5 system-ui,sans-serif;color:inherit;overflow-wrap:anywhere}
*{box-sizing:border-box}h2,h3,p{margin:0}h2{font-size:18px;margin-bottom:16px}h3{font-size:15px}
.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin-top:12px}article{min-width:0;padding:20px;border:1px solid color-mix(in srgb,currentColor 18%,transparent);border-radius:14px}
.value{font-size:30px;font-weight:600;font-variant-numeric:tabular-nums;margin-top:12px}.meta{font-size:12px;opacity:.7;margin-top:8px}
.eyebrow{font-size:11px;text-transform:uppercase;letter-spacing:.07em;opacity:.65;margin-bottom:4px}
details{margin-top:16px}summary{cursor:pointer;font-size:12px;min-height:44px;padding:12px 0}details p{font-size:12px;white-space:pre-wrap;margin-top:10px}
.scroll{overflow:auto;overscroll-behavior-inline:contain;max-height:360px;margin-top:12px}table{width:100%;border-collapse:collapse;text-align:left;font-size:12px}th,td{padding:9px 14px 9px 0;vertical-align:top;border-bottom:1px solid color-mix(in srgb,currentColor 12%,transparent)}
.message{padding:20px;border:1px dashed color-mix(in srgb,currentColor 25%,transparent);border-radius:14px}
svg{height:120px;width:100%;color:#0ea5e9;display:block;margin-top:16px}.dates{display:flex;justify-content:space-between;font-size:11px;opacity:.7}
@container(max-width:800px){article,.message{padding:16px}.value{font-size:28px}}
@container(max-width:620px){.grid{grid-template-columns:1fr}}
`;
const versions = new WeakMap<Element, number>();
const number = (n: number) =>
  n.toLocaleString(undefined, { maximumFractionDigits: 2 });
const money = (n: number) =>
  new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  }).format(n);
const score = (r: EvalResultRecord) =>
  r.skipped
    ? "Skipped"
    : !r.score_captured || !Number.isFinite(r.score)
      ? "Score not captured"
      : r.max_score > 0
        ? `${number(r.score)} / ${number(r.max_score)}`
        : number(r.score);
function element<K extends keyof HTMLElementTagNameMap>(
  doc: Document,
  tag: K,
  text = "",
  cls = "",
) {
  const node = doc.createElement(tag);
  node.textContent = text;
  if (cls) node.className = cls;
  return node;
}
function table(
  doc: Document,
  label: string,
  headings: string[],
  rows: Array<Array<string | HTMLElement>>,
) {
  const el = <K extends keyof HTMLElementTagNameMap>(
    tag: K,
    text = "",
    cls = "",
  ) => element(doc, tag, text, cls);
  const scroll = el("div", "", "scroll"),
    grid = el("table"),
    head = el("thead"),
    tr = el("tr"),
    body = el("tbody");
  grid.append(el("caption", label));
  headings.forEach((h) => tr.append(el("th", h)));
  head.append(tr);
  rows.forEach((row) => {
    const line = el("tr");
    row.forEach((cell) => {
      const td = el("td");
      if (typeof cell === "string") td.textContent = cell;
      else td.append(cell);
      line.append(td);
    });
    body.append(line);
  });
  grid.append(head, body);
  scroll.append(grid);
  return scroll;
}
async function renderWidget<T>(
  doc: Document,
  target: string | HTMLElement,
  title: string,
  load: () => Promise<T>,
  render: (data: T, section: HTMLElement) => void,
): Promise<T> {
  const host =
    typeof target === "string"
      ? doc.querySelector<HTMLElement>(target)
      : target;
  if (
    !host ||
    host.ownerDocument !== doc ||
    typeof host.attachShadow !== "function"
  )
    throw new Error(`${title} needs a container in this report`);
  const version = (versions.get(host) || 0) + 1;
  versions.set(host, version);
  const root = host.shadowRoot || host.attachShadow({ mode: "open" });
  const show = (message: string) =>
    root.replaceChildren(
      element(doc, "style", styles),
      element(doc, "p", message, "message"),
    );
  host.setAttribute("aria-busy", "true");
  show(`Loading ${title.toLowerCase()}…`);
  try {
    const data = await load();
    if (versions.get(host) !== version) return data;
    const section = element(doc, "section");
    section.setAttribute("aria-label", title);
    section.append(element(doc, "h2", title));
    render(data, section);
    root.replaceChildren(element(doc, "style", styles), section);
    return data;
  } catch (error) {
    if (versions.get(host) === version)
      show(`${title} unavailable. Refresh the report to try again.`);
    throw error;
  } finally {
    if (versions.get(host) === version) host.setAttribute("aria-busy", "false");
  }
}
export function renderReportEvaluations(
  doc: Document,
  api: ReportDataApi,
  target: string | HTMLElement,
) {
  return renderWidget(
    doc,
    target,
    "Evaluations",
    () => getReportEvaluations(api),
    (data, section) => {
      const el = <K extends keyof HTMLElementTagNameMap>(
        tag: K,
        text = "",
        cls = "",
      ) => element(doc, tag, text, cls);
      if (!data.results.length) {
        section.append(
          el("p", "No evaluation results recorded yet.", "message"),
        );
        return;
      }
      section.append(
        el(
          "p",
          `Scores by criterion · ${data.run_count} ${data.run_count === 1 ? "run" : "runs"} in this history${data.possibly_truncated ? " · Latest 200 results; older history may exist" : ""}`,
          "meta",
        ),
      );
      const grid = el("div", "", "grid"),
        historical = el("details"),
        oldGrid = el("div", "", "grid");
      historical.append(el("summary", "Previous evaluation criteria"));
      for (const criterion of data.criteria) {
        const latest = criterion.latest,
          card = el("article");
        card.append(
          el("h3", criterion.title),
          el("p", score(latest), "value"),
          el(
            "p",
            `${latest.run_folder} · ${latest.generated_at ? new Date(latest.generated_at).toLocaleString() : "Date not recorded"}`,
            "meta",
          ),
        );
        if (latest.description)
          card.append(el("p", latest.description, "meta"));
        const details = el("details");
        details.append(el("summary", "Reasoning, evidence and history"));
        details.append(
          table(
            doc,
            "Recorded evaluations",
            ["Run", "Score", "Details"],
            criterion.history.map((r) => {
              const evidence = el("div");
              evidence.append(
                el("p", r.reasoning || "No reasoning recorded"),
                el("p", r.evidence || "No evidence recorded"),
              );
              return [`${r.run_folder}\n${r.generated_at}`, score(r), evidence];
            }),
          ),
        );
        card.append(details);
        (criterion.historical ? oldGrid : grid).append(card);
      }
      section.append(grid);
      if (oldGrid.childElementCount) {
        historical.append(oldGrid);
        section.append(historical);
      }
    },
  );
}
export function renderReportCosts(
  doc: Document,
  api: ReportDataApi,
  target: string | HTMLElement,
  options?: ReportCostOptions,
) {
  return renderWidget(
    doc,
    target,
    "Costs",
    () => getReportCosts(api, options),
    (data, section) => {
      const el = <K extends keyof HTMLElementTagNameMap>(
        tag: K,
        text = "",
        cls = "",
      ) => element(doc, tag, text, cls);
      if (!data.summary) {
        section.append(
          el(
            "p",
            "Cost ledger unavailable. No cost total can be shown.",
            "message",
          ),
        );
        return;
      }
      const s = data.summary,
        grid = el("div", "", "grid");
      const total = el("article"),
        period = el("article");
      total.append(
        el("p", "All-time recorded cost", "eyebrow"),
        el("p", money(s.total.total_cost_usd), "value"),
        el("p", `${number(s.total.call_count)} calls · USD`, "meta"),
      );
      period.append(
        el("p", "Selected window", "eyebrow"),
        el("p", money(data.window_total_usd!), "value"),
        el(
          "p",
          data.history
            ? `${data.history.window_from} – ${data.history.window_to} · UTC`
            : "Recorded daily history",
          "meta",
        ),
      );
      grid.append(total, period);
      section.append(grid);
      const days = Object.entries(s.by_date || {}).sort(([a], [b]) =>
        a.localeCompare(b),
      );
      if (days.length > 1) {
        const svg = doc.createElementNS("http://www.w3.org/2000/svg", "svg");
        svg.setAttribute("viewBox", "0 0 600 110");
        svg.setAttribute("preserveAspectRatio", "none");
        svg.setAttribute("role", "img");
        svg.setAttribute(
          "aria-label",
          "Daily recorded cost; exact values in daily history",
        );
        const high = Math.max(
            ...days.map(([, d]) => d.total_cost_usd),
            0.000001,
          ),
          start = Date.parse(days[0][0]),
          end = Date.parse(days.at(-1)![0]);
        const line = doc.createElementNS(svg.namespaceURI, "polyline");
        line.setAttribute(
          "points",
          days
            .map(
              ([date, d]) =>
                `${8 + ((Date.parse(date) - start) / (end - start)) * 584},${100 - (d.total_cost_usd / high) * 90}`,
            )
            .join(" "),
        );
        line.setAttribute("fill", "none");
        line.setAttribute("stroke", "currentColor");
        line.setAttribute("stroke-width", "2");
        line.setAttribute("vector-effect", "non-scaling-stroke");
        svg.append(line);
        const dates = el("div", "", "dates");
        dates.append(el("span", days[0][0]), el("span", days.at(-1)![0]));
        section.append(svg, dates);
      }
      if (!days.length)
        section.append(
          el("p", "No cost events recorded in this window.", "meta"),
        );
      for (const [label, headings, rows] of [
        [
          "Daily history · selected window (UTC)",
          ["Date", "Recorded cost", "Calls"],
          [...days]
            .reverse()
            .map(([date, d]) => [
              date,
              money(d.total_cost_usd),
              number(d.call_count),
            ]),
        ],
        [
          "Activity breakdown · all time",
          ["Activity", "Recorded cost", "Calls"],
          Object.entries(s.by_scope || {}).map(([scope, d]) => [
            scope.replaceAll("_", " "),
            money(d.total_cost_usd),
            number(d.call_count),
          ]),
        ],
        [
          "Model breakdown · selected window",
          ["Model", "Recorded cost", "Calls"],
          Object.entries(s.by_model || {}).map(([model, d]) => [
            model,
            money(d.total_cost_usd),
            number(d.call_count),
          ]),
        ],
      ] as Array<[string, string[], string[][]]>) {
        const details = el("details");
        details.append(el("summary", label), table(doc, label, headings, rows));
        section.append(details);
      }
      section.append(
        el("p", "Recorded USD amounts from the workflow cost ledger.", "meta"),
      );
      if (data.history?.has_more)
        section.append(
          el(
            "p",
            `Older daily history is available before ${data.history.next_before}.`,
            "meta",
          ),
        );
    },
  );
}
