# Learnings Architecture: Explore vs. Exploit

This document outlines the workflow for learning stabilization and model optimization in the orchestrator.

## 🔄 Core Learning Loop

### 1. Pre-Execution: Step Hash Guard
- **Action:** Calculate SHA256 of step definition (Title, Desc, Criteria, Deps).
- **Logic:** If hash mismatches the stored `StepHash`, the plan has changed.
- **Result:** 
    - Reset all stable run counters (`successful_runs_simple`, `successful_runs_medium`, `successful_runs_complex` → 0)
    - Set `LockLearnings = false` (Unlock) to restart the learning process for the new requirements
    - This ensures a fresh start when step requirements change

### 2. Execution Mode Selection

#### Mode A: UNLOCKED (Learning Phase)
- **Learning Activity:**
    - **Learning Agent:** **RUNS** after execution to extract new patterns.
- **Existing Learnings:** **ALWAYS LOADED** into system prompt if they exist (via `LoadStepLearningHistory()`).
- **Locking Decision (TurnCount-Based):**
    - Based on `TurnCount` (complexity) of successful executions:
        - **Simple (< 15 turns):** Lock after **3** successful runs.
        - **Medium (15-30 turns):** Lock after **5** successful runs.
        - **Complex (> 30 turns):** Lock after **10** successful runs.
    - *Successful run* means validation passed (success criteria met).
    - Locking is automatic after reaching the threshold for the complexity level.

#### Mode B: LOCKED (Optimized Phase)
- **Learning Activity:**
    - **Learning Agent:** **NOT CALLED**. No new patterns are extracted.
- **Existing Learnings:** **STILL LOADED** into system prompt if they exist (via `LoadStepLearningHistory()`).
- **Trigger to Unlock:** If validation fails while locked, the system automatically unlocks to trigger re-learning.

---

## 📊 TurnCount-Based Auto-Locking System

The system uses a simple, reliable approach: count successful executions and lock based on complexity.

### How It Works:

1. **After each successful execution** (validation passed):
   - System records the `TurnCount` (number of LLM turns)
   - Increments successful run counter for that complexity level
   - Checks if threshold reached → auto-locks if yes
   - **Cost Optimization:** After reaching 50% of stable runs, learning agents use cheaper tempLLM
     - **Simple:** After **2** runs (of 3) → use tempLLM for learning
     - **Medium:** After **3** runs (of 5) → use tempLLM for learning
     - **Complex:** After **5** runs (of 10) → use tempLLM for learning

2. **Complexity Classification:**
   - **Simple:** `< 15 turns` → Lock after **3** successful runs
   - **Medium:** `15-30 turns` → Lock after **5** successful runs  
   - **Complex:** `> 30 turns` → Lock after **10** successful runs

3. **Locking Behavior:**
   - Once threshold reached, `LockLearnings = true` is set automatically
   - Learning agents stop running (no new extraction)
   - Existing learnings continue to be used

4. **Unlocking & Reset Triggers:**
   - **Plan changes (Step Hash mismatch):**
     - Unlocks learnings (`LockLearnings = false`)
     - Resets all stable run counters to 0
     - Starts fresh learning cycle for new requirements
   - **Validation failure while locked:**
     - Auto-unlocks to trigger re-learning
     - Does NOT reset counters (preserves progress)

### Benefits:
- ✅ **Simple & Reliable:** No complex learning detection logic
- ✅ **Predictable:** Clear thresholds based on execution complexity
- ✅ **Efficient:** Reduces unnecessary learning extraction after patterns stabilize

---

## 🔧 Model Selection

### Execution Agent LLM Selection (Independent of Lock Status)

**Important:** Execution model selection is **NOT** controlled by lock status. It operates independently based on:

#### LLM Selection Priority:
1. **tempLLM1** (if learnings exist AND retryAttempt == 1)
2. **tempLLM2** (if learnings exist AND retryAttempt == 2)
3. **Step Config LLM** (if configured)
4. **Preset Default LLM** (if configured)
5. **Orchestrator Default LLM** (fallback)

