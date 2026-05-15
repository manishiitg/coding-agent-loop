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
  "session_mode": "single_conversation",
  "conversation_scope": "resume_within_orchestrator_run",
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

If the step is nested inside an orchestrator and the orchestrator calls the same step again, the step can resume its existing agent conversation instead of starting from scratch.

```text
first call to write-tests:
  create conversation A
  send configured message queue

later orchestrator calls write-tests again:
  resume conversation A
  send orchestrator-provided re-entry user message
```

This allows critique or execution feedback to loop back into the same writer agent with its existing context.

## Design Principles

- The workflow controls ordering.
- The agent handles conditional logic inside messages.
- A sequence step can resume its own conversation when an orchestrator re-enters it.
- Prevalidation is a backend gate, not a normal LLM task.
- Learning, knowledgebase, DB, execution, and code can all be explicit sequence items.
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

## Conversation Re-Entry

A `message_sequence` step can be called more than once by a parent orchestrator or manually from the workflow builder.

Recommended config:

```json
{
  "conversation_scope": "resume_within_orchestrator_run",
  "reentry_policy": "resume_existing",
  "reentry_message_source": "orchestrator|builder"
}
```

Orchestrator behavior:

- First entry creates the agent conversation and sends the configured item queue.
- The step completes, but the conversation handle is kept in the orchestrator run state.
- If the orchestrator calls the same sequence step again, runtime resumes that conversation.
- Re-entry sends a new user message provided by the orchestrator.
- Re-entry does not replay the original queue unless explicitly requested.

Example:

```text
1. write-tests sequence runs and creates tests
2. execute-tests sequence runs tests
3. critique-tests sequence finds missing cases
4. orchestrator calls write-tests again with:
   "Use this critique and update the tests: {{critique.output}}"
5. write-tests resumes its original conversation and updates the tests
```

The identity key should be the sequence step instance inside the current orchestrator run. That means two different sequence steps do not share conversation state, but the same step can continue when re-entered.

Builder behavior:

- The builder can start the sequence from the beginning.
- The builder can resume an existing sequence conversation.
- On resume, the builder sends a new user message into the same conversation.
- Resume does not replay the original queue unless the builder explicitly chooses restart.
- Builder resume should show the previous run/session being resumed so the user does not accidentally continue the wrong conversation.

## Item Types

### User Message Item

Sends a user message into the same agent session.

