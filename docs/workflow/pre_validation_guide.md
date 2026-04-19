# Deterministic Pre-Validation Guide

Pre-validation is the current automated validation gate for workflow steps.

This is not a secondary optimization layer anymore. In the current runtime:

- if pre-validation passes, the step is auto-approved
- if pre-validation fails, the step fails and retries

There is no separate LLM validation phase in the main workflow execution path.

## What it does

Pre-validation runs deterministic checks against a step's `validation_schema`:

- file existence
- JSON parsing
- JSONPath existence checks
- value type checks
- string and array length checks
- numeric range checks
- regex pattern checks
- cross-field consistency checks

Runtime entry point:

- [`RunPreValidation`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/pre_validation.go)

Current workflow execution behavior:

1. Run the step.
2. Run pre-validation on the step output folder.
3. Convert that result into `ValidationResponse`.
4. Pass means `COMPLETED`; fail means `FAILED`.

On `FAILED` the retry loop runs up to 3 attempts per step. Attempt 2+ **continues the existing execution agent** — the validation errors are sent as a follow-up user message on the same agent, preserving its system prompt, tool state, and prior tool calls. The loop falls back to creating a fresh agent when there's nothing to continue from (attempt 1, an empty prior conversation, or a `learn_code` step whose own fix loop owns conversation shape).

See:

- [`controller_execution.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_execution.go)

## Schema shape

Pre-validation is driven by `validation_schema` on the step:

```json
{
  "validation_schema": {
    "files": [
      {
        "file_name": "results.json",
        "must_exist": true,
        "json_checks": [
          {
            "path": "$.users",
            "must_exist": true,
            "value_type": "array",
            "min_length": 1
          },
          {
            "path": "$.metadata.total_count",
            "must_exist": true,
            "value_type": "number",
            "consistency_check": {
              "type": "array_length",
              "compare_with_path": "$.users"
            }
          }
        ]
      }
    ]
  }
}
```

Core types are defined in:

- [`planning_agent.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_agent.go)

## Supported checks

### File-level

| Field | Meaning |
|---|---|
| `file_name` | File to validate |
| `must_exist` | File must exist |
| `json_checks` | Optional checks to run after parsing JSON |

### JSON checks

| Field | Meaning |
|---|---|
| `path` | JSONPath expression |
| `must_exist` | Path must resolve |
| `value_type` | `string`, `number`, `boolean`, `array`, `object` |
| `min_length` / `max_length` | Length constraints for strings and arrays |
| `min_value` / `max_value` | Numeric bounds |
| `pattern` | Go regex pattern for string validation |
| `consistency_check` | Cross-field validation rule |

### Consistency rules

Supported rule types in current code:

- `equals`
- `array_length`
- `greater_than`
- `less_than`
- `in_array`

These are implemented in:

- [`pre_validation.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/pre_validation.go)

## Path behavior

This is an important current detail that older docs often miss.

`file_name` is resolved relative to different scopes depending on the path:

- bare filenames like `results.json` are treated as step-local outputs
- workflow-relative paths such as `knowledgebase/...` are resolved from the workflow root

The validator also tracks an alternate path and may include path hints in errors when the file exists somewhere else than expected.

So:

- use bare output filenames for normal step artifacts
- only use workflow-root paths intentionally

## Skip behavior

If `validation_schema` is:

- `nil`, or
- present but `files` is empty

then pre-validation is treated as skipped and returns a passing result with zero checks.

That means a missing schema is effectively no automated gate.

Current workshop guidance strongly prefers giving every meaningful output step a real schema.

## Resource limits

Current code enforces hard limits:

- max files per schema: `20`
- max checks per file: `100`
- max JSON file size: `10 MB`

If the schema exceeds those limits, validation setup fails before checks run.

## Regex behavior

Regex validation has a few implementation details worth knowing:

- patterns use Go regex syntax
- invalid regex patterns do **not** fail the whole step as normal validation failures
- they are treated as **schema warnings**
- validation continues

The code also tries to repair double-escaped patterns during unmarshaling and matching.

This means:

- invalid regexes are still a bug in the schema
- but they show up as warnings instead of directly failing output validation

## Output and logging

Pre-validation produces a `WorkspaceVerificationResult` with:

- `OverallPass`
- `FilesChecked`
- summary counts
- `Errors`
- `SchemaWarnings`

It also writes logs to:

- `logs/{step-id}/pre_validation.json`

and emits:

- `pre_validation_completed`

So the result is available to:

- runtime retry logic
- workflow debugging
- hardening tools
- frontend event displays

## Best practices

### 1. Treat it as the real gate

Do not write vague schemas and assume another validation phase will catch mistakes later.

Current runtime assumes:

- good schema = meaningful automated pass/fail

### 2. Make schemas anti-stale

Do not validate only:

- a `success: true` flag
- file existence alone

Also require:

- expected fields
- non-empty arrays
- identifying values
- structural evidence that fresh work was done

Example:

- not just `$.login_success`
- also `$.dashboard_url`
- `$.account_name`
- `$.pan`

### 3. Use consistency checks aggressively

This is the most useful anti-gaming tool.

Examples:

- reported count equals actual array length
- selected ID appears in returned array
- summary totals match raw totals

### 4. Keep semantic validation separate

If you need to answer questions like:

- “is this summary accurate?”
- “is this analysis good?”
- “did the report reflect the right business conclusion?”

do that in a separate workflow step.

`validation_schema` should stay machine-checkable.

### 5. Prefer output-focused schemas

Validate the final artifact the step is responsible for, not every incidental intermediate file.

### 6. Be careful with regexes

Use simple, valid Go regex patterns.

Good examples:

- `^success$`
- `^\\d+$`
- `^[A-Za-z0-9_]+$`

Avoid incomplete or overly clever patterns that are likely to compile incorrectly.

## Common pitfalls

### Missing schema

No `validation_schema` means no real automated gate.

### Wrong `array_length` direction

Preferred pattern:

- `path` points to the count field
- `compare_with_path` points to the array

Example:

```json
{
  "path": "$.count",
  "consistency_check": {
    "type": "array_length",
    "compare_with_path": "$.items"
  }
}
```

### Validating non-JSON files with JSON checks

If a file has `json_checks`, it must parse as JSON.

### Using semantic `success_criteria` as the gate

That is no longer the right model for workflow automation.

Put:

- execution guidance in `description`
- machine-checkable requirements in `validation_schema`

## Where this fits with other docs

- [learning_architecture.md](learning_architecture.md): current global-skill learning model
- [step_config_format_specification.md](step_config_format_specification.md): step config and validation schema placement
- [learn_code_flow.md](learn_code_flow.md): how scripted steps use pre-validation during fast path and repair loops