#### tempLLM Usage Rules:
- **Only used when:** Step has existing learnings (learnings folder has files)
- **Skipped when:** Learnings folder is empty → uses original LLM
- **Cascading:** tempLLM1 → tempLLM2 → base LLM (on retry attempts)
- **Independent:** Works the same whether learnings are locked or unlocked

### Learning Agent LLM Selection (Cost Optimization)

**Purpose:** Reduce costs as we approach lock threshold by using cheaper models for learning extraction.

#### Learning Agent LLM Selection Logic:
- **Early Phase (< 50% of stable runs):**
  - Uses **High-IQ Primary LLM** (e.g., Claude 3.5 Sonnet)
  - Ensures high-quality pattern extraction during initial learning

- **Late Phase (≥ 50% of stable runs):**
  - Uses **Cheaper tempLLM** for learning agents
  - Patterns are already established, cheaper model can follow them
  - Thresholds:
    - **Simple:** After **2** successful runs (of 3 total)
    - **Medium:** After **3** successful runs (of 5 total)
    - **Complex:** After **5** successful runs (of 10 total)

#### Benefits:
- ✅ **Cost Reduction:** Cheaper models for learning when patterns are stable
- ✅ **Quality Preserved:** High-IQ model used during critical early learning phase
- ✅ **Automatic:** No manual configuration needed

---

## 📚 Learning Modes: Exact vs General

The learning agent supports two detail levels for pattern extraction, configurable per step via `learning_detail_level` in `step_config.json`.

### EXACT Mode (`learning_detail_level: "exact"`)

**Purpose:** Extract complete, replayable workflow sequences with dependencies and data flow.

**Focus:**
- WORKFLOW-CENTRIC execution sequence
- Dependencies between steps
- Data flow tracing (Step 1 Output → Step 2 Input)

**Output Format:**
```
⭐ OPTIMAL PATH [Runs: X | Success: Y%]
1. server.tool:
   - arguments: {COMPLETE JSON - replace hardcode paths with {{WORKSPACE_PATH}}}
   - prerequisites: [Condition]
   - outputs: [Description]
   - on_error: [Specific recovery]

### 📊 DATA FLOW
Step 1 Output -> Step 2 Input. Trace the flow accurately.
```

**What It Captures:**
- ✅ Complete MCP tool call sequences
- ✅ Full JSON arguments (with variable placeholders)
- ✅ Prerequisites/conditions
- ✅ Output descriptions
- ✅ Error recovery strategies
- ✅ Data flow between steps

**Use Case:** Complex steps requiring precise, replayable workflows with dependencies.

---

### GENERAL Mode (`learning_detail_level: "general"`)

**Purpose:** Extract high-level patterns, tool names, and Python scripts.

**Focus:**
- Tool names and high-level patterns
- Python scripts/recipes
- Brief strategy descriptions

**Output Format:**
```
### ✅ SUCCESS PATTERN
- **Tools**: server.tool [Runs: X | Success: Y%]
- **Approach**: Brief description of the strategy.
```

**What It Captures:**
- ✅ Tool names used successfully
- ✅ High-level approach/strategy
- ✅ Python scripts (saved to scripts folder)
- ✅ Success rates

**Use Case:** Simpler steps where high-level patterns are sufficient.

---

### Comparison Table

| Aspect | EXACT Mode | GENERAL Mode |
|--------|------------|--------------|
| **Detail Level** | Complete, replayable workflow | High-level patterns |
| **Data Flow** | ✅ Traces step-to-step data flow | ❌ No data flow tracking |
| **Arguments** | Complete JSON with all fields | Tool names only |
| **Dependencies** | ✅ Prerequisites & conditions | ❌ Not captured |
| **Error Handling** | ✅ Specific recovery strategies | ❌ Not captured |
| **Use Case** | Complex workflows with dependencies | Simple steps, tool discovery |