```json
{
  "type": "user_message",
  "id": "login",
  "kind": "execution",
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
  "session_mode": "single_conversation",
  "items": [
    {
      "type": "user_message",
      "id": "login",
      "kind": "execution",
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
      "kind": "execution",
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
      "kind": "execution",
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
      "kind": "execution",
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
| execution | false | false | false |
| learning | false | false | true |
| knowledgebase | true | false | false |
| db | false | true | false |
| code | false | based on output files | false |
| prevalidation | false | false | false |

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
    SessionMode string `json:"session_mode,omitempty"`
    ConversationScope string `json:"conversation_scope,omitempty"`
    ReentryPolicy string `json:"reentry_policy,omitempty"`
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
  kind?: 'execution' | 'learning' | 'knowledgebase' | 'db';
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
- write paths are empty for execution items by default.
- DB write is present only for active DB/code items.
- KB write grants `knowledgebase/notes` but not `knowledgebase/context`.
- learning write grants `learnings/_global` only for active learning items.

Add frontend type/build coverage for:

- `write_access` serializes as booleans.
- message-sequence editor shows read access globally and write access per item.

## Builder Instructions Implementation

The workflow builder needs explicit authoring instructions so it knows when to create `message_sequence`, how to shape the queue, and how to configure item write windows, Python items, resume, and prevalidation.

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

Use a `message_sequence` step when the workflow needs to send a known ordered queue of user messages into one persistent agent conversation.

Choose `message_sequence` when:
- the order of messages is known at design time
- the same agent should keep context across multiple messages
- learning, KB, DB, prevalidation, and Python/code actions must happen at exact points
- the agent can handle conditional details inside each user message
- a parent orchestrator or the builder may later resume the same conversation with a new user message

Do NOT use `message_sequence` when:
- the agent should dynamically choose routes or delegate work; use `todo_task`
- a single normal instruction is enough; use `regular`
- the workflow needs explicit branch routing; use `routing` or `conditional`

Message sequence rules:
- The sequence owns ordering. Do not ask the agent to invent the whole plan order.
- Each `user_message` item is sent as a user message into the same conversation.
- Prefer smaller user messages over one large message. Break a big task into multiple focused items.
- Do not model workflow-level if/else inside the sequence. Put conditional behavior inside the message text.
- Learning, KB, and DB updates are normal `user_message` items with kind labels. They run wherever they appear in the queue.
- Add explicit reference-check, hallucination-check, critique, or self-validation messages when quality matters.
- Use reference-check items to force the agent to cite files, sources, DB rows, screenshots, logs, or prior outputs before making claims.
- Use self-validation items to ask the same conversation to inspect its own output before backend prevalidation or before moving to the next major action.
- Prevalidation is a backend item/gate, not a normal LLM task.
- Python/code items run before involving the LLM. The LLM is called only if the code fails, validation fails, or the next item is a user message.
- If prevalidation fails after configured repair attempts, the step stops.
- Re-entry/resume sends only the new user message. It does not replay the original queue unless the user explicitly chooses restart.

Default message sequence settings:
- `session_mode`: `single_conversation`
- `conversation_scope`: `resume_within_orchestrator_run`
- `reentry_policy`: `resume_existing`
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

Builder resume rules:
- The builder can start a message sequence from the beginning.
- The builder can resume an existing sequence conversation by selecting a prior run/session and sending one new user message.
- On resume, show the run/session being resumed.
- Reject resume without a new message.
- Do not replay the original item queue on resume unless the user explicitly chooses restart.

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
  "session_mode": "single_conversation",
  "conversation_scope": "resume_within_orchestrator_run",
  "reentry_policy": "resume_existing",
  "items": [
    {
      "type": "user_message",
      "id": "write-tests",
      "kind": "execution",
      "message": "Read the approved use case and existing test files. List the behaviors that need test coverage with file references.",
      "write_access": {}
    },
    {
      "type": "user_message",
      "id": "draft-tests",
      "kind": "execution",
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
      "kind": "execution",
      "message": "Check the tests against the use case and test run output. Identify any unsupported assumptions or missing references before adding more code.",
      "write_access": {}
    },
    {
      "type": "user_message",
      "id": "critique-tests",
      "kind": "execution",
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
- update resume policy
- add inline prevalidation
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
- resume policy is explicit when the sequence may be nested inside an orchestrator

These checks should warn by default. They should block save only for malformed schema, missing required fields, invalid access values, or resume without a message.

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

Resume:
- first run sends the queue
- resume sends only the new user message
```

Do not call it "single shot". Use "message sequence" or "single conversation sequence".

## Builder Start/Resume State Implementation

Builder start/resume is the manual version of orchestrator re-entry. The user chooses whether to replay the configured queue from scratch or continue a previous sequence conversation with one new message.

### Builder UI

For every `message_sequence` step, show run controls:

```text
Start from beginning
Resume conversation
```

`Start from beginning` requires:

- run folder / group selection
- optional initial builder instruction
- confirmation if an existing session will be archived

`Resume conversation` requires:

- run folder / group selection
- selected existing sequence session
- new user message

The resume dialog should show:

```text
Run: iteration-1
Sequence: write-tests
Last updated: 2026-05-14 15:30
Status: completed
Last entry: critique feedback applied
```

Do not allow resume without a message.

### Session Listing

Add an API that lists available message sequence sessions for a step.

Request:

```text
GET /api/workflow/message-sequences/sessions?workspace_path=...&step_id=write-tests&run_folder=iteration-1
```

Response:

```json
{
  "sessions": [
    {
      "session_id": "step-2-sub-write-tests/write-tests-sequence",
      "step_id": "write-tests-sequence",
      "run_folder": "iteration-1",
      "status": "completed",
      "created_at": "...",
      "updated_at": "...",
      "entry_count": 2,
      "last_entry_source": "builder_resume",
      "last_entry_summary": "Added missing auth edge case tests."
    }
  ]
}
```

Backend reads:

```text
runs/{run_folder}/execution/message_sequences/**/session.json
```

Filter by `step_id` when supplied.

### Start From Beginning

Start-from-scratch request:

```json
{
  "step_id": "write-tests-sequence",
  "run_folder": "iteration-1",
  "mode": "start_from_beginning",
  "message": "Optional builder instruction for this run."
}
```

Backend behavior:

1. Resolve the step and confirm it is `message_sequence`.
2. Resolve the target session path for this run and step.
3. If `session.json` exists, archive it:

```text
runs/{run}/execution/message_sequences/{session_key}/archive/{timestamp}/session.json
```

