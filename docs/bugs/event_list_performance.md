# Event List Performance & Virtualization

## 🚨 Problem Description

The current chat interface experiences significant performance degradation as the number of events increases.

### Root Causes
1.  **Recursive Rendering:** `EventHierarchy.tsx` builds and renders a recursive tree structure for nested events. This means every event (and its children) exists in the DOM simultaneously.
2.  **DOM Heaviness:** Each event component (`EventDisplay`, `EventDispatcher`) is complex, often containing Markdown renderers, syntax highlighters, and deeply nested HTML structures for logs and tool outputs.
3.  **No Virtualization:** The browser attempts to layout and paint hundreds of these complex components at once. Even with the current limit of ~100 events, this can create thousands of DOM nodes, causing the main thread to block during updates or scrolling.
4.  **Re-render Cascades:** An update to the event list often triggers a re-render of the entire tree structure.

### Symptoms
*   Browser becomes unresponsive or "janky" when scrolling through a long chat history.
*   High CPU usage during event streaming.
*   Delayed response to user interactions (e.g., clicking to expand/collapse an item).

---

## 🛠 Proposed Solution

We will implement **UI Virtualization** to decouple the *number of events in memory* from the *number of events in the DOM*.

### 1. Library Selection: `react-virtuoso`
We will use `react-virtuoso` because:
*   It supports **variable height items** out of the box (essential for chat events which vary wildly in size).
*   It handles **dynamic resizing** automatically (e.g., when a user expands a log section).
*   It provides "stick to bottom" behavior needed for chat interfaces.

### 2. Architecture Change: Flattened Tree
Virtualization libraries work best with flat lists, not recursive trees. We will refactor `EventHierarchy.tsx`:

*   **Current:** Recursive Component Structure
    ```jsx
    <Node>
      <Children>
        <Node />
      </Children>
    </Node>
    ```

*   **New:** Linear List with Metadata
    We will transform the tree into a flat array where each item knows its depth:
    ```javascript
    [
      { event: A, level: 0, expanded: true },
      { event: B, level: 1, parent: A }, // Visible because A is expanded
      { event: C, level: 0, expanded: false },
      // { event: D, level: 1 } // Hidden because C is collapsed
    ]
    ```

### 3. Implementation Plan

1.  **Dependencies:** Install `react-virtuoso`.
2.  **Data Structure:** Implement a `useMemo` hook in `EventHierarchy` that:
    *   Takes the recursive tree structure.
    *   Traverses it (respecting `expandedNodes` state).
    *   Produces a flat array of visible items.
3.  **Component Refactor:**
    *   Replace the manual `.map()` rendering with the `<Virtuoso>` component.
    *   Pass the flattened list to `data`.
    *   Render each item with the appropriate `marginLeft` based on its `level` property to visually simulate the tree structure.
4.  **Configuration:**
    *   Enable `followOutput` to keep the chat scrolled to the bottom during streaming.
    *   Configure `overscan` to ensure smooth scrolling.

### 4. Expected Benefits
*   **Constant DOM Size:** Regardless of whether there are 100 or 10,000 events, the DOM will only contain the ~20 items currently visible on screen.
*   **Scalability:** We can safely increase `MAX_EVENTS_TO_PROCESS` (currently ~150) to a much higher number (e.g., 1,000+), allowing users to scroll back through entire long-running sessions without performance penalties.
*   **Smoothness:** eliminating main-thread blocking will result in 60fps scrolling.