### Configuration

- **Default:** `"exact"` (if not specified)
- **Options:** `"exact"`, `"general"`, or `"none"` (disables learning)
- **Location:** `step_config.json` → `AgentConfigs.learning_detail_level`
- **Per-Step:** Each step can have its own learning detail level

---

## 🔀 Learning Content Delivery: Dynamic Exploration vs. Exploitation

The system employs a dynamic strategy for passing learning content to execution agents, balancing **Exploration** (trying new ways) with **Exploitation** (sticking to known patterns). This is controlled by the `KeepLearningFull` logic.

### Phase 1: Exploration (Paths Only)
**Trigger:** Initial runs / unstable patterns (Low successful run count).

**Behavior:**
- **File Paths Only:** Only file paths are passed in the user message.
- **Goal:** Encourage the agent to "experience and learn more." By not having the full pattern immediately in context, the agent is forced to either:
    - Explore alternative approaches (if it thinks it knows better).
    - Read the learning files explicitly (if it needs guidance).
- **Result:** Broader coverage of potential solutions and "smarter" final patterns.

### Phase 2: Exploitation (Full Content)
**Trigger:** Stable patterns (Threshold reached).

**Behavior:**
- **Full Content:** Full learning content is included directly in the system prompt.
- **Goal:** Efficiency and Reliability. The agent has the "answer key" immediately available and is strongly guided to follow the proven path.
- **Result:** Faster execution and higher reliability.

### Switching Thresholds (Dynamic Logic)
The system automatically switches from Exploration (False) to Exploitation (True) based on successful run history:

- **Simple Steps:** Switch after **2** successful runs.
- **Medium Steps:** Switch after **3** successful runs.
- **Complex Steps:** Switch after **5** successful runs.

*Note: If any of these thresholds are met, the step enters Exploitation mode.*

### Feature Flag Overrides (`keep_learning_full`)

You can override this dynamic behavior using the `keep_learning_full` feature flag.

- `keep_learning_full: true` → **Force Exploitation** (Always pass full content).
- `keep_learning_full: false` → **Force Exploration** (Always pass paths only).
- `null` (Default) → **Use Dynamic Logic** (Switch based on thresholds above).

### Configuration Priority

1. **Step Config** (`step_config.json` → `AgentConfigs.keep_learning_full`)
2. **Environment Variable** (`KEEP_LEARNING_FULL=true` or `KEEP_LEARNING_FULL=false`)
3. **Dynamic Logic:** (Based on successful run counts vs thresholds)
4. **Fallback:** `false` (Exploration mode) if no history exists.

### Benefits Comparison

| Aspect | Exploration (Paths) | Exploitation (Full) |
|--------|---------------------|---------------------|
| **Token Usage** | Lower (paths only) | Higher (content in prompt) |
| **Agent Behavior** | Encouraged to try new ways / read on demand | Strictly guided to follow pattern |
| **Learning Quality** | Higher (broader experience) | Stable (optimization) |
| **Phase** | Early / Learning | Late / Optimized |

---

## 🛠 Component Roles

| Component | Responsibility | Timing |
| :--- | :--- | :--- |
| **Step Hash Guard** | Detects plan changes. Unlocks and resets stable run counters if definition changed. | Pre-Execution |
| **Learning Loader** | Loads learnings into execution agent. Switch between **Full Content** (Exploitation) and **File Paths** (Exploration) based on run history. | Pre-Execution |
| **Learning Agents** | Extract new patterns. **Only run when Unlocked.** | Post-Execution |
| **TurnCount Tracker** | Tracks successful execution count per complexity level. Triggers auto-lock when threshold reached. | Post-Execution |
| **Execution LLM Selector** | Selects execution LLM based on learnings existence + retry attempts (independent of lock status). | Execution |
| **Learning LLM Selector** | Selects learning agent LLM based on stable run progress. Uses tempLLM after 50% threshold for cost optimization. | Post-Execution |

