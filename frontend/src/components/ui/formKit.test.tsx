// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Switch } from "./Switch";
import { ToggleRow } from "./ToggleRow";
import { SecretField } from "./SecretField";
import { FormSection } from "./FormSection";

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
});

afterEach(() => {
  document.body.innerHTML = "";
});

async function render(node: React.ReactNode) {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () => root.render(node));
  return container;
}

async function click(element: Element) {
  await act(async () => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

it("switch exposes its state and reports toggles", async () => {
  const onCheckedChange = vi.fn();
  const host = await render(<Switch checked={false} onCheckedChange={onCheckedChange} aria-label="Enable" />);
  const button = host.querySelector('[role="switch"]')!;
  expect(button.getAttribute("aria-checked")).toBe("false");
  await click(button);
  expect(onCheckedChange).toHaveBeenCalledWith(true);
});

it("switch stays silent when disabled", async () => {
  const onCheckedChange = vi.fn();
  const host = await render(<Switch checked={false} onCheckedChange={onCheckedChange} disabled aria-label="Enable" />);
  await click(host.querySelector('[role="switch"]')!);
  expect(onCheckedChange).not.toHaveBeenCalled();
});

it("toggle row pairs a label with the switch", async () => {
  const onCheckedChange = vi.fn();
  const host = await render(
    <ToggleRow label="Enable Slack bot" description="Platform switch" checked onCheckedChange={onCheckedChange} />,
  );
  expect(host.textContent).toContain("Enable Slack bot");
  expect(host.querySelector('[role="switch"]')?.getAttribute("aria-checked")).toBe("true");
  await click(host.querySelector('[role="switch"]')!);
  expect(onCheckedChange).toHaveBeenCalledWith(false);
});

it("secret field reveals on demand and keeps its hint", async () => {
  const host = await render(
    <SecretField label="Bot Token" required hint="Starts with xoxb-" value="secret" onChange={() => {}} />,
  );
  const input = host.querySelector("input")!;
  expect(input.getAttribute("type")).toBe("password");
  expect(host.textContent).toContain("Starts with xoxb-");
  await click(host.querySelector('button[aria-label="Show Bot Token"]')!);
  expect(input.getAttribute("type")).toBe("text");
});

it("form section lays out title, description, actions, and body", async () => {
  const host = await render(
    <FormSection title="Connection" description="Shared by all." actions={<button type="button">Add</button>}>
      <p>Body</p>
    </FormSection>,
  );
  expect(host.querySelector("h3")?.textContent).toBe("Connection");
  expect(host.textContent).toContain("Shared by all.");
  expect(host.textContent).toContain("Body");
});
