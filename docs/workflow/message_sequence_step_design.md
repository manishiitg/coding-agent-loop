# Message Sequence Step Design

## Summary

This document proposes a new workflow step type for running an ordered queue of user messages in one agent conversation.

The goal is to support workflows where the builder wants explicit control over the order of prompts sent to an agent, while still allowing the agent to handle conditional behavior inside each message.

Example use case:

1. Log in to a bank portal.
2. Update learnings about login.
3. Go to dashboard and extract balance.
4. Update learnings about dashboard extraction.
5. Update database with balance.
6. Run prevalidation.
7. Download account statement.
8. Update learnings about statement download.
9. Run a Python parser for the statement.
10. Validate transactions database.

This is different from a `todo_task` orchestrator. A `todo_task` orchestrator dynamically decides what to do next. A message sequence is explicit: the workflow owns the order of messages.

## Core Idea

Add a step type:

```json
{
  "type": "message_sequence",
  "id": "bank-portal-daily-run",
  "title": "Bank portal daily run",
  "items": []
}
```

The runtime creates one agent session and sends each configured item in order.

```text
message 1 -> assistant response
message 2 -> assistant response
code item -> script result
message 3 -> assistant response
prevalidation -> pass/fail
...
```

If the step is used as a route inside a `todo_task` orchestrator and the orchestrator calls the same route again within the same run, the step continues its existing agent conversation instead of starting from scratch. That conversation is held **in memory on the orchestrator instance, scoped to the current workflow run**. It is not loaded back from disk.

```text
first orchestrator call to write-tests route:
  create conversation A (held in memory for this run)
  send configured message queue

later orchestrator call to write-tests route (same run):
  continue conversation A from the in-memory cache
  send orchestrator-provided re-entry user message
```

This allows critique or execution feedback to loop back into the same writer agent with its existing context, but only within one run. See "Persistence model" below.

## Design Principles

- The workflow controls ordering.
- The agent handles conditional logic inside messages.
- A sequence step used as a `todo_task` route continues its own in-memory conversation when the orchestrator re-enters it within the same run. A standalone (top-level) sequence step has no memory and re-runs its fixed queue.
- Prevalidation is a backend gate, not a normal LLM task.
- Work turns, learning, knowledgebase, DB, and code can all be explicit sequence items.
- Read access is open for the sequence; write access is opened only for the active item.
- Python/scripted items can run without LLM unless they fail.
- Failed validations stop the step after any configured repair attempts are exhausted.

## Why Not Use Todo Task Orchestrator

`todo_task` is useful when the agent should decide the next route or sub-agent dynamically.

`message_sequence` is useful when the builder already knows the order.

Use `message_sequence` when:

- The process is naturally procedural.
- The user wants specific user messages sent in order.
- Learning or KB updates should happen at exact points.
- Some parts are deterministic Python/code.
- Prevalidation should gate later messages.

Use `todo_task` when:

- The runtime needs adaptive delegation.
- The next route depends heavily on the result of the previous route.
- The agent should choose between predefined routes.

## Single-step quality patterns

The patterns elsewhere use `message_sequence` as a `todo_task` route. `message_sequence` is equally useful as a standalone step that makes one unit of work trustworthy, using the item queue (`user_message` + `code` + prevalidation sharing one conversation):

- Self-Validation Gate: after a work turn, add `user_message` items that interrogate the same conversation about what it actually did ("Did you actually call xyz? Quote the exact output. Did you actually produce abc?"), then a prevalidation item whose schema checks the concrete artifacts. Interleave several interrogate→prevalidation pairs, each prevalidation using a different schema, to gate distinct claims.
- Compute-then-Reason: alternate `code` items (fetch/parse/compute ground truth) with `user_message` items that reason over the result; the runtime feeds each code item's stdout + exit code into the next message turn.
- Citation / Grounding Gate: a `user_message` forcing the agent to cite the exact file/line/tool-output behind each claim, then a prevalidation that the cited files exist.
- Self-Healing Script: on a `code` item set `on_failure: repair_with_llm` with `max_retries` (and `save_repaired_script`) so the same conversation debugs its own failing script across attempts.

Briefer variants: Plan-then-Execute, Dry-Run-then-Commit, Accumulator.

Constraint: the item queue is linear and runs once — no branching, no conditional skip, no "loop until prevalidation passes" inside a single sequence. Iteration comes only from in-memory orchestrator re-entry (route) or the code-item repair loop.

## Persistence model: route (in-memory) vs standalone (fixed queue)

A `message_sequence` has two roles that differ ONLY in whether the conversation is remembered. There is no disk-based session resume in either role.

**ROUTE** — a `message_sequence` used as a sub-agent inside a `todo_task`'s `predefined_routes`:

- The orchestrator re-enters the same specialist across its calls within one run, and the conversation IS remembered.
- That memory lives **in memory on the orchestrator instance, scoped to a single workflow run**. It is detected by call source `orchestrator_reentry`.
- It is NOT loaded back from disk, does NOT survive a process restart, and does NOT resume across separate workflow runs.

**STANDALONE** — a top-level `message_sequence` step in the plan:

- A **fixed item queue that runs once**. There is NO memory and NO re-entry.
- Re-running a standalone step simply re-runs the configured queue from the start.

In both roles the runtime still writes `session.json`, but only as a **one-way observability log** (see below); it is never read back to resume a conversation.

There is NO support for "resume/rerun at a later time" across separate runs or across process restarts. If you need iteration with memory, drive re-entry from the orchestrator (route) within a single run. If you need to run the whole sequence again later, schedule the workflow to re-run — that produces a fresh run with a fresh standalone queue (and, for routes, a fresh in-memory conversation).

### session.json is a write-only observability log

The runtime writes:

```text
runs/{run_folder}/execution/message_sequences/{step_path}/{step_id}/session.json
```

This file records the conversation history and per-item entries for debugging and inspection. It is a one-way log: the runtime never reads it back to seed or resume a conversation. (Tests assert it exists and contains conversation content, so it stays on disk.)

## Conversation Re-Entry

Conversation re-entry happens only for a ROUTE, only within one run, and only in memory. There is no builder/manual disk-based resume.

Route re-entry behavior:

- First call (within a run) creates the agent conversation and sends the configured item queue. The conversation is stored in the orchestrator's in-memory route cache for the remainder of the run.
- The step completes, but the conversation stays in the in-memory cache keyed by `step_path + step_id`.
- If the orchestrator calls the same route again in the same run, the runtime continues that in-memory conversation.
- Re-entry sends a new user message provided by the orchestrator (from `instructions`/`InstructionsToSubAgent`). It does not replay the original queue.
- `message_sequence_restart=true` clears the in-memory conversation AND wipes the route's on-disk runtime artifacts (working code copies, stdout, the `session.json` log) for a clean start. There is no archive of the old session.

Example:

```text
1. write-tests route runs and creates tests (conversation cached in memory for this run)
2. execute-tests route runs tests
3. critique-tests route finds missing cases
4. orchestrator calls write-tests route again (same run) with:
   "Use this critique and update the tests: {{critique.output}}"
5. write-tests continues its in-memory conversation and updates the tests
```

