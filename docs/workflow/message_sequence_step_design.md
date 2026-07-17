# Message Sequence Steps

## Purpose

A `message_sequence` is a persistent, ordered conversation with one coding agent. Use one large sequence per coherent shared-context span, when later turns must retain the reasoning and tool context from earlier turns.

The preferred shape is: complete the whole span, re-open authoritative evidence and prove every criterion, repair verified gaps, then double-check before the top-level validation gate. Require run-specific proof/provenance in the output. Use multiple large sequences only when contexts should not be shared because of security/credentials, independently rerunnable outputs or failure domains, clean-room independence, human/routing boundaries, or context contamination.

Supported item types:

- `user_message`: send one focused follow-up instruction.
- `foreach`: render and send one follow-up instruction for each row returned by a read-only SQLite query.
- `prevalidation`: run a backend validation gate and send concrete repair feedback to the same conversation when it fails.

Deterministic code is not a sequence item. Put code in a standalone `regular` step configured with `declared_execution_mode: "scripted"`. Connect it to adjacent conversational steps through explicit `context_dependencies`, `context_output`, and validation schemas.

## Shape

```json
{
  "id": "review-and-correct",
  "type": "message_sequence",
  "title": "Review and correct",
  "description": "Review the latest generated report against the workflow goal.",
  "context_dependencies": [
    "runs/iteration-0/default/execution/generate/report.json"
  ],
  "context_output": ["report.json", "report_proof.json"],
  "items": [
    {
      "id": "prove",
      "type": "user_message",
      "message": "Re-open authoritative evidence, prove every criterion, identify unsupported conclusions, and write run-specific source references and criterion checks to report_proof.json."
    },
    {
      "id": "repair-and-double-check",
      "type": "user_message",
      "message": "Repair every verified gap, update the proof record, and double-check the complete report against the sources.",
      "write_access": { "db": true }
    },
    {
      "id": "validate-proof",
      "type": "prevalidation",
      "validation_schema": {
        "files": [
          {"file_name": "report.json", "required": true, "validation_type": "json"},
          {"file_name": "report_proof.json", "required": true, "validation_type": "json"}
        ]
      }
    }
  ]
}
```

The step `description` is turn 0. Items are turns 1 through N.

## Foreach

Use `foreach` when every selected database row must receive the same conversational treatment.

```json
{
  "id": "review-each-finding",
  "type": "foreach",
  "source_sql": "SELECT id, summary FROM findings WHERE status = 'open' ORDER BY id",
  "message": "Review finding {{.id}}: {{.summary}}",
  "max_iterations": 50
}
```

`source_sql` is read-only and runs against `db/db.sqlite`. Each row is bound to `.` in the Go template.

## Write Access

Reads from workflow execution outputs, `db/`, `knowledgebase/`, and learnings are available. Writes are item-scoped and off by default.

```json
{
  "write_access": {
    "db": true,
    "knowledgebase": false,
    "learnings": false
  }
}
```

Write access is folder-level. Per-file path lists are rejected because they create a misleading security boundary.

## Deterministic Fetching Before Agentic Processing

Use explicit plan steps:

```text
regular scripted: fetch-and-normalize-authoritative-data
  -> message_sequence: analyze-verify-and-repair-from-fetched-data
```

The scripted step owns `learnings/fetch-and-normalize-authoritative-data/main.py`, batches related deterministic API/SDK calls or CLI commands under one source/auth/retry/output contract, and writes validated DB rows or an explicit output artifact with provenance and freshness. This makes failures, retries, permissions, logs, and costs visible at the workflow-step level. The message sequence consumes those results for judgment, synthesis, semantic verification, and repair; it does not re-fetch or re-parse stable response shapes conversationally.

When call selection requires judgment, use an agentic request-specification step before the scripted executor and a later message sequence to interpret the result. Do not create one scripted step per endpoint or tiny transform.

## Workflow Contract v1.0.10

Contract v1.0.10 removes legacy `type: "code"` items.

Before a scheduled workflow sends its first normal message, the scheduler runs every missing workflow-version upgrade in order and verifies that each upgrade stamped its expected version. The v1.0.10 upgrade calls `migrate_message_sequence_code_items`.

Contract v1.0.11 then upgrades `builder/improve.html` to the schema-2 Pulse history contract. It preserves the time-ordered history while adding canonical Signals / Reflection / Improvements attribution used by the Pulse popup. The v1.0.10 migration keeps its own trusted finalizer, so workflows on v1.0.9 can still traverse both upgrades safely.

The migration automatically converts only unambiguous top-level sequences containing code items and their immediately following prevalidation gates. It:

1. Copies each script to `learnings/<step-id>/main.py`.
2. Creates a standalone scripted regular step for each code item.
3. Preserves cumulative dependencies, outputs, validation, and step configuration.
4. Replaces the legacy sequence only after the migrated plan validates.

Mixed conversational/code sequences, nested sequence code, `input_json`, missing outputs, and unusual write ownership are not guessed. The preflight blocks the scheduled run with `MESSAGE_SEQUENCE_CODE_MIGRATION_BLOCKED` and an actionable split requirement.

The runtime also rejects any remaining code item with a precise v1.0.10 upgrade message. This prevents an old workflow from starting and failing halfway through execution.

## Authoring Rules

- Keep each user message focused on one outcome.
- Use the same conversation only when shared context is valuable.
- Use explicit prevalidation for deterministic acceptance checks that should trigger same-conversation repair, not for subjective review.
- Use `foreach` only with bounded, read-only queries.
- Put all deterministic code in standalone scripted regular steps.
- Put fixed API/SDK/CLI fetching, pagination, stable parsing/normalization, and mechanical persistence in coherent scripted fetchers; feed their durable outputs to large message sequences.
- Pass data between steps through declared files or the workflow database, never hidden in terminal context.
