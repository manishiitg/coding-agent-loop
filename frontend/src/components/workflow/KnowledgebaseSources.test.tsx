// @vitest-environment happy-dom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { KnowledgebaseSources } from "./KnowledgebaseSources";
import KnowledgebaseView from "./KnowledgebaseView";
import { workflowManifestApi, agentApi } from "../../services/api";
import { useCanWriteWorkflow } from "../../hooks/useCanWriteWorkflow";
vi.mock("../../services/api", () => ({
  workflowManifestApi: {
    getKnowledgebaseSources: vi.fn(),
    listWorkflowManifests: vi.fn(),
    updateWorkflowManifest: vi.fn(),
    readKnowledgebaseSource: vi.fn(),
  },
  agentApi: { getPlannerFileContent: vi.fn() },
}));
vi.mock("../../hooks/useCanWriteWorkflow", () => ({
  useCanWriteWorkflow: vi.fn(() => true),
}));
vi.mock("../ui/MarkdownRenderer", () => ({
  MarkdownRenderer: ({ content }: { content: string }) => <div>{content}</div>,
}));
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
const cleanups: Array<() => void> = [];
afterEach(() => {
  cleanups.splice(0).forEach((f) => f());
  vi.clearAllMocks();
  vi.mocked(useCanWriteWorkflow).mockReturnValue(true);
});
async function mount(node: React.ReactNode) {
  const host = document.createElement("div");
  document.body.append(host);
  const root = createRoot(host);
  await act(async () => root.render(node));
  cleanups.push(() => {
    act(() => root.unmount());
    host.remove();
  });
  return host;
}
const source = {
  workflow_id: "source",
  alias: "rts",
  access: "read" as const,
  label: "RTS",
  available: true,
};
it("shows several sources and detaches only the selected attachment", async () => {
  const other = {
    ...source,
    workflow_id: "security",
    alias: "security",
    label: "Security",
    available: false,
    reason: "Source unavailable",
  };
  vi.mocked(workflowManifestApi.getKnowledgebaseSources).mockResolvedValue({
    success: true,
    sources: [source, other],
  });
  vi.mocked(workflowManifestApi.updateWorkflowManifest).mockResolvedValue({
    success: true,
  });
  const select = vi.fn();
  const host = await mount(
    <KnowledgebaseSources
      workspacePath="Workflow/consumer"
      selected="rts"
      onSelect={select}
    />,
  );
  expect(host.querySelectorAll("option")).toHaveLength(3);
  expect(host.textContent).toContain("Source unavailable");
  await act(async () => {
    Array.from(host.querySelectorAll("button"))
      .find((b) => b.textContent === "Detach")!
      .click();
  });
  expect(workflowManifestApi.updateWorkflowManifest).toHaveBeenCalledWith({
    workspace_path: "Workflow/consumer",
    knowledgebase_sources: [
      { workflow_id: "security", alias: "security", access: "read" },
    ],
  });
  expect(select).toHaveBeenCalledWith("");
});
it("keeps management controls unavailable to readers", async () => {
  vi.mocked(useCanWriteWorkflow).mockReturnValue(false);
  vi.mocked(workflowManifestApi.getKnowledgebaseSources).mockResolvedValue({
    success: true,
    sources: [source],
  });
  const host = await mount(
    <KnowledgebaseSources
      workspacePath="Workflow/consumer"
      selected="rts"
      onSelect={() => {}}
    />,
  );
  expect(host.textContent).not.toContain("Detach");
  expect(host.textContent).not.toContain("Attach knowledge");
  expect(host.querySelector("select")).not.toBeNull();
});
it("reads shared notes through the consumer-scoped API and surfaces failed collection", async () => {
  vi.mocked(workflowManifestApi.getKnowledgebaseSources).mockResolvedValue({
    success: true,
    sources: [source],
  });
  vi.mocked(agentApi.getPlannerFileContent).mockResolvedValue({
    success: true,
    data: { content: '{"topics":[]}' },
  } as never);
  vi.mocked(workflowManifestApi.readKnowledgebaseSource).mockImplementation(
    async (_ws, _alias, path) => ({
      success: true,
      content:
        path === "notes/_index.json"
          ? '{"topics":[{"id":"architecture","file":"architecture.md"}]}'
          : path === "notes/architecture.md"
            ? "Verified shared architecture"
            : "{}",
    }),
  );
  const host = await mount(
    <KnowledgebaseView workspacePath="Workflow/consumer" />,
  );
  await act(async () => {
    const select = host.querySelector("select")!;
    select.value = "rts";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  expect(workflowManifestApi.readKnowledgebaseSource).toHaveBeenCalledWith(
    "Workflow/consumer",
    "rts",
    "notes/_index.json",
  );
  await act(async () => {
    Array.from(host.querySelectorAll("button"))
      .find((b) => b.textContent?.includes("architecture"))!
      .click();
  });
  expect(host.textContent).toContain("Verified shared architecture");
  vi.mocked(workflowManifestApi.readKnowledgebaseSource).mockRejectedValue(
    new Error("Source permission revoked"),
  );
  await act(async () => {
    const refresh = host.querySelector(
      'button[title="Refresh"]',
    ) as HTMLButtonElement | null;
    if (refresh) refresh.click();
    else {
      const select = host.querySelector("select")!;
      select.value = "";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    }
  });
  // Re-selecting always remounts the source, discarding cached notes.
  await act(async () => {
    const select = host.querySelector("select")!;
    select.value = "rts";
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
  expect(host.textContent).toContain("Source permission revoked");
});

it("attaches another workflow while preserving existing sources", async () => {
  vi.mocked(workflowManifestApi.getKnowledgebaseSources).mockResolvedValue({
    success: true,
    sources: [source],
  });
  vi.mocked(workflowManifestApi.listWorkflowManifests).mockResolvedValue({
    success: true,
    workflows: [
      {
        workspace_path: "Workflow/security",
        manifest: { id: "security", label: "Security" },
      },
    ],
  } as never);
  vi.mocked(workflowManifestApi.updateWorkflowManifest).mockResolvedValue({
    success: true,
  });
  const host = await mount(
    <KnowledgebaseSources
      workspacePath="Workflow/consumer"
      selected=""
      onSelect={() => {}}
    />,
  );
  await act(async () => {
    Array.from(host.querySelectorAll("button"))
      .find((b) => b.textContent === "Attach knowledge")!
      .click();
  });
  await act(async () => {
    const select = host.querySelector("form select")! as HTMLSelectElement;
    select.value = "security";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    const input = host.querySelector("input")!;
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )!.set!.call(input, "security");
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await act(async () => {
    host
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  expect(workflowManifestApi.updateWorkflowManifest).toHaveBeenCalledWith({
    workspace_path: "Workflow/consumer",
    knowledgebase_sources: [
      { workflow_id: "source", alias: "rts", access: "read" },
      { workflow_id: "security", alias: "security", access: "read" },
    ],
  });
});
