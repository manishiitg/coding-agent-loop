// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it } from "vitest";
import { usePersistentTab } from "./usePersistentTab";

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

// happy-dom runs without a localStorage implementation here; the hook only
// needs the get/set/clear surface.
const backingStore = new Map<string, string>();
Object.defineProperty(window, "localStorage", {
  value: {
    getItem: (key: string) => (backingStore.has(key) ? backingStore.get(key)! : null),
    setItem: (key: string, value: string) => {
      backingStore.set(key, String(value));
    },
    removeItem: (key: string) => {
      backingStore.delete(key);
    },
    clear: () => {
      backingStore.clear();
    },
  },
  configurable: true,
});

afterEach(() => {
  document.body.innerHTML = "";
  window.localStorage.clear();
});

function Probe({ storageKey }: { storageKey: string }) {
  const [tab, setTab] = usePersistentTab(storageKey, "a", ["a", "b"] as const);
  return (
    <button type="button" onClick={() => setTab("b")}>
      tab:{tab}
    </button>
  );
}

async function renderProbe(storageKey: string) {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(<Probe storageKey={storageKey} />);
  });
  return container;
}

it("defaults, restores, and persists the tab while ignoring stale values", async () => {
  const first = await renderProbe("test-persist-tab");
  expect(first.textContent).toBe("tab:a");

  await act(async () => {
    first.querySelector("button")!.click();
  });
  expect(first.textContent).toBe("tab:b");
  expect(window.localStorage.getItem("test-persist-tab")).toBe("b");

  // A reload restores the stored tab.
  const second = await renderProbe("test-persist-tab");
  expect(second.textContent).toBe("tab:b");

  // A stored value that is no longer a valid tab falls back to the default.
  window.localStorage.setItem("test-persist-tab", "retired");
  const third = await renderProbe("test-persist-tab");
  expect(third.textContent).toBe("tab:a");
});
