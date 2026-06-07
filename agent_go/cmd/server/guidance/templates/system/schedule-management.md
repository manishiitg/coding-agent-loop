## Schedule Management

You can create scheduled tasks that run automatically on a cron schedule. Schedules are stored in `_users/<user-id>/multiagent-schedules.json` (default user-id is "default" unless the multi-tenant runtime has set one).

> Scope: this doc covers **multi-agent chat** schedules only, which are **cron-only** (repeating cadence). To schedule a **workflow** — including dated, one-time **calendar** schedules (a fixed list of specific date/time runs, e.g. a content calendar) — use the workflow-schedule tools (`create_schedule` / `create_calendar_schedule`) documented in `get_reference_doc(kind="workflow-tools")`; those live in the workflow's `workflow.json`, not here.

### File Format

```json
{
  "schedules": [
    {
      "id": "unique-uuid",
      "name": "Human-readable name",
      "description": "What this schedule does",
      "cron_expression": "0 9 * * *",
      "timezone": "America/New_York",
      "enabled": true,
      "mode": "multi-agent",
      "query": "The message/instruction to execute on schedule"
    }
  ],
  "capabilities": {
    "selected_servers": [],
    "selected_tools": [],
    "selected_skills": [],
    "selected_secrets": [],
    "browser_mode": "none",
    "use_code_execution_mode": false
  }
}
```

### How to manage schedules

Use `execute_shell_command` to read and write the schedule file:

**List schedules:**
```bash
cat _users/<user-id>/multiagent-schedules.json 2>/dev/null || echo '{"schedules":[],"capabilities":{}}'
```

**Create/update schedules:** Read the file, modify the JSON (add/update/remove entries), write it back. Use `python3` or `jq` for JSON manipulation. Always generate a UUID for new schedule IDs (`python3 -c "import uuid; print(uuid.uuid4())"`). **Preserve entries you are not changing** — the file may contain built-in/system entries (e.g. a memory-enrich schedule) and disabled ones; mutate the existing array in place rather than rebuilding it from scratch, or you will silently drop them.

**Cron expression examples:**
- `0 9 * * *` — daily at 9:00 AM
- `0 9 * * 1-5` — weekdays at 9:00 AM
- `*/30 * * * *` — every 30 minutes
- `0 0 1 * *` — first day of each month at midnight

**Important:** The scheduler picks up file changes automatically. After writing the file, the schedule will be active within 60 seconds.

### When users ask to schedule something

1. Confirm the schedule details (what to run, when, timezone)
2. Read the current schedule file
3. Add the new schedule entry with a generated UUID, `mode: "multi-agent"`, and the user's instruction as `query`
4. Write the updated file back
5. Confirm to the user what was scheduled

### Updating or removing schedules

- To **update**: read the file, find the entry by `id` or `name`, modify in place, write back.
- To **remove**: filter out the entry by `id`, write back.
- To **disable temporarily**: set `enabled: false` on the entry — preserves the rest of the schedule definition.

### Multiple-user note

If the runtime is multi-tenant, the user-id in the file path will be the actual user ID, not "default". Read the value from the shell environment if available, otherwise read from the context provided in the conversation.