4. Create a new empty session.
5. Run the configured item queue from item 1.
6. If `message` is non-empty, inject it as high-priority initial context before the first configured item. Do not replace the queue.
7. Persist the new conversation history and item results.

Start from beginning always replays the configured queue.

### Resume Existing Conversation

Resume request:

```json
{
  "step_id": "write-tests-sequence",
  "run_folder": "iteration-1",
  "mode": "resume_existing",
  "session_id": "step-2-sub-write-tests/write-tests-sequence",
  "message": "Use the critique and add missing auth edge cases."
}
```

Backend behavior:

1. Validate `message` is non-empty.
2. Load the selected `session.json`.
3. Restore `conversation_history`.
4. Create one synthetic planned item:

```json
{
  "type": "user_message",
  "id": "builder-resume-<timestamp>",
  "kind": "execution",
  "message": "Use the critique and add missing auth edge cases."
}
```

5. Send only that message into the existing conversation.
6. Do not replay configured items.
7. Save updated `conversation_history`.
8. Append a new session entry with `source: "builder_resume"`.

Resume existing always continues the selected conversation.

### Backend API Shape

Prefer adding a dedicated controller method:

```go
func (hcpo *StepBasedWorkflowOrchestrator) ExecuteMessageSequenceForBuilder(
    ctx context.Context,
    req BuilderMessageSequenceRunRequest,
) (BuilderMessageSequenceRunResponse, error)
```

Request:

```go
type BuilderMessageSequenceRunRequest struct {
    StepID    string `json:"step_id"`
    RunFolder string `json:"run_folder"`
    Mode      string `json:"mode"` // start_from_beginning | resume_existing
    SessionID string `json:"session_id,omitempty"`
    Message   string `json:"message,omitempty"`
}
```

Response:

```go
type BuilderMessageSequenceRunResponse struct {
    StepID    string `json:"step_id"`
    SessionID string `json:"session_id"`
    Mode      string `json:"mode"`
    Status    string `json:"status"`
    Summary   string `json:"summary"`
}
```

This can internally reuse `executeMessageSequenceStep`, but the builder API should not pretend this is a normal `run_single_step` call because resume semantics are different.

### Workshop Option Integration

If we want to reuse `ExecuteStepForWorkshop`, extend `WorkshopExecuteOptions`:

```go
type WorkshopExecuteOptions struct {
    // existing fields...
    MessageSequenceMode      string // start_from_beginning | resume_existing
    MessageSequenceSessionID string
    MessageSequenceMessage   string
}
```

Then `ExecuteStepForWorkshop` should special-case `StepTypeMessageSequence`:

```text
if target step is message_sequence and MessageSequenceMode is set:
  call ExecuteMessageSequenceForBuilder
  return its summary
else:
  use existing run_single_step path
```

This avoids cleanup logic deleting the session on resume.

### State Restore

Restoring state means loading:

```text
session.json.conversation_history
session.json.entries
session.json.last_runtime_context
```

The executor uses `conversation_history` as the seed for the next LLM call. It does not summarize and restart unless the normal LLM/provider layer requires compaction.

Minimal session fields for restore:

```json
{
  "session_id": "step-2-sub-write-tests/write-tests-sequence",
  "step_id": "write-tests-sequence",
  "run_folder": "iteration-1",
  "status": "completed",
  "conversation_history": [],
  "last_runtime_context": {
    "items_completed": ["write-tests", "run-tests"],
    "outputs": ["db/test_results.json"]
  },
  "entries": []
}
```

### Frontend Service Methods

Add methods in:

```text
frontend/src/services/api.ts
frontend/src/services/api-types.ts
```

```ts
listMessageSequenceSessions(params): Promise<MessageSequenceSessionSummary[]>

runMessageSequenceFromBuilder({
  step_id,
  run_folder,
  mode,
  session_id,
  message,
}): Promise<MessageSequenceRunResponse>
```

Frontend must send:

- `mode: "start_from_beginning"` for restart
- `mode: "resume_existing"` for continue
- `session_id` only for resume
- `message` required for resume

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

Scheduling does not change the model. It only triggers the sequence.

## Logging

The runtime should persist:

```json
{
  "sequence_id": "bank-daily-run",
  "session_id": "...",
  "items": [
    {
      "id": "login",
      "kind": "execution",
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
worktree: /Users/mipl/ai-work/mcp-agent-builder-go-message-sequence-step-design
```

Scope for v1:

