# Task Agent Mode & Workflow Library Extraction Plan

## 1. Overview
The goal is to implement the **Task Agent Mode** while simultaneously refactoring the core workflow logic into a reusable **Workflow Library**. This ensures the Task Agent is built on a clean, decoupled foundation that can be used by external projects.

**Key Objectives:**
1.  **Extract TodoTask Logic:** Move the "Manager-Worker" orchestration logic from `controller_todo_task.go` into a standalone `pkg/workflow/todotask` package.
2.  **Implement Task Agent Mode:** Build the Task Agent backend by utilizing this new library, injecting a synthetic plan.
3.  **Frontend Integration:** Connect the existing Chat UI to the new backend mode.

---

## 2. Architecture

### 2.1 Workflow Library (`pkg/workflow/todotask`)
A self-contained package that manages the "Manager" lifecycle:
*   **Input:** Configuration (objective, routes, constraints), Callbacks (for LLM, tools, logging).
*   **State:** Manages `tasks.md` and `completed.txt` in the workspace.
*   **Execution:** Runs the loop: Read Tasks -> Plan/Delegate -> Execute Callback -> Reflect -> Update Tasks.
*   **Decoupling:** Does NOT depend on the heavy `StepBasedWorkflowOrchestrator` struct directly. It defines interfaces for what it needs.

### 2.2 Task Agent Backend
Instead of being a "mode" inside the monolithic orchestrator, the Task Agent becomes a lightweight consumer of the library.
*   **Synthetic Plan:** We still generate a "Virtual Plan" (single `TodoTaskPlanStep`), but now we execute it using the library's `Orchestrator`.
*   **Integration:** `server.go` initializes the library with the necessary callbacks (bridging to existing LLM and Tool infrastructure).

### 2.3 Frontend ("Unified Chat Stream")
*   **UI:** Standard `ChatArea`.
*   **Events:** The library emits standard events (`TodoTaskItemUpdated`, `TodoTaskRouteSelected`) which the frontend already understands or can easily adapt to.

---

## 3. Implementation Steps

### Phase 1: Workflow Library Extraction (`pkg/workflow/todotask`)

#### 1. Define Types & Interfaces (`types.go`)
*   Define `Config`, `Response`, `RouteConfig`.
*   Define interfaces: `LLMProvider`, `WorkspaceClient`, `EventListener`.
*   Define callback types: `SubAgentExecutor`, `GenericExecutor`.

#### 2. Move Prompt Templates (`templates.go`)
*   Extract `todoTaskOrchestratorSystemTemplate` and `userTemplate` from `todo_task_orchestrator_agent.go`.
*   Make them exported variables/functions so they can be customized or used as defaults.

#### 3. Implement the Core Loop (`orchestrator.go`)
*   Create the `Orchestrator` struct.
*   Port the logic from `controller_todo_task.go` (specifically `executeTodoTaskStep` and its helpers).
*   Refactor hardcoded dependencies (like `hcpo.GetLogger()`, `hcpo.variableValues`) to use the Config/Interfaces.
*   **Critical:** Ensure `tasks.md` management via shell commands remains robust.

#### 4. Tool Executors (`executors.go`)
*   Move the logic for `call_sub_agent` and `call_generic_agent` processing here (the logic that parses the tool call and invokes the callback).

### Phase 2: Refactor Existing Backend to Use Library

#### 1. Update `StepBasedWorkflowOrchestrator`
*   Modify `controller_todo_task.go` to delete the inline logic.
*   Instead, instantiate `todotask.New(...)` and call `orchestrator.Execute()`.
*   Implement the necessary callbacks (adapters) to bridge the old `hcpo` methods to the new library interfaces.

### Phase 3: Task Agent Mode Implementation

#### 1. Task Agent Factory (`task_agent_factory.go`)
*   (Already partially done) Ensure it generates a `TodoTaskPlanStep` compatible with the new library config.

#### 2. Server Integration (`server.go`)
*   Handle `agent_mode: "task_agent"`.
*   Instead of spinning up the full `StepBasedWorkflowOrchestrator` (which expects a file-based plan), initialize a lightweight context.
*   Create a specialized "Adapter" that sets up the `todotask.Orchestrator` with the synthetic plan and runs it.

### Phase 4: Frontend & Verification

#### 1. Frontend Updates
*   Add "Task Agent" to the mode selector in `App.tsx`.
*   Verify event stream rendering (Manager actions vs. Worker logs).

#### 2. Verification
*   Test with a simple query: "Create a React app."
*   Verify `tasks.md` creation.
*   Verify delegation to generic agent.
*   Verify task updates and completion.

---

## 4. Development Checklist

- [ ] **Phase 1: Library Extraction**
    - [ ] Create `pkg/workflow/todotask/types.go`
    - [ ] Create `pkg/workflow/todotask/templates.go`
    - [ ] Create `pkg/workflow/todotask/orchestrator.go` (Core Logic Port)
    - [ ] Create `pkg/workflow/todotask/executors.go` (Tool Logic)
- [ ] **Phase 2: Refactor Existing**
    - [ ] Update `controller_todo_task.go` to use `pkg/workflow/todotask`.
- [ ] **Phase 3: Task Agent Mode**
    - [ ] Create `task_agent_adapter.go` (Bridges Server -> Library).
    - [ ] Update `server.go` to route `task_agent` requests.
- [ ] **Phase 4: Frontend**
    - [ ] Enable mode selection.
    - [ ] Test end-to-end.
