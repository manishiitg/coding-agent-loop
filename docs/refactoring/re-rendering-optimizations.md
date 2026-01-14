# Re-rendering Optimizations & Bug Fixes

## 🐛 Issue: Excessive Re-renders in Core Components

**Status**: ✅ Fixed
**Date**: 2026-01-09
**Components Affected**: `ChatArea`, `EventDisplay`, `WorkflowToolbar`

### Symptoms
- UI sluggishness during high-frequency updates (e.g., streaming tokens).
- `why-did-you-render` reported "different objects that are equal by value" for Zustand store hooks.
- Components were re-rendering on *any* store update, not just when relevant data changed.

### Root Cause
Zustand's `useStore()` hook, when used without a selector (e.g., `const { a, b } = useStore()`), returns the entire state object. Since Zustand creates a new immutable state object on every update, the object reference changes even if properties `a` and `b` haven't changed. This forces React to re-render the component.

Custom hooks like `usePresetApplication` were also wrapping store calls and returning new object references on every render, bypassing inherent optimizations.

---

## 🛠️ Implementation Details

### 1. Diagnostic Tooling
Installed and configured `@welldone-software/why-did-you-render` to identify performance bottlenecks.
- **Config**: `frontend/src/wdyr.ts` (enabled only in dev mode, `trackAllPureComponents: false`).
- **Usage**: Added `Component.whyDidYouRender = true` to suspect components.

### 2. The Fix: `useShallow` Pattern
Refactored store subscriptions to use `useShallow` from `zustand/react/shallow`. This ensures the component only re-renders if the shallow comparison of the selected object changes.

**Before (Bad):**
```typescript
// Subscribes to entire store. Any update to ANY property triggers re-render.
const { isStreaming, setIsStreaming } = useChatStore()
```

**After (Good):**
```typescript
import { useShallow } from 'zustand/react/shallow'

// Only re-renders if isStreaming or setIsStreaming changes.
const { isStreaming, setIsStreaming } = useChatStore(useShallow(state => ({
  isStreaming: state.isStreaming,
  setIsStreaming: state.setIsStreaming
})))
```

### 3. Component-Specific Fixes

#### `ChatArea.tsx`
- Refactored 6 store subscriptions: `useAppStore`, `useLLMStore`, `useMCPStore`, `useChatStore`, `useModeStore`, `useGlobalPresetStore`.
- **Critical Fix**: Replaced `usePresetApplication` custom hook (which caused leaks) with direct `useGlobalPresetStore(useShallow(...))` usage.
- Refactored polling action selectors to use atomic selectors.

#### `EventDisplay.tsx`
- Refactored `useChatStore` subscription for `finalResponse` and `isCompleted`.

#### `WorkflowToolbar.tsx`
- Consolidated 30+ individual atomic selectors into a single `useWorkflowStore(useShallow(...))` call for better readability and performance maintenance.
- Fixed `useWorkspaceStore` subscription.

---

## 📏 Coding Standards for State Management

To prevent regression:

1.  **Always use Selectors**: Never call `useStore()` without arguments if you only need a slice of the state.
2.  **Use `useShallow` for Destructuring**: When selecting multiple properties, wrap the selector in `useShallow`.
    ```typescript
    const { a, b } = useStore(useShallow(state => ({ a: state.a, b: state.b })))
    ```
3.  **Atomic Selectors for Singles**: For single properties, simple selectors are fine.
    ```typescript
    const a = useStore(state => state.a)
    ```
4.  **Avoid Wrapper Hooks**: Be cautious with custom hooks that wrap stores and return new objects. They often break referential equality.