- Add the new `message_sequence` step type.
- Support first-run queue execution.
- Support orchestrator re-entry into the same sequence conversation.
- Support builder start/resume controls.
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
    SessionMode string `json:"session_mode,omitempty"`
    ConversationScope string `json:"conversation_scope,omitempty"`
    ReentryPolicy string `json:"reentry_policy,omitempty"`
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
  session_mode?: 'single_conversation';
  conversation_scope?: 'resume_within_orchestrator_run';
  reentry_policy?: 'resume_existing' | 'fresh_each_call';
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
  kind?: 'execution' | 'learning' | 'knowledgebase' | 'db';
  message?: string;
  write_access?: MessageSequenceWriteAccess;
}
```

Update the builder/editor:

- Add `message_sequence` as a selectable step type.
- Add an ordered item editor with add/remove/reorder.
- Add item kinds: execution, learning, knowledgebase, db, code, prevalidation.
- Show global read access for KB, DB, and learnings.
- Add item-scoped write access controls for KB, DB, and learnings.
- Add run controls:
  - `Start from beginning`
  - `Resume existing conversation`
- For resume, show the target run/session and require a new user message.
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
resolve call mode:
  first_entry
  orchestrator_reentry
  builder_resume

load or create sequence session
configure sequence read access and active item write access
build planned_items:
  first_entry -> configured queue
  orchestrator_reentry -> one user_message from orchestrator
  builder_resume -> one user_message from builder

for item in planned_items:
  persist item_started
  run item
  run item prevalidation if configured
  persist item_completed or item_failed
  if validation fails after repair attempts:
    stop the step

persist sequence session
return sequence summary as execution result
```

The executor should reuse existing regular execution primitives where possible, but it should not call `executeSingleStep` for every user message because that would create fresh agent conversations. It needs one conversation history for the whole sequence step.

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

The message sequence executor should decide call mode with this order:

```text
session = load sequence session by key

if caller explicitly requested restart:
  mode = first_entry
  archive/delete old session
  planned_items = configured queue

else if session does not exist:
  mode = first_entry
  planned_items = configured queue

else if caller supplied a non-empty reentry_message:
  mode = orchestrator_reentry
  planned_items = one synthetic user_message(reentry_message)

else:
  fail clearly:
    "message_sequence session already exists; provide reentry_message or restart"
```

This avoids accidental replay. The orchestrator cannot silently start over once a sequence session exists.

#### Session Key

Use a stable key so the same route reuses the same conversation inside the same orchestrator run:

```text
run_folder + parent_orchestrator_step_id + sub_agent_step_path + message_sequence_step_id
```

Example:

```text
runs/iteration-1/execution/message_sequences/
  step-2-sub-write-tests/
    write-tests-sequence/
      session.json
```

Why include `sub_agent_step_path`:

- the same reusable orphan sequence could be mounted under two routes
- each mounted route should get its own conversation
- repeated calls to the same mounted route should resume the same conversation

#### Where Re-Entry Message Comes From

For todo orchestrator routes, re-entry message should come from:

```text
TodoTaskResponse.InstructionsToSubAgent
```

On first call, `InstructionsToSubAgent` can be added as high-priority context before the configured queue starts.

On later calls, `InstructionsToSubAgent` becomes the single re-entry user message.

Example:

```text
First route call:
  route = write-tests
  session missing
  run configured queue:
    1. write initial tests

Second route call:
  route = write-tests
  session exists
  instructions = "Critique found missing auth edge cases. Add coverage."
  send one user message into existing conversation
```

#### Orchestrator Prompt/Tool Instruction

Update `todo_task_orchestrator_agent.go` route-tool guidance:

```text
Some predefined routes may be message sequence routes.

For a message sequence route:
- First call starts the sequence and sends its configured queue.
- Later calls to the same route resume the existing route conversation.
- Your `instructions` argument becomes the re-entry user message on later calls.
- Do not ask to replay the original queue unless you intentionally want a restart.
- Use the same route again when critique/test/output feedback should go back to the original specialist with its prior context.
```

The route description returned by `get_route_description(route_id)` should include:

```text
Step type: message_sequence
Conversation: resumes within this orchestrator run
First call: sends configured queue
Re-entry: sends your instructions as the next user message
```

#### State Stored For Orchestrator

The session file should also record caller metadata:

