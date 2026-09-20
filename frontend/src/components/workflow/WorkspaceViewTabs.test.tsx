// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Puzzle } from "lucide-react";
import { WorkspaceViewTabs } from "./WorkspaceViewTabs";

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
});

afterEach(() => {
  document.body.innerHTML = "";
});

async function renderTabs(value: "apps" | "skills", onChange: (value: string) => void) {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <WorkspaceViewTabs
        value={value}
        onChange={onChange}
        options={[
          { value: "apps", label: "Apps" },
          { value: "skills", label: "Skills" },
        ]}
        ariaLabel="Integrations"
      />,
    ),
  );
  return container;
}

it("renders every option with the selected one marked", async () => {
  const host = await renderTabs("apps", () => {});
  expect(host.querySelector('[role="tablist"]')?.getAttribute("aria-label")).toBe("Integrations");
  const tabs = Array.from(host.querySelectorAll('[role="tab"]'));
  expect(tabs.map(tab => tab.textContent)).toEqual(["Apps", "Skills"]);
  expect(tabs[0].getAttribute("aria-selected")).toBe("true");
  expect(tabs[1].getAttribute("aria-selected")).toBe("false");
});

it("reports the picked option", async () => {
  const onChange = vi.fn();
  const host = await renderTabs("apps", onChange);
  const tabs = Array.from(host.querySelectorAll('[role="tab"]'));
  await act(async () => {
    tabs[1].dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  expect(onChange).toHaveBeenCalledWith("skills");
});

it("renders a tab icon when the option carries one", async () => {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <WorkspaceViewTabs
        value="apps"
        onChange={() => {}}
        options={[{ value: "apps", label: "Apps", icon: Puzzle }]}
        ariaLabel="Integrations"
      />,
    ),
  );
  const tab = container.querySelector('[role="tab"]')!;
  expect(tab.querySelector("svg")).not.toBeNull();
  expect(tab.textContent).toBe("Apps");
});