The in-memory key is `step_path + step_id` within the current orchestrator run. Two different sequence steps do not share conversation state, but the same route can continue when re-entered in the same run. Once the run ends (or the process restarts), the in-memory conversation is gone.

Standalone behavior:

- A standalone `message_sequence` step has no re-entry. Running it always runs the configured queue once.
- Re-running it just re-runs the queue. There is no "session exists → provide a re-entry message or restart" path, and there is no error for re-running a standalone step.

## Item Types

### User Message Item

Sends a user message into the same agent session.

```json
{
  "type": "user_message",
  "id": "login",
  "message": "Log in to the bank portal. If OTP is required, ask the user. If not, continue."
}
```

The agent can still do conditional work inside the message. There is no separate workflow-level `if/else`.

### Learning Message Item

Sends a learning-focused user message.

```json
{
  "type": "user_message",
  "id": "learn-login",
  "kind": "learning",
  "message": "Update workflow learnings with reusable details about the login flow. Do not include secrets or one-time OTP values."
}
```

Expected write target:

```text
learnings/_global/SKILL.md
```

### Knowledgebase Message Item

Sends a KB-focused user message.

```json
{
  "type": "user_message",
  "id": "update-bank-kb",
  "kind": "knowledgebase",
  "message": "Update knowledgebase notes with durable facts discovered during this run. Only record verified facts with source context."
}
```

Expected write target:

```text
knowledgebase/notes/
```

### DB Message Item

Sends a DB-focused user message.

```json
{
  "type": "user_message",
  "id": "save-balance",
  "kind": "db",
  "message": "Upsert the current account balance into db/balances.json using a stable account key."
}
```

Expected write target:

```text
db/
```

### Code Item

Runs deterministic code before involving the LLM.

```json
{
  "type": "code",
  "id": "parse-statement",
  "script": "learnings/parse-statement/main.py",
  "input_files": [
    "execution/download-statement/latest_statement.pdf"
  ],
  "output_files": [
    "db/transactions.json"
  ],
  "on_success": "continue",
  "on_failure": "repair_with_llm",
  "prevalidation": "transactions_db_schema"
}
```

Runtime behavior:

```text
run script
if success:
  run optional prevalidation
  continue to next item
if failure:
  send logs to same LLM session
  ask it to repair or rerun
```

This lets the workflow use Python for stable parsing, while keeping LLM repair available.

## Python Code Item Implementation

Python code items should run as deterministic runtime work first. The LLM is only involved when the script fails, validation fails, or the sequence later reaches a normal user-message item.

### Code Item Schema

Use this v1 shape:

```json
{
  "type": "code",
  "id": "parse-statement",
  "runtime": "python",
  "script_path": "learnings/parse-statement/main.py",
  "input_files": [
    "execution/download-statement/latest_statement.pdf"
  ],
  "input_json": {
    "account_id": "{{account_id}}",
    "statement_month": "{{statement_month}}"
  },
  "output_files": [
    "db/transactions.json"
  ],
  "on_failure": {
    "action": "repair_with_llm",
    "max_retries": 2
  },
  "save_repaired_script": false,
  "prevalidation": {
    "schema": "transactions_db_schema"
  }
}
```

Important fields:

- `script_path`: source script to copy into this run.
- `input_files`: workspace-relative paths resolved before execution.
- `input_json`: structured variables/config for the script.
- `output_files`: expected files for runtime summary and validation.
- `on_failure.action`: `stop_step` or `repair_with_llm`.
- `save_repaired_script`: default `false`; repaired code stays in the run copy.

### Runtime Folders

Each code item gets its own folder:

```text
runs/{run_folder}/execution/message_sequences/{sequence_step_id}/items/{item_id}/
```

Inside it:

```text
code/main.py
input.json
result.json
stdout.txt
stderr.txt
repair/
```

The runtime copies `script_path` to:

```text
runs/{run_folder}/execution/message_sequences/{sequence_step_id}/items/{item_id}/code/main.py
```

The script always executes from the `code/` directory. The copied script is the working copy the LLM can patch during repair.

### Inputs To Python

The script receives inputs in three ways.

1. Environment variables:

```text
MCP_API_URL
MCP_API_TOKEN
STEP_OUTPUT_DIR
STEP_EXECUTION_DIR
MESSAGE_SEQUENCE_STEP_ID
MESSAGE_SEQUENCE_ITEM_ID
MESSAGE_SEQUENCE_ITEM_DIR
MESSAGE_SEQUENCE_INPUT_JSON
MESSAGE_SEQUENCE_OUTPUT_FILES_JSON
PYTHONDONTWRITEBYTECODE=1
SCRIPT_VERBOSE=1
```

2. `input.json`:

```json
{
  "input_json": {
    "account_id": "checking-1234",
    "statement_month": "2026-05"
  },
  "input_files": [
    "/app/workspace-docs/Workflow/demo/runs/iteration-1/execution/download-statement/latest_statement.pdf"
  ],
  "output_files": [
    "/app/workspace-docs/Workflow/demo/db/transactions.json"
  ],
  "read_access": {
    "knowledgebase": true,
    "db": true,
    "learnings": true
  },
  "write_access": {
    "knowledgebase": false,
    "db": true,
    "learnings": false
  }
}
```

3. CLI args:

```text
python3 -B main.py /absolute/input/file/1 /absolute/input/file/2
```

The JSON file is the canonical contract. CLI args are only for simple compatibility with existing `learn_code` style scripts.

### Execution

The backend should execute Python through the same workspace shell API pattern used by `execLearnCodeScript`, not by local `os/exec`.

Execution command:

```text
python3 -B /abs/item/code/main.py /abs/input/file/1 ...
```

Working directory:

```text
runs/{run_folder}/execution/message_sequences/{sequence_step_id}/items/{item_id}/code
```

Folder guard:

- read access plus item write access from `setupMessageSequenceFolderGuard`
- plus write access to this item folder
- plus read/write access to this item `code/` folder for repair

The runner writes:

```json
{
  "item_id": "parse-statement",
  "status": "success|failed",
  "exit_code": 0,
  "script_path": ".../items/parse-statement/code/main.py",
  "stdout_path": ".../stdout.txt",
  "stderr_path": ".../stderr.txt",
  "output_files": [
    "db/transactions.json"
  ],
  "prevalidation_status": "passed|failed|skipped"
}
```

### Success Path

If Python exits with code `0`:

1. Save `stdout.txt`, `stderr.txt`, and `result.json`.
2. Run inline prevalidation if configured.
3. If prevalidation passes or is not configured, mark the item complete.
4. Add a compact runtime context block to the sequence session.
5. Continue to the next item.

The runtime should not immediately call the LLM just to announce success.

Instead, the next LLM user-message item receives the code result as prepended context:

```text
## Runtime context from previous code item: parse-statement
Status: success
Exit code: 0
Executed script:
runs/iteration-1/execution/message_sequences/bank-portal-daily-run/items/parse-statement/code/main.py
Outputs:
- db/transactions.json
Stdout summary:
Parsed 42 transactions from latest_statement.pdf.

## Next instruction
Update the database summary and explain any unusual transactions.
```

If the next item is another code item or prevalidation item, keep carrying this runtime context forward until the next user-message item or final sequence summary.

Do not prepend the full Python code by default. Include the executed script path plus result summary. The next agent can read the script if needed.

