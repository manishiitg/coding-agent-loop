// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

vi.mock("./PulseWorkspace", () => ({
  PulseWorkspace: () => <div data-testid="pulse-workspace" />,
}));
vi.mock("./SoulViewer", () => ({
  WORKFLOW_SOUL_REFRESH_EVENT: "test-soul-refresh",
}));

import PulseView from "./PulseView";

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
});

afterEach(() => {
  document.body.innerHTML = "";
});

async function renderView(monitorOn: boolean) {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <PulseView
        workspacePath="/tmp/workflow-test"
        monitorOn={monitorOn}
        monitorSaving={false}
        onToggleMonitor={() => {}}
        disabledReviewModules={[]}
        reviewModuleSaving={null}
        onToggleReviewModule={() => {}}
        moduleStates={[]}
        planDriftDue={false}
        planDriftDueItems={[]}
        planDriftDueError={null}
        finalCommandStates={[]}
        reviewFocuses={[]}
        reviewFocusSelections={[]}
        statusError={null}
        statusLoading={false}
        overview={{ recorded: 0, total: 8, latest: "" }}
        onRefresh={() => {}}
        headerAction={<button type="button" data-testid="ask-ai">Ask AI</button>}
      />,
    ),
  );
  return container;
}

it.each([true, false])("shows the standard refresh with monitor %s", async (monitorOn) => {
  const host = await renderView(monitorOn);
  const refresh = host.querySelector('[aria-label="Refresh Pulse status"]');
  expect(refresh).not.toBeNull();
  expect(refresh?.className).toContain("h-8 w-8");
  // Ask AI left, refresh right.
  const askIndex = host.innerHTML.indexOf('data-testid="ask-ai"');
  const refreshIndex = host.innerHTML.indexOf('aria-label="Refresh Pulse status"');
  expect(askIndex).toBeGreaterThanOrEqual(0);
  expect(askIndex).toBeLessThan(refreshIndex);
});
