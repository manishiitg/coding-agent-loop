// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { DeliveryTick } from "./DeliveryTick";

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
});

afterEach(() => {
  document.body.innerHTML = "";
});

function mount(metadata: Record<string, unknown> | undefined) {
  const host = document.createElement("div");
  document.body.appendChild(host);
  const root = createRoot(host);
  act(() => {
    root.render(<DeliveryTick metadata={metadata} />);
  });
  return host;
}

describe("DeliveryTick icons", () => {
  it.each([
    [{ confirmation: "confirmed" }, "lucide-check-check", "confirmed"],
    [{ delivery_status: "sent_to_cli" }, "lucide-check", "fast"],
    [{ confirmation: "accepted_but_unflushed" }, "lucide-clock", "unflushed"],
    [{ confirmation: "failed" }, "lucide-circle-alert", "failed"],
  ])("renders %s with the matching icon", (metadata, iconClass, state) => {
    const host = mount(metadata);
    const tick = host.querySelector('[data-testid="delivery-tick"]');
    expect(tick?.querySelector(`svg.${iconClass}`)).not.toBeNull();
    expect(tick?.getAttribute("data-state")).toBe(state);
  });

  it("renders nothing without delivery metadata", () => {
    expect(mount(undefined).querySelector('[data-testid="delivery-tick"]')).toBeNull();
    expect(mount({}).querySelector('[data-testid="delivery-tick"]')).toBeNull();
  });
});
