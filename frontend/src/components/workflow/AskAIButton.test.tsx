// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AskAIButton } from "./AskAIButton";
import { TooltipProvider } from "../ui/tooltip";

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

async function renderButton(onAsk: (message: string) => void, iconOnly = false) {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <TooltipProvider>
        <AskAIButton workspacePath={null} message="hello builder" onAsk={onAsk} iconOnly={iconOnly} />
      </TooltipProvider>,
    ),
  );
  const button = container.querySelector("button")!;
  const click = async () => {
    await act(async () => {
      button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
  };
  const hover = async () => {
    await act(async () => {
      // React synthesizes onMouseEnter from mouseover.
      button.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
    });
  };
  return {
    container,
    button,
    click,
    hover,
    unmount: async () => {
      await act(async () => root.unmount());
      container.remove();
    },
  };
}

function stubHoverCapable(matches: boolean) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: vi.fn().mockReturnValue({ matches, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
  });
}

it("sends only on a deliberate second click", async () => {
  const onAsk = vi.fn();
  const ui = await renderButton(onAsk);
  try {
    expect(ui.button.textContent).toBe("Ask AI");

    // First click arms, never sends.
    await ui.click();
    expect(onAsk).not.toHaveBeenCalled();
    expect(ui.button.textContent).toBe("Sure?");

    // The second half of a double-click is too fast to be deliberate.
    await ui.click();
    expect(onAsk).not.toHaveBeenCalled();

    await act(async () => {
      vi.advanceTimersByTime(700);
    });
    await ui.click();
    await act(async () => {});
    expect(onAsk).toHaveBeenCalledExactlyOnceWith("hello builder");
    expect(ui.button.textContent).toBe("Sent!");
    await act(async () => {
      vi.advanceTimersByTime(1900);
    });
    expect(ui.button.textContent).toBe("Ask AI");
  } finally {
    await ui.unmount();
  }
});

it("shows no Sent! state when delivery fails", async () => {
  const onAsk = vi.fn().mockRejectedValue(new Error("no chat"));
  const ui = await renderButton(onAsk);
  try {
    await ui.click();
    await act(async () => {
      vi.advanceTimersByTime(700);
    });
    await ui.click();
    await act(async () => {});
    expect(onAsk).toHaveBeenCalledTimes(1);
    expect(ui.button.textContent).toBe("Ask AI");
  } finally {
    await ui.unmount();
  }
});

it("expands the icon on hover and only then accepts clicks", async () => {
  stubHoverCapable(true);
  const onAsk = vi.fn();
  const ui = await renderButton(onAsk, true);
  try {
    const label = () => ui.button.querySelector("span");

    // Collapsed: the label takes no space.
    expect(label()?.className).toContain("w-0");

    // A click that arrives without a hover only expands.
    await ui.click();
    expect(onAsk).not.toHaveBeenCalled();
    expect(label()?.className).toContain("w-auto");
    expect(ui.button.textContent).toContain("Ask AI");

    // Now the normal arm/confirm flow applies.
    await ui.click();
    expect(ui.button.textContent).toContain("Sure?");
    await act(async () => {
      vi.advanceTimersByTime(700);
    });
    await ui.click();
    await act(async () => {});
    expect(onAsk).toHaveBeenCalledExactlyOnceWith("hello builder");
    expect(ui.button.textContent).toContain("Sent!");
  } finally {
    await ui.unmount();
  }
});

it("reveals the label on hover before any click", async () => {
  stubHoverCapable(true);
  const onAsk = vi.fn();
  const ui = await renderButton(onAsk, true);
  try {
    await ui.hover();
    expect(ui.button.textContent).toContain("Ask AI");
    await ui.click();
    expect(onAsk).not.toHaveBeenCalled();
    expect(ui.button.textContent).toContain("Sure?");
  } finally {
    await ui.unmount();
  }
});

it("skips the hover gate on touch devices", async () => {
  stubHoverCapable(false);
  const onAsk = vi.fn();
  const ui = await renderButton(onAsk, true);
  try {
    await ui.click();
    expect(ui.button.textContent).toContain("Sure?");
    await act(async () => {
      vi.advanceTimersByTime(700);
    });
    await ui.click();
    await act(async () => {});
    expect(onAsk).toHaveBeenCalledExactlyOnceWith("hello builder");
    expect(ui.button.textContent).toContain("Sent!");
  } finally {
    await ui.unmount();
  }
});

it("disarms after a timeout and on Escape", async () => {
  const onAsk = vi.fn();
  const ui = await renderButton(onAsk);
  try {
    await ui.click();
    expect(ui.button.textContent).toBe("Sure?");
    await act(async () => {
      vi.advanceTimersByTime(4100);
    });
    expect(ui.button.textContent).toBe("Ask AI");

    await ui.click();
    expect(ui.button.textContent).toBe("Sure?");
    await act(async () => {
      ui.button.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    });
    expect(ui.button.textContent).toBe("Ask AI");
    expect(onAsk).not.toHaveBeenCalled();
  } finally {
    await ui.unmount();
  }
});