```json
{
  "step_id": "write-tests-sequence",
  "parent_step_id": "write-test-orchestrator",
  "sub_agent_step_path": "step-2-sub-write-tests",
  "route_id": "write-tests",
  "conversation_history": [],
  "entries": [
    {
      "source": "configured_queue",
      "todo_id": "todo-1",
      "instructions": "Write the initial test cases.",
      "status": "completed"
    },
    {
      "source": "orchestrator_reentry",
      "todo_id": "todo-3",
      "instructions": "Add missing auth edge cases from critique.",
      "status": "completed"
    }
  ]
}
```

The orchestrator does not need to keep the whole conversation in its own prompt. It only needs the session reference and item summary. The detailed history stays in the message-sequence session file.

### Phase 4: Conversation Session Persistence

Add a small session store for message sequences.

Suggested path:

```text
runs/{run_folder}/execution/message_sequences/{step_id}/session.json
```

Session shape:

```json
{
  "step_id": "write-tests",
  "conversation_id": "write-tests",
  "created_at": "...",
  "updated_at": "...",
  "conversation_history": [],
  "entries": [
    {
      "entry_id": "initial",
      "source": "configured_queue|orchestrator_reentry|builder_resume",
      "items_run": ["write-tests-main"],
      "status": "completed"
    }
  ]
}
```

Identity rules:

- For normal workflow execution, key by run folder + step ID.
- For orchestrator re-entry, key by parent orchestrator run + nested sequence step ID.
- For builder resume, user selects the run/session explicitly.
- Restart creates a new session or archives the old one before replaying the configured queue.

Implementation detail:

- Store `conversation_history` as `[]llmtypes.MessageContent`.
- Add helpers such as `loadMessageSequenceSession`, `saveMessageSequenceSession`, and `appendMessageSequenceEntry`.
- Use atomic writes if available in the repo.

### Phase 5: Agent Execution

Use a single execution-capable agent for the sequence conversation.

For each `user_message` item:

- Build a user message from item text plus item metadata.
- Include previous item summaries only if needed; the full conversation already has prior context.
- Execute with the existing conversation history.
- Capture the updated conversation history after each item.

For re-entry:

- Load the prior conversation history.
- Append the new user message from orchestrator or builder.
- Execute once.
- Save updated history.

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
  - first call runs the configured queue.
  - later call to the same sequence step can pass a re-entry message.
- Store the sequence session reference where todo-task route execution can find it.
- Use the call-mode rules from "Orchestrator Step-Type And State Selection".

Likely backend touchpoints:

- `controller_todo_task.go`
- `todo_task_orchestrator_agent.go`
- `controller_agent_factory.go` tools that execute sub-agents or inspect sub-agent conversations.

The orchestrator should explicitly choose re-entry. If it does not pass a re-entry message, runtime should treat the call as a normal first-entry or fail with a clear error if a session already exists and replay is not requested.

### Phase 9: Builder Start/Resume API

Implement the detailed flow from "Builder Start/Resume State Implementation".

Required behavior:

- list existing sequence sessions for a step/run
- start from beginning by archiving old session and replaying the configured queue
- resume existing by loading selected `session.json` and sending one new user message
- reject resume if no session exists
- reject resume without a message
- avoid normal single-step cleanup when resuming because cleanup could delete the session being restored

Likely touchpoints:

- workflow execution API handler that powers `ExecuteStepForWorkshop`.
- `controller_workshop.go`.
- frontend service methods in `frontend/src/services/api.ts`.

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
- re-entry resumes existing conversation and does not replay the queue.
- builder resume rejects missing session or empty message.
- prevalidation failure stops the step.
- code item success continues; code item failure can send repair message.

Frontend tests or type checks:

- `PlanStep` union accepts `message_sequence`.
- builder can edit item queue.
- item `write_access` fields serialize correctly.
- start/resume controls produce the expected API payload.

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
6. When an orchestrator re-enters the same sequence step, it can resume the existing conversation and send a new user message.
7. The workflow builder can also start a sequence from the beginning or resume an existing sequence conversation with a new user message.

## Open Question

1. Should code-item repair save changes back to `learnings/<id>/main.py`, or only patch the run copy?

## Recommended Defaults

- New step type: `message_sequence`.
- Session mode: `single_conversation`.
- Conversation scope: `resume_within_orchestrator_run`.
- Re-entry or builder resume sends only the new user message.
- No workflow-level `if/else`; conditionals live inside user messages.
- Learning and KB updates run wherever the sequence places them.
- Prevalidation failures stop the step after configured repair attempts are exhausted.
- Code repair does not save back by default.
- Reads are available for the whole sequence; writes are opened only for the active item.
