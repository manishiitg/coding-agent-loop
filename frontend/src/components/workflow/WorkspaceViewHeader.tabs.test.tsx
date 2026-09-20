// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Puzzle } from "lucide-react";
import { WorkspaceViewHeader } from "./WorkspaceViewHeader";

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
});

afterEach(() => {
  document.body.innerHTML = "";
});

async function renderHeader(onChange: (value: string) => void) {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <WorkspaceViewHeader
        icon={<Puzzle className="h-4 w-4 text-primary" />}
        title="Integrations"
        subtitle="Pick what this workflow may use."
        below={<div data-testid="below" />}
        tabs={{
          value: "apps",
          onChange,
          options: [
            { value: "apps", label: "Apps" },
            { value: "skills", label: "Skills" },
          ],
          ariaLabel: "Integrations",
        }}
      />,
    ),
  );
  return container;
}

it("renders the standard tab row after below with the selected option marked", async () => {
  const host = await renderHeader(() => {});
  const below = host.querySelector('[data-testid="below"]')!;
  const tablist = host.querySelector('[role="tablist"]')!;
  expect(below.compareDocumentPosition(tablist) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  const tabs = Array.from(host.querySelectorAll('[role="tab"]'));
  expect(tabs.map(tab => tab.textContent)).toEqual(["Apps", "Skills"]);
  expect(tabs[0].getAttribute("aria-selected")).toBe("true");
});

it("reports the picked tab", async () => {
  const onChange = vi.fn();
  const host = await renderHeader(onChange);
  const tabs = Array.from(host.querySelectorAll('[role="tab"]'));
  await act(async () => {
    tabs[1].dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  expect(onChange).toHaveBeenCalledWith("skills");
});