Only include the full script text when the next item is explicitly about:

- repairing the script
- reviewing the script
- critiquing code quality
- self-validating the script implementation
- explaining how the script works

### Failure Path

Failure means:

- non-zero exit code
- workspace shell API error
- missing expected output
- prevalidation failure after script success

On failure:

1. Save `stdout.txt`, `stderr.txt`, and `result.json`.
2. If `on_failure.action` is `stop_step`, stop the whole sequence step.
3. If `on_failure.action` is `repair_with_llm`, send a repair user message into the same sequence conversation.

Repair message shape:

```text
## Python code item failed: parse-statement

Script working copy:
runs/{run}/execution/message_sequences/{sequence_step_id}/items/{item_id}/code/main.py

Input contract:
runs/{run}/execution/message_sequences/{sequence_step_id}/items/{item_id}/input.json

Exit code: 1

Stdout:
...truncated stdout...

Stderr:
...truncated stderr...

Expected outputs:
- db/transactions.json

Instructions:
Fix the working copy of main.py, then run it again from the code directory.
Do not fabricate output data. Use the input files and allowed stores only.
```

The LLM repair turn uses the same conversation as previous sequence messages. It can read prior context, patch `code/main.py`, and run it with shell/tools.

After the repair turn:

1. Runtime reruns `code/main.py`.
2. Runtime reruns prevalidation if configured.
3. If success, continue to the next item.
4. If failure and retries remain, send another repair message.
5. If retries are exhausted, stop the sequence step.

### Context After Repair Success

When repair succeeds, the next user-message item gets a context block that includes both the failure and the recovery:

```text
## Runtime context from previous code item: parse-statement
Status: repaired_success
Attempts: 2
Final exit code: 0
Executed script:
runs/iteration-1/execution/message_sequences/bank-portal-daily-run/items/parse-statement/code/main.py
Outputs:
- db/transactions.json
Repair summary:
The parser was fixed to handle blank transaction rows.
```

### Save-Back Policy

Default:

```json
{
  "save_repaired_script": false
}
```

That means the LLM may patch:

```text
runs/{run}/execution/message_sequences/{sequence_step_id}/items/{item_id}/code/main.py
```

but the runtime does not overwrite:

```text
learnings/parse-statement/main.py
```

If later enabled, save-back should happen only after:

- script exits `0`
- prevalidation passes
- static code review passes
- `save_repaired_script` is explicitly true

### Prevalidation Item

Runs backend validation. It is not a normal user message.

```json
{
  "type": "prevalidation",
  "id": "validate-balance-db",
  "schema": "balance_db_schema",
  "on_fail": {
    "action": "repair_same_session",
    "max_retries": 2
  }
}
```

On failure, the runtime sends a generated repair message:

```text
Prevalidation failed.

Errors:
- db/balances.json missing field account_id
- db/balances.json has duplicate key checking:1234

Fix only these issues, then stop.
```

Then it reruns that prevalidation.

## Example Bank Workflow

```json
{
  "type": "message_sequence",
  "id": "bank-daily-run",
  "title": "Bank daily run",
  "items": [
    {
      "type": "user_message",
      "id": "login",
      "message": "Log in to the bank portal. If OTP is required, ask the user for OTP. If not, continue. Stop after login is complete."
    },
    {
      "type": "user_message",
      "id": "learn-login",
      "kind": "learning",
      "message": "Update learnings with reusable login steps, selectors, page timing, and failure patterns. Do not store secrets, OTPs, or account credentials."
    },
    {
      "type": "user_message",
      "id": "extract-balance",
      "message": "Go to the dashboard and extract the current account balance. Write the raw extracted value and source evidence to the step output folder."
    },
    {
      "type": "user_message",
      "id": "learn-dashboard",
      "kind": "learning",
      "message": "Update learnings with how to reach the dashboard and extract the balance reliably."
    },
    {
      "type": "user_message",
      "id": "save-balance",
      "kind": "db",
      "message": "Upsert the extracted balance into db/balances.json. Use stable account identifiers and include observed_at."
    },
    {
      "type": "prevalidation",
      "id": "validate-balance",
      "schema": "balance_db_schema",
      "on_fail": {
        "action": "repair_same_session",
        "max_retries": 2
      }
    },
    {
      "type": "user_message",
      "id": "download-statement",
      "message": "Go to the account statement page and download the latest statement PDF or CSV. Save it under this step's execution folder."
    },
    {
      "type": "user_message",
      "id": "learn-statement-download",
      "kind": "learning",
      "message": "Update learnings with how to navigate to and download account statements."
    },
    {
      "type": "code",
      "id": "parse-statement",
      "script": "learnings/parse-statement/main.py",
      "input_files": [
        "execution/download-statement/latest_statement.pdf"
      ],
      "output_files": [
        "db/transactions.json"
      ],
      "on_failure": "repair_with_llm"
    },
    {
      "type": "prevalidation",
      "id": "validate-transactions",
      "schema": "transactions_db_schema",
      "on_fail": {
        "action": "repair_same_session",
        "max_retries": 2
      }
    }
  ]
}
```

## Store Access Model

Read access should be open for the whole `message_sequence` conversation. Write access should be temporary and item-scoped.

This gives the agent all context it needs while still preventing accidental writes outside the current item.

```json
{
  "items": [
    {
      "type": "user_message",
      "id": "login",
      "write_access": {
        "knowledgebase": false,
        "db": false,
        "learnings": false
      }
    },
    {
      "type": "user_message",
      "id": "update-learning",
      "kind": "learning",
      "write_access": {
        "knowledgebase": false,
        "db": false,
        "learnings": true
      }
    }
  ]
}
```

Read paths for `knowledgebase/`, `db/`, and `learnings/_global/` are available across the sequence. Write paths are recomputed before each item.

Default write access by item kind:

| Item kind | KB write | DB write | Learnings write |
| --- | --- | --- | --- |
| (no kind) | false | false | false |
| learning | false | false | true |
| knowledgebase | true | false | false |
| db | false | true | false |
| code | based on output files | based on output files | false |
| prevalidation | false | false | false |

`kind` only meaningfully accepts `learning`, `knowledgebase`, `db`, and `code`; it drives item-scoped write access. For `code`, DB/KB write access is auto-inferred from `output_files`. A work turn that just performs the task needs no `kind`.

The builder can still allow overrides, but it should show them as item write windows, not broad sequence permissions.

## Store Access Implementation

This should be implemented as a new message-sequence permission path and should not change regular-step behavior.

### Backend Schema

Add item write access to `MessageSequenceItem` in:

```text
agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go
```

```go
type MessageSequenceWriteAccess struct {
    Knowledgebase bool `json:"knowledgebase,omitempty"`
    DB            bool `json:"db,omitempty"`
    Learnings     bool `json:"learnings,omitempty"`
}

type MessageSequencePlanStep struct {
    Type StepType `json:"type"`
    CommonStepFields
    Items []MessageSequenceItem `json:"items"`
    NextStepID string `json:"next_step_id,omitempty"`
    AgentConfigs *AgentConfigs `json:"-"`
}

type MessageSequenceItem struct {
    ID string `json:"id"`
    Type string `json:"type"`
    Kind string `json:"kind,omitempty"`
    Message string `json:"message,omitempty"`
    WriteAccess MessageSequenceWriteAccess `json:"write_access,omitempty"`
}
```

