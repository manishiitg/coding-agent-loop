// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ConfirmationDialog from "./ConfirmationDialog";

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
});

afterEach(() => {
  document.body.innerHTML = "";
});

async function renderDialog(props: { onConfirm: () => void; requireText?: string }) {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <ConfirmationDialog
        isOpen
        onClose={() => {}}
        onConfirm={props.onConfirm}
        title="Delete Demo?"
        message="This cannot be undone."
        confirmText="Delete workflow"
        requireText={props.requireText}
      />,
    ),
  );
  return container;
}

function confirmButton() {
  return Array.from(document.body.querySelectorAll("button")).find(
    button => button.textContent === "Delete workflow",
  )!;
}

async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")!.set!;
    setter.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

it("confirms immediately without required text", async () => {
  const onConfirm = vi.fn();
  await renderDialog({ onConfirm });
  expect(document.body.querySelector('input[aria-label="Confirmation text"]')).toBeNull();
  expect(confirmButton().disabled).toBe(false);
  await act(async () => {
    confirmButton().dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  expect(onConfirm).toHaveBeenCalledTimes(1);
});

it("enables confirm only after the exact text is typed", async () => {
  const onConfirm = vi.fn();
  await renderDialog({ onConfirm, requireText: "Demo" });
  const input = document.body.querySelector('input[aria-label="Confirmation text"]') as HTMLInputElement;
  expect(confirmButton().disabled).toBe(true);
  await type(input, "Dem");
  expect(confirmButton().disabled).toBe(true);
  await type(input, "Demo");
  expect(confirmButton().disabled).toBe(false);
  await act(async () => {
    confirmButton().dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  expect(onConfirm).toHaveBeenCalledTimes(1);
});