---

## 📂 File Structure
```text
learnings/
  step-id/
    .learning_metadata.json  # Tracks Hash, Successful Run Counters, TurnCount, Complexity
    step_title_learning.md   # Patterns (loaded into system prompt when they exist)
    code/
      step_title_code.go     # Go code patterns
```

### Metadata Fields:
- `step_id`: Step identifier
- `step_hash`: SHA256 of step definition (for change detection)
- `total_iterations`: Total number of learning attempts
- `successful_runs_simple`: Counter for simple steps (< 15 turns)
- `successful_runs_medium`: Counter for medium steps (15-30 turns)
- `successful_runs_complex`: Counter for complex steps (> 30 turns)
- `last_turn_count`: Last recorded TurnCount
- `auto_locked_at`: Timestamp when auto-lock was triggered
- `auto_lock_reason`: Reason for lock (e.g., "threshold_reached")

---

## 🔑 Key Principles

1. **Lock Status = Learning Extraction Control**
   - Locked: Skip learning agents (no new extraction)
   - Unlocked: Run learning agents (extract new patterns)

2. **TurnCount-Based Auto-Locking**
   - Locking is based on successful execution count, not learning detection
   - Complexity is determined by `TurnCount` (number of LLM turns in execution)
   - Simple steps (< 15 turns) lock faster (3 runs), complex steps (> 30 turns) need more runs (10 runs)
   - Each successful run (validation passed) increments the counter

3. **Existing Learnings Usage (Explore vs. Exploit)**
   - If learnings exist, they are **always available** to the agent.
   - **Exploration Phase (Early):** Passed as file references in user message. Prompt encourages **innovation and alternative approaches**.
   - **Exploitation Phase (Late):** Passed as full content in system prompt. Prompt encourages **strict adherence** to stable patterns.

4. **Model Selection Strategy**
   - **Execution LLM:** Independent of lock status, based on learnings existence + retry attempts
   - **Learning LLM:** Cost-optimized based on stable run progress
         - Early phase (< 50%): High-IQ model for quality
         - Late phase (≥ 50%): Cheaper tempLLM for cost savings
     
     ---
     
     ## 🚀 Implementation Roadmap (Refactor Plan)
     
     This section tracks the migration from the legacy "No-New-Learning" logic to the "TurnCount-Based" architecture.
     
     ### Phase 1: Foundation - Data Structures & Hashing (DONE)
     - [x] **Metadata Upgrade:** Update `LearningMetadata` struct in `controller_learning_detection.go` to include `StepHash`, `LastTurnCount`, and complexity counters.
     - [x] **Step Hash Helper:** Implement SHA256 hashing for step definitions (Title, Desc, Criteria, Deps).
     - [x] **Hash Guard:** Implement the pre-execution check that resets counters and unlocks learnings if the hash mismatches.
     
### Phase 2: Logic - Complexity-Based Auto-Locking (DONE)
- [x] **TurnCount Capture**: Extract `turnCount` from execution history in `controller_execution.go`.
- [x] **Threshold Logic**: Implement deterministic auto-locking logic (3/5/10) based on complexity classification.
- [x] **Metadata Persistence**: Update the metadata saving logic to increment complexity-specific counters.
- [x] **Integration & Validation**: Integrated TurnCount-based classification across success and failure learning phases.

### Phase 3: Optimization - Smart Model Selection (DONE)
- [x] **Dynamic LLM Switching**: Refactor `selectLearningLLM` to switch to `tempLLM` once a step reaches the 50% stability threshold.
- [x] **Event Logging**: Log cost-optimization model switches for transparency.
     
     ### Phase 4: Integration & Validation (TODO)
     - [ ] **Cleanup:** Remove legacy detection logic and unused metadata fields.
     - [ ] **Testing:** Verify "Change Step -> Reset" and "Run X times -> Auto-lock" workflows.     