### Access Resolution

Add helpers in the new executor file:

```text
agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_message_sequence.go
```

```go
func resolveMessageSequenceItemWriteAccess(item MessageSequenceItem) MessageSequenceWriteAccess {
    access := item.WriteAccess
    if access != (MessageSequenceWriteAccess{}) {
        return access
    }

    switch item.Kind {
    case "learning":
        access.Learnings = true
    case "knowledgebase":
        access.Knowledgebase = true
    case "db":
        access.DB = true
    case "code":
        access.DB = codeItemWritesDB(item)
        access.Knowledgebase = codeItemWritesKB(item)
        access.Learnings = false
    }
    return access
}
```

For code items, infer write access from declared `output_files`. For example, `db/transactions.json` opens DB write for that item.

### Folder Guard

Do not use `setupExecutionFolderGuard` directly for this step because it always grants DB read/write today.

Add a new helper:

```go
func (hcpo *StepBasedWorkflowOrchestrator) setupMessageSequenceFolderGuard(
    stepPath string,
    stepID string,
    itemWriteAccess MessageSequenceWriteAccess,
) (readPaths []string, writePaths []string) {
    baseWorkspacePath := hcpo.GetWorkspacePath()
    runWorkspacePath := baseWorkspacePath
    if hcpo.selectedRunFolder != "" {
        runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
    }

    executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
    stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepID, stepPath)
    downloadsPath := fmt.Sprintf("%s/Downloads", executionWorkspacePath)

    readPaths = []string{
        executionWorkspacePath,
        fmt.Sprintf("%s/soul", baseWorkspacePath),
        fmt.Sprintf("%s/builder", baseWorkspacePath),
    }
    writePaths = []string{stepFolderPath, downloadsPath}

    readPaths = append(readPaths,
        getDBPath(baseWorkspacePath),
        getKnowledgebasePath(baseWorkspacePath),
        filepath.Join(baseWorkspacePath, LearningsFolderName, GlobalLearningID),
    )

    if itemWriteAccess.DB {
        writePaths = append(writePaths, getDBPath(baseWorkspacePath))
    }
    if itemWriteAccess.Knowledgebase {
        writePaths = append(writePaths, filepath.Join(getKnowledgebasePath(baseWorkspacePath), "notes"))
    }
    if itemWriteAccess.Learnings {
        writePaths = append(writePaths, filepath.Join(baseWorkspacePath, LearningsFolderName, GlobalLearningID))
    }

    return readPaths, writePaths
}
```

Important behavior:

- KB write only grants `knowledgebase/notes`, not `knowledgebase/context`.
- DB write is open only for the active DB/code item that needs it.
- Learning write grants `learnings/_global` only for the active learning item.
- Code save-back to `learnings/<step-id>/main.py` remains separate and only happens if the code item explicitly enables save-back.

### Prompt Variables

The folder guard is enforcement, but the agent prompt also needs clear instructions.

For every message-sequence agent call, include:

```text
Read access:
- knowledgebase: enabled
- db: enabled
- learnings: enabled

Temporary write access for this item:
- knowledgebase: {{.KnowledgebaseWriteEnabled}}
- db: {{.DBWriteEnabled}}
- learnings: {{.LearningsWriteEnabled}}

You may read context from all stores. Only write to stores that are enabled for this item.
```

Include concrete read paths for KB, DB, and learnings in every sequence prompt. Include write instructions only for the active item's enabled write stores.

### Frontend Types

Update:

```text
frontend/src/utils/stepConfigMatching.ts
frontend/src/services/api-types.ts
```

```ts
export interface MessageSequenceWriteAccess {
  knowledgebase?: boolean;
  db?: boolean;
  learnings?: boolean;
}

export interface MessageSequencePlanStep extends CommonStepFields {
  type: 'message_sequence';
  items: MessageSequenceItem[];
}

export interface MessageSequenceItem {
  id: string;
  type: 'user_message' | 'code' | 'prevalidation';
  kind?: 'learning' | 'knowledgebase' | 'db' | 'code';
  message?: string;
  write_access?: MessageSequenceWriteAccess;
}
```

### Builder UI

In the message-sequence editor, show read access as always enabled:

```text
Read access: KB, DB, learnings
```

Then show write access on each item:

```text
Item write access:
[ ] KB
[ ] DB
[ ] Learnings
```

Builder warnings:

- item kind `learning` but learnings write is disabled
- item kind `knowledgebase` but KB write is disabled
- item kind `db` but DB write is disabled
- code item outputs to `db/...` but DB write is disabled
- user message asks to write to a store but the item write window is disabled

Warnings should not silently widen permissions. The user or builder must explicitly enable the item write window.

### Tests

Add backend tests for:

- read paths include KB, DB, and learnings for every sequence item.
- write paths are empty for items with no kind by default.
- DB write is present only for active DB/code items.
- KB write grants `knowledgebase/notes` but not `knowledgebase/context`.
- learning write grants `learnings/_global` only for active learning items.

Add frontend type/build coverage for:

- `write_access` serializes as booleans.
- message-sequence editor shows read access globally and write access per item.

## Builder Instructions Implementation

The workflow builder needs explicit authoring instructions so it knows when to create `message_sequence`, how to shape the queue, and how to configure item write windows, Python items, route re-entry, and prevalidation.

### Where To Add Instructions

Add the instruction block to the builder/workshop prompt in:

```text
agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/interactive_workshop_manager.go
```

Place it near the existing guidance for:

- persistent stores: learnings, knowledgebase, db
- step config knobs
- execution mode / learn_code guidance
- plan modification tools

Also update plan-modification tool descriptions in the same file so builder tools accept `message_sequence` steps and items.

### Exact Builder Prompt Block

Add this text to the builder instructions:

