// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { EntityIdentityIcon } from "./EntityIdentityIcon";
import { IconUploadField } from "./IconUploadField";

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

const IMAGE = "data:image/png;base64,AAAA";

it("entity icon renders an uploaded image instead of text", async () => {
  const host = await render(<EntityIdentityIcon icon={IMAGE} label="Nova" />);
  const img = host.querySelector("img");
  expect(img?.getAttribute("src")).toBe(IMAGE);
  expect(host.textContent).not.toContain("AAAA");
});

it("entity icon keeps emoji and initial badges", async () => {
  const emoji = await render(<EntityIdentityIcon icon="🚀" label="Nova" />);
  expect(emoji.querySelector("img")).toBeNull();
  expect(emoji.textContent).toContain("🚀");
  const initial = await render(<EntityIdentityIcon label="Nova" />);
  expect(initial.textContent).toContain("N");
});

it("icon field edits emoji text and caps it at eight characters", async () => {
  const onChange = vi.fn();
  const host = await render(<IconUploadField value="" onChange={onChange} label="Nova" inputAriaLabel="Project icon" />);
  const input = host.querySelector('input[aria-label="Project icon"]') as HTMLInputElement;
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")?.set;
    setter?.call(input, "🚀".repeat(12));
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  expect(onChange).toHaveBeenCalledWith("🚀".repeat(8));
});

it("icon field removes an uploaded image", async () => {
  const onChange = vi.fn();
  const host = await render(<IconUploadField value={IMAGE} onChange={onChange} label="Nova" inputAriaLabel="Project icon" />);
  expect(host.querySelector("img")).not.toBeNull();
  const remove = Array.from(host.querySelectorAll("button")).find(button => button.textContent === "Remove");
  expect(remove).not.toBeUndefined();
  await click(remove!);
  expect(onChange).toHaveBeenCalledWith("");
});

it("icon field rejects a non-image upload with plain copy", async () => {
  const onChange = vi.fn();
  const host = await render(<IconUploadField value="" onChange={onChange} label="Nova" inputAriaLabel="Project icon" />);
  const fileInput = host.querySelector('input[aria-label="Upload icon image"]') as HTMLInputElement;
  const file = new File(["hello"], "notes.txt", { type: "text/plain" });
  await act(async () => {
    Object.defineProperty(fileInput, "files", { value: [file], configurable: true });
    fileInput.dispatchEvent(new Event("change", { bubbles: true }));
  });
  for (let i = 0; i < 10 && !host.textContent?.includes("Choose an image file"); i++) {
    await act(async () => {});
  }
  expect(onChange).not.toHaveBeenCalled();
  expect(host.textContent).toContain("Choose an image file");
});