```text
## Message Sequence Steps

Use a `message_sequence` step when the workflow needs to send a known ordered queue of user messages into one agent conversation.

Choose `message_sequence` when:
- the order of messages is known at design time
- the same agent should keep context across multiple messages within one run
- learning, KB, DB, prevalidation, and Python/code actions must happen at exact points
- the agent can handle conditional details inside each user message
- as a `todo_task` route, the orchestrator may re-enter the same specialist with a new user message within the same run (in-memory conversation; not across runs or restarts)

Do NOT use `message_sequence` when:
- the agent should dynamically choose routes or delegate work; use `todo_task`
- a single normal instruction is enough; use `regular`
- the workflow needs explicit branch routing; use `routing` or `conditional`

Message sequence rules:
- The sequence owns ordering. Do not ask the agent to invent the whole plan order.
- Each `user_message` item is sent as a user message into the same conversation.
- Prefer smaller user messages over one large message. Break a big task into multiple focused items.
- Do not model workflow-level if/else inside the sequence. Put conditional behavior inside the message text.
- Learning, KB, and DB updates are normal `user_message` items with the matching `kind` (`learning`/`knowledgebase`/`db`) for write access. They run wherever they appear in the queue.
- Add explicit reference-check, hallucination-check, critique, or self-validation `user_message` turns when quality matters. These are message techniques, not `kind` values.
- Use reference-check messages to force the agent to cite files, sources, DB rows, screenshots, logs, or prior outputs before making claims.
- Use self-validation messages to ask the same conversation to inspect its own output before backend prevalidation or before moving to the next major action.
- Prevalidation is a backend item/gate, not a normal LLM task.
- Python/code items run before involving the LLM. The LLM is called only if the code fails, validation fails, or the next item is a user message.
- If prevalidation fails after configured repair attempts, the step stops.
- Route re-entry sends only the new user message into the in-memory conversation. It does not replay the original queue.

Default message sequence settings:
- A route's conversation is remembered in memory for the current run only. Passing `message_sequence_restart=true` at execution time clears that in-memory conversation and wipes the route's on-disk runtime artifacts for a clean start. There is no per-step config and no disk-based resume.
- A standalone (top-level) sequence step has no memory; it runs the configured queue once and re-runs it on every run.
- read access: KB, DB, and learnings enabled for the whole sequence
- write access: disabled by default except item-kind defaults

Read/write access rules:
- Read access is open for KB, DB, and learnings during the whole sequence.
- Write access is item-scoped.
- If a learning item writes SKILL.md, set that item's `write_access.learnings` to true.
- If a KB item writes notes, set that item's `write_access.knowledgebase` to true.
- If a DB item writes `db/`, set that item's `write_access.db` to true.
- Do not silently widen write access. Ask or explain before enabling an item write window.

Python/code item rules:
- Use a code item for deterministic parsing, file transforms, API response normalization, or stable data processing.
- Set `runtime: "python"`.
- Set `script_path` to the source script, usually `learnings/<script-id>/main.py` or another builder-created script path.
- Set `input_files` for concrete files the script must read.
- Set `input_json` for structured variables and options.
- Set `output_files` for files the script should produce.
- Default `save_repaired_script` to false.
- On failure, prefer `on_failure.action: "repair_with_llm"` only when the agent can reasonably patch the script from logs.
- If the script is only a strict gate and should not be patched, use `on_failure.action: "stop_step"`.

Prevalidation rules:
- Add prevalidation after any message or code item whose output must be guaranteed before continuing.
- For DB writes, validate the target `db/*.json` shape and merge/key expectations.
- For learning writes, validate no secrets, OTPs, tokens, or raw credentials are stored.
- For KB writes, validate durable facts have source context and notes/index stay consistent.

Re-run rules:
- Running a `message_sequence` always runs its configured item queue.
- There is no disk-based resume: a sequence cannot continue a prior run's conversation. Conversation memory only exists in-memory for a `todo_task` route within one run.
- To iterate with memory, make it a `todo_task` route and let the orchestrator re-enter it within the same run. To run the whole thing again later, re-run the workflow (a fresh run with a fresh queue).

When creating a message sequence, produce compact item messages. Each message should tell the agent what to do now, what files/stores it may use, and what output should exist after the item. Avoid long meta-explanations.

Recommended decomposition pattern:
- gather/reference context
- perform one concrete action
- write/update one store if needed
- run a hallucination/reference check if the output contains claims
- run self-validation or backend prevalidation before the next major action
```

### Builder Tool Schema Changes

Any builder tool that creates or updates plan steps must accept `type: "message_sequence"`.

Add item schema support:

```json
{
  "type": "message_sequence",
  "id": "write-and-critique-tests",
  "title": "Write and refine test cases",
  "items": [
    {
      "type": "user_message",
      "id": "write-tests",
      "message": "Read the approved use case and existing test files. List the behaviors that need test coverage with file references.",
      "write_access": {}
    },
    {
      "type": "user_message",
      "id": "draft-tests",
      "message": "Write focused test cases for the listed behaviors. Keep the changes minimal and summarize files changed.",
      "write_access": {}
    },
    {
      "type": "code",
      "id": "run-tests",
      "runtime": "python",
      "script_path": "scripts/run_tests.py",
      "input_json": {
        "test_target": "{{test_target}}"
      },
      "output_files": [
        "db/test_results.json"
      ],
      "on_failure": {
        "action": "repair_with_llm",
        "max_retries": 2
      },
      "save_repaired_script": false,
      "write_access": {
        "db": true
      }
    },
    {
      "type": "user_message",
      "id": "reference-check-tests",
      "message": "Check the tests against the use case and test run output. Identify any unsupported assumptions or missing references before adding more code.",
      "write_access": {}
    },
    {
      "type": "user_message",
      "id": "critique-tests",
      "message": "Add missing meaningful cases found by the reference check. Do not add brittle tests.",
      "write_access": {}
    }
  ]
}
```

Builder tools should support these update operations:

- add item
- edit item
- delete item
- reorder item
- update item write access
- add inline prevalidation
- toggle a standalone sequence vs a `todo_task` route mounting
- convert regular step to message sequence only when the user asks for ordered multi-message behavior

### Builder Validation Before Saving

Before saving a `message_sequence`, the builder should check:

- every item has a stable `id`
- every `user_message` has non-empty `message`
- every `code` item has `runtime` and `script_path`
- every `code` item with `input_files` uses workspace-relative paths
- `write_access` values are booleans
- DB-writing items have item DB write access
- KB-writing items have item KB write access
- learning-writing items have item learnings write access
- prevalidation schemas reference accessible files

These checks should warn by default. They should block save only for malformed schema, missing required fields, or invalid access values.

### Builder Response Style

When the user asks for a sequence step, the builder should summarize it like this:

```text
Created message sequence: Write and refine test cases

Items:
1. write-tests — user message
2. run-tests — python code
3. critique-tests — user message

Access:
- Reads: KB, DB, learnings
- Writes: item-scoped

Re-entry:
- standalone: runs the configured queue once (re-runs on every run; no memory)
- as a todo_task route: orchestrator re-entry continues the in-memory conversation within the run and sends only the new user message
```

Do not call it "single shot". Use "message sequence" or "single conversation sequence".

## Run State Implementation

There is no disk-based session store, no session listing, no builder "resume existing" mode, and no archive. Running a `message_sequence` always runs its configured queue. The only conversation memory is the orchestrator's in-memory route cache, and it lives only for the current run.

### Running a standalone sequence

Running a standalone (top-level) `message_sequence` step:

1. Resolve the step and confirm it is `message_sequence`.
2. Build `plannedItems` from the configured queue.
3. Run each item in order, writing per-item logs and the `session.json` observability log.
4. There is no "session exists" check and no archive — the queue always runs from the start.

A standalone step has no `Restart` semantics it needs: re-running it is itself a fresh run of the queue.

### In-memory route cache

For a `todo_task` route, the orchestrator keeps the conversation in an in-memory map keyed by `step_path + step_id` (call source `orchestrator_reentry`):

```text
loadMsgSeqRouteSession(key)   // returns the in-memory conversation if this run already ran it
storeMsgSeqRouteSession(key)  // records the conversation after a call so re-entry can continue it
clearMsgSeqRouteSession(key)  // drops the conversation on restart
```

Route call mode resolution:

```text
isRoute := source == "orchestrator_reentry"

if isRoute && restart:
  clear in-memory conversation
  cleanupMessageSequenceRuntime(...)   // wipe working code copies, stdout, session.json log
  // falls through to first-entry

if isRoute && in-memory conversation exists (and not restart):
  mode = re-entry
  require a non-empty re-entry message
  plannedItems = one synthetic user_message(reentry_message)

else:
  mode = first entry (or standalone)
  plannedItems = configured queue
```

The conversation seed for re-entry comes from the in-memory cache, never from disk. After the run ends or the process restarts, the cache is gone and there is nothing to resume.

### session.json (write-only log)

The runtime writes `session.json` after each item and at the end, in both standalone and route cases:

```text
runs/{run_folder}/execution/message_sequences/{step_path}/{step_id}/session.json
```

It captures `conversation_history` and per-item `entries` for observability only. It is NEVER read back to seed or resume a conversation. There is no `loadMessageSequenceSession` and no archive step; restart wipes the runtime directory (including the log) via `cleanupMessageSequenceRuntime`.

### Restart

`message_sequence_restart=true` applies only to a route. It:

1. Clears the route's in-memory conversation.
2. Wipes the route's on-disk runtime artifacts (working code copies, stdout/stderr, the `session.json` log) so the next call starts clean.

There is no archive of the old session — the directory is simply cleaned and rebuilt.

## Prevalidation Model

Prevalidation can run:

- after a specific item
- after every item of a kind
- at the end of the whole sequence

Simple item-level form:

```json
{
  "type": "user_message",
  "id": "save-balance",
  "kind": "db",
  "message": "Update db/balances.json.",
  "prevalidation": {
    "schema": "balance_db_schema",
    "max_retries": 2
  }
}
```

Equivalent explicit form:

```json
{
  "type": "prevalidation",
  "schema": "balance_db_schema"
}
```

Validation should remain backend-controlled. The LLM receives feedback only when repair is needed.

Default failure behavior:

- If no repair message is configured, stop the step immediately.
- If repair is configured, send repair feedback into the same conversation and retry validation.
- If validation still fails after configured repair attempts, stop the step.
- Do not continue the sequence with warnings by default.

## Learning And KB Validation

Learning and KB updates can have their own validation.

Learning validation checks:

- No secrets, OTPs, passwords, tokens, or raw credentials.
- Learning is reusable, not a one-off trace.
- Includes source/run reference where useful.
- Is concise enough to remain useful.
- Does not duplicate existing learning.

KB validation checks:

- Durable facts have evidence or source context.
- Temporary guesses are not stored.
- `notes/_index.json` and note files stay consistent.
- No duplicate or contradictory facts without reconciliation.
- User-owned `knowledgebase/context/` is not modified.

DB validation checks:

- Required files exist.
- JSON shape matches schema.
- Stable keys are present.
- No duplicate rows by key.
- Required timestamps and normalized fields exist.

## Scheduling

A scheduled workflow can run the same sequence.

Each scheduled run should create:

```text
new run folder
new agent session
sequence execution log
per-item logs
validation reports
updated db/learnings/kb outputs
```

Scheduling does not change the model. It only triggers the sequence. A scheduled re-run is the supported way to "run the sequence again later" — each run is independent, with a fresh standalone queue and (for routes) a fresh in-memory conversation. No conversation state carries over from a previous run.

## Logging

The runtime should persist:

```json
{
  "sequence_id": "bank-daily-run",
  "session_id": "...",
  "items": [
    {
      "id": "login",
      "status": "completed",
      "started_at": "...",
      "completed_at": "...",
      "message": "...",
      "assistant_summary": "...",
      "files_written": []
    }
  ]
}
```

For code items, include:

```json
{
  "exit_code": 0,
  "stdout_path": "...",
  "stderr_path": "...",
  "script_path": "...",
  "prevalidation_status": "passed"
}
```

## Implementation Plan

### Phase 0: Worktree And Scope

Implement on a dedicated branch/worktree:

```text
branch: message-sequence-step-design
worktree: <workspace>/mcp-agent-builder-go-message-sequence-step-design
```

Scope for v1:

- Add the new `message_sequence` step type.
- Support configured-queue execution (standalone and first route call).
- Support in-memory orchestrator re-entry into the same route conversation within one run.
- Support user-message, code, and prevalidation items first.
- Represent learning, KB, and DB as user-message items with kind-specific labels plus item-scoped write access.

Do not change `todo_task` semantics for v1.

### Phase 1: Backend Plan Schema

Update backend step typing in:

- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_management.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/plan_orphan_refs.go`

Add:

```go
const StepTypeMessageSequence StepType = "message_sequence"
```

Add structs:

```go
type MessageSequencePlanStep struct {
    Type StepType `json:"type"`
    CommonStepFields
    Items []MessageSequenceItem `json:"items"`
    NextStepID string `json:"next_step_id,omitempty"`
    AgentConfigs *AgentConfigs `json:"-"`
}

type MessageSequenceItem struct {
    ID string `json:"id"`
    Type string `json:"type"`
    Kind string `json:"kind,omitempty"`
    Title string `json:"title,omitempty"`
    Message string `json:"message,omitempty"`
    ScriptPath string `json:"script_path,omitempty"`
    ValidationSchema *ValidationSchema `json:"validation_schema,omitempty"`
    OnFailure MessageSequenceFailurePolicy `json:"on_failure,omitempty"`
    WriteAccess MessageSequenceWriteAccess `json:"write_access,omitempty"`
}
```

Wire it into:

- `PlanStepInterface` implementation.
- `parseStepFromJSON`.
- `unmarshalStepFromJSON`.
- `convertMapToStep`.
- `mergePartialStepUpdate`.
- plan validation and ID collection.
- orphan-step clone/reuse logic.

### Phase 2: Frontend Types And Builder UI

Update frontend plan types in:

- `frontend/src/utils/stepConfigMatching.ts`
- `frontend/src/services/api-types.ts`
- generated event types if backend events are added.

Add:

```ts
export interface MessageSequencePlanStep extends CommonStepFields {
  type: 'message_sequence';
  items: MessageSequenceItem[];
  next_step_id?: string;
}

export interface MessageSequenceWriteAccess {
  knowledgebase?: boolean;
  db?: boolean;
  learnings?: boolean;
}

export interface MessageSequenceItem {
  id: string;
  type: 'user_message' | 'code' | 'prevalidation';
  kind?: 'learning' | 'knowledgebase' | 'db' | 'code';
  message?: string;
  write_access?: MessageSequenceWriteAccess;
}
```

Update the builder/editor:

- Add `message_sequence` as a selectable step type.
- Add an ordered item editor with add/remove/reorder.
- Add item kinds: learning, knowledgebase, db, code.
- Show global read access for KB, DB, and learnings.
- Add item-scoped write access controls for KB, DB, and learnings.
- Add a single `Run` control that runs the configured queue. There is no disk-based resume control: standalone runs re-run the queue, and route re-entry is driven by the orchestrator in memory within a run.
- Add the builder prompt/tool instructions from "Builder Instructions Implementation" so the builder can author and validate this step type.

Likely UI files:

- `frontend/src/components/events/orchestrator/StepEditPanel.tsx`
- workflow plan editor components that render step type selectors and nested/orphan steps.
- any step display helper that switches on `step.type`.

### Phase 3: Runtime Executor

Create a backend executor:

```text
agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_message_sequence.go
```

Add dispatch in the main execution loop in:

```text
agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_execution.go
```

Dispatch before the regular-step fallback:

```text
if step.StepType() == StepTypeMessageSequence:
  executeMessageSequenceStep(...)
```

Runtime shape:

```text
isRoute := source == "orchestrator_reentry"

if isRoute and restart:
  clear in-memory route conversation
  cleanupMessageSequenceRuntime (wipe working copies, stdout, session.json log)

look up in-memory route conversation (route only; never from disk)
configure sequence read access and active item write access
build planned_items:
  in-memory route conversation exists -> one user_message from orchestrator (re-entry)
  otherwise (first route call or standalone) -> configured queue

for item in planned_items:
  run item
  run item prevalidation if configured
  write per-item entry + session.json (observability log only)
  if validation fails after repair attempts:
    stop the step

store the route conversation back in the in-memory cache (route only)
write session.json (observability log only)
return sequence summary + conversation history as execution result
```

The executor should reuse existing regular execution primitives where possible, but it should not call `executeSingleStep` for every user message because that would create fresh agent conversations. It needs one conversation history for the whole sequence step (kept in memory; for a route it is also held in the orchestrator's route cache across calls within the run).

### Orchestrator Step-Type And State Selection

The parent orchestrator learns about `message_sequence` in the same way it learns about other typed steps: the plan parser unmarshals the route's `sub_agent_step` into a concrete `MessageSequencePlanStep`, and runtime checks `StepType()`.

Backend files:

```text
agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go
agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_todo_task.go
agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/todo_task_orchestrator_agent.go
```

#### Step Type Recognition

Add the new type:

```go
const StepTypeMessageSequence StepType = "message_sequence"
```

`MessageSequencePlanStep` implements:

```go
func (m *MessageSequencePlanStep) StepType() StepType {
    return StepTypeMessageSequence
}
```

Then update parsing paths so `sub_agent_step` can be this type:

```text
parseStepFromJSON
unmarshalStepFromJSON
convertMapToStep
clonePlanStep
cloneStepWithDelegationOverrides
```

That is enough for a predefined todo route to hold:

```json
{
  "route_id": "write-tests",
  "route_name": "Write tests",
  "sub_agent_step": {
    "type": "message_sequence",
    "id": "write-tests-sequence",
    "items": []
  }
}
```

#### Execution Dispatch From Todo Orchestrator

Today `executeRoutedSubAgentStep` special-cases nested `todo_task`; otherwise it calls `executeSingleStep`.

Add a second special case before `executeSingleStep`:

```go
if isMessageSequenceStep(stepToExecute) {
    return hcpo.executeMessageSequenceStep(
        ctx,
        stepToExecute,
        stepIndex,
        subAgentStepPath,
        progress,
        localExecCtx,
        allSteps,
        MessageSequenceCallOptions{
            Source: "orchestrator",
            RouteID: routeID,
            TodoID: todoID,
            ReentryMessage: instructions,
        },
    )
}
```

The exact signature can vary, but it needs the route ID, todo ID, and delegated instructions because those decide whether this is first entry or re-entry.

#### First Entry vs Existing Conversation

The message sequence executor decides call mode against the in-memory route cache (never from disk):

```text
isRoute = source == "orchestrator_reentry"

if isRoute and caller requested restart:
  clear in-memory route conversation
  cleanupMessageSequenceRuntime (wipe working copies, stdout, session.json log)
  // falls through to first-entry

if isRoute and in-memory route conversation exists (and not restart):
  mode = orchestrator_reentry
  require a non-empty reentry_message (error if missing)
  planned_items = one synthetic user_message(reentry_message)

else:  // first route call, or any standalone run
  mode = first_entry
  planned_items = configured queue
```

A standalone (non-route) run is always first-entry: it has no in-memory cache, so it just runs the configured queue. Re-running a standalone step is not an error. The "session already exists; provide a re-entry message or restart" footgun does not exist; the only place a missing re-entry message is an error is when an in-memory route conversation already exists and the orchestrator re-enters without instructions.

#### In-Memory Route Key

The route conversation is held in an in-memory map on the orchestrator, keyed by:

```text
sub_agent_step_path + message_sequence_step_id
```

This key scopes the conversation to one mounted route within one run. It is NOT a disk path and is never persisted as resumable state. The `session.json` written on disk uses the same step path / step id only as an observability log location:

```text
runs/iteration-1/execution/message_sequences/
  step-2-sub-write-tests/
    write-tests-sequence/
      session.json   # write-only log; never read back
```

Why include `sub_agent_step_path`:

- the same reusable orphan sequence could be mounted under two routes
- each mounted route should get its own in-memory conversation
- repeated calls to the same mounted route in the same run continue the same in-memory conversation

#### Where Re-Entry Message Comes From

For todo orchestrator routes, re-entry message should come from:

```text
TodoTaskResponse.InstructionsToSubAgent
```

On first call, `InstructionsToSubAgent` can be added as high-priority context before the configured queue starts.

On later calls, `InstructionsToSubAgent` becomes the single re-entry user message.

Example:

```text
First route call (this run):
  route = write-tests
  no in-memory conversation yet
  run configured queue:
    1. write initial tests

Second route call (same run):
  route = write-tests
  in-memory conversation exists
  instructions = "Critique found missing auth edge cases. Add coverage."
  send one user message into the in-memory conversation
```

#### Orchestrator Prompt/Tool Instruction

Update `todo_task_orchestrator_agent.go` route-tool guidance:

```text
Some predefined routes may be message sequence routes.

For a message sequence route:
- First call (this run) starts the sequence and sends its configured queue.
- Later calls to the same route in the same run continue the in-memory route conversation.
- Your `instructions` argument becomes the re-entry user message on later calls.
- Do not ask to replay the original queue unless you intentionally want a restart.
- Use the same route again when critique/test/output feedback should go back to the original specialist with its prior context.
- The conversation lives only for this run. It does not carry over to a future run or survive a process restart.
```

The route description returned by `get_route_description(route_id)` should include:

```text
Step type: message_sequence
Conversation: continues in memory within this orchestrator run only
First call: sends configured queue
Re-entry: sends your instructions as the next user message
```

#### State Stored For Orchestrator

The in-memory route conversation (and the write-only `session.json` log) records entries per item:

```json
{
  "step_id": "write-tests-sequence",
  "conversation_history": [],
  "entries": [
    {
      "source": "configured_queue",
      "status": "completed"
    },
    {
      "source": "orchestrator_reentry",
      "status": "completed"
    }
  ]
}
```

The orchestrator keeps the conversation in its in-memory route cache for the run. It does not need the whole history in its own prompt; it only needs the per-item summaries. The detailed history lives in memory (and is mirrored to the write-only `session.json` log).

### Phase 4: In-Memory Conversation State

There is no disk-based session store. Conversation state for a route lives only in the orchestrator's in-memory route cache for the duration of the run; standalone steps keep no state at all.

The write-only observability log is written here (never read back):

```text
runs/{run_folder}/execution/message_sequences/{step_path}/{step_id}/session.json
```

Log shape:

```json
{
  "session_id": "write-tests-sequence",
  "step_id": "write-tests-sequence",
  "run_folder": "iteration-1",
  "status": "completed",
  "created_at": "...",
  "updated_at": "...",
  "conversation_history": [],
  "entries": [
    {
      "entry_id": "initial",
      "source": "configured_queue|orchestrator_reentry",
      "item_id": "write-tests-main",
      "status": "completed"
    }
  ]
}
```

Identity rules:

- The in-memory route key is `sub_agent_step_path + message_sequence_step_id`, scoped to the current run.
- Standalone steps have no key — they always run the configured queue.
- Restart clears the in-memory conversation and wipes the runtime directory (no archive).

Implementation detail:

- Store `conversation_history` as `[]llmtypes.MessageContent` in the in-memory cache.
- Helpers: `loadMsgSeqRouteSession`, `storeMsgSeqRouteSession`, `clearMsgSeqRouteSession` (in-memory), plus `saveMessageSequenceSession` (write-only log). There is no `loadMessageSequenceSession` — the log is never read back.

### Phase 5: Agent Execution

Use a single execution-capable agent for the sequence conversation.

For each `user_message` item:

- Build a user message from item text plus item metadata.
- Include previous item summaries only if needed; the full conversation already has prior context.
- Execute with the existing conversation history.
- Capture the updated conversation history after each item.

For route re-entry:

- Take the prior conversation history from the in-memory route cache.
- Append the new user message from the orchestrator.
- Execute once.
- Store the updated history back in the in-memory route cache.

Backend changes likely touch:

- `controller_agent_factory.go` if a new sequence-specific execution agent factory is needed.
- `execution_only_agent.go` or existing execution agent code if it can already accept injected history.
- `controller_types.go` if `ExecutionContext` needs a `ConversationHistorySeed` in addition to `ConversationHistoryCapture`.

### Phase 6: Code Items

For code items, reuse learn-code/script infrastructure from:

- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_learn_code.go`
- `agent_go/docs/learn_code_flow.md`

Behavior:

- Copy `script_path` into the code item's run folder.
- Write `input.json` with resolved input files, structured input, expected outputs, read access, and active item write access.
- Execute `python3 -B code/main.py ...` through the workspace shell API.
- Save stdout/stderr, exit code, and `result.json` under the sequence item log folder.
- If success, run optional prevalidation and add a compact runtime context block for the next LLM user-message item.
- If failure and repair is enabled, send a repair user message into the same sequence conversation with:
  - working-copy script path
  - input contract path
  - exit code
  - stdout/stderr snippets and paths
  - expected outputs
  - instruction to patch and rerun the working copy
- Let the agent patch/rerun according to the configured policy.
- Runtime reruns the working copy after each repair turn.
- Do not save repaired script back to canonical learnings by default.

### Phase 7: Prevalidation And Stop Policy

Reuse existing schema validation where possible:

- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/pre_validation.go`
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/validation_types.go`

For each prevalidation item or inline item prevalidation:

```text
run validation
if pass:
  continue
if fail and repair configured:
  send repair feedback into same conversation
  retry validation
if still fail:
  mark sequence failed
  stop the step
```

Learning and KB validation can initially be specialized prevalidation schemas plus kind-specific checks.

### Phase 8: Orchestrator Integration

For `todo_task` routes and reusable orphan steps:

- Allow `message_sequence` as a `sub_agent_step`.
- When todo orchestrator chooses a route pointing to a message sequence:
  - first call (this run) runs the configured queue.
  - later call to the same route in the same run continues the in-memory conversation with a re-entry message.
- Hold the route conversation in the orchestrator's in-memory route cache, keyed by `sub_agent_step_path + step_id`.
- Use the call-mode rules from "Orchestrator Step-Type And State Selection".

Likely backend touchpoints:

- `controller_todo_task.go`
- `todo_task_orchestrator_agent.go`
- `controller_agent_factory.go` tools that execute sub-agents or inspect sub-agent conversations.

The orchestrator continues an in-memory conversation only if one already exists for the route in this run. If a route conversation exists and the orchestrator re-enters without a re-entry message, that is an error; otherwise the call is a first-entry that runs the configured queue.

### Phase 9: Standalone Run (no resume API)

There is no builder resume API, no session-listing endpoint, and no `start_from_beginning`/`resume_existing` modes. Running a standalone `message_sequence` always runs the configured queue.

Required behavior:

- run the configured queue for a standalone step
- write the `session.json` observability log (never read it back)
- for a route, restart (`message_sequence_restart=true`) clears the in-memory conversation and wipes the runtime directory via `cleanupMessageSequenceRuntime`

Likely touchpoints:

- workflow execution API handler that powers `ExecuteStepForWorkshop`.
- `controller_workshop.go`.

### Phase 10: Events And Logs

Add sequence-specific events so the UI is debuggable:

```text
message_sequence_started
message_sequence_item_started
message_sequence_item_completed
message_sequence_item_failed
message_sequence_prevalidation_completed
message_sequence_reentry_started
message_sequence_completed
```

Persist item logs under:

```text
runs/{run_folder}/execution/message_sequences/{step_id}/items/{item_id}/
```

Each item should write:

- input message
- assistant summary
- tool/conversation history reference
- files written
- validation result
- code stdout/stderr if applicable

### Phase 11: Tests

Backend tests:

- plan JSON unmarshals and marshals `message_sequence`.
- step config matching still works by ID.
- main execution dispatch calls `executeMessageSequenceStep`.
- first entry runs configured items in order.
- route re-entry continues the in-memory conversation and does not replay the queue.
- a standalone step re-runs its configured queue (no "session exists" error).
- route restart clears the in-memory conversation and wipes the runtime directory.
- `session.json` is written but never read back to seed a conversation.
- prevalidation failure stops the step.
- code item success continues; code item failure can send repair message.

Frontend tests or type checks:

- `PlanStep` union accepts `message_sequence`.
- builder can edit item queue.
- item `write_access` fields serialize correctly.
- the run control produces the expected API payload.

Run checks:

```text
go test ./agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/...
./node_modules/.bin/tsc -b
```

## Resolved Decisions

1. `message_sequence` is a new step type.
2. Learning and KB messages can run anywhere in the sequence. Their timing depends only on item order.
3. Per-message max turns are not needed.
4. Read access is open for the full sequence; write access is item-scoped for KB, DB, and learnings.
5. Prevalidation failures stop the step after configured repair attempts are exhausted.
6. When an orchestrator re-enters the same route within one run, it continues the in-memory conversation and sends a new user message. This memory is not persisted to disk and does not survive across runs or process restarts.
7. A standalone (top-level) `message_sequence` step has no memory and no re-entry; it runs the configured queue once and re-runs it on every run.
8. `session.json` is a write-only observability log; it is never read back to resume a conversation. There is no session archiving.

## Open Question

1. Should code-item repair save changes back to `learnings/<id>/main.py`, or only patch the run copy?

## Recommended Defaults

- New step type: `message_sequence`.
- A route's conversation is remembered in memory for the current run only; `message_sequence_restart=true` clears it and wipes the runtime directory (no per-step config, no disk-based resume). A standalone step keeps no memory and re-runs its queue.
- Route re-entry sends only the new user message into the in-memory conversation.
- No workflow-level `if/else`; conditionals live inside user messages.
- Learning and KB updates run wherever the sequence places them.
- Prevalidation failures stop the step after configured repair attempts are exhausted.
- Code repair does not save back by default.
- Reads are available for the whole sequence; writes are opened only for the active item.
