# Workflow API triggers

A workflow route can run from either a **time trigger** (cron/calendar) or an **API trigger** (an inbound webhook). Several triggers can target the same route. API triggers use the normal workflow execution, ownership, cancellation, run history, and finalization lifecycle.

## Setup

Open **Setup → API triggers**, choose a name, one or more saved routing/branch selections and variable groups, and save. Each trigger gets its own endpoint and secret. The selected route runs through the full plan, including its prerequisites; it does not jump directly to an arbitrary step.

Use generic bearer authentication for services capable of sending an Authorization header. Use GitHub authentication for GitHub's signed webhooks. Copy the generated secret immediately; it is returned only at creation/rotation and stored encrypted with the existing server secrets key. Editing authentication mode rotates the secret. Disable revokes new deliveries; remove deletes the attachment. Neither interrupts a run already accepted; use Stop in run history for that.

The displayed URL uses the currently connected server. Localhost can receive requests from the same computer. Public services require a reachable HTTPS deployment or a separately configured tunnel; this feature does not publish the local server automatically.

## Generic caller

```sh
curl -X POST 'https://YOUR_SERVER/api/hooks/workflow/TRIGGER_ID' \
  -H "Authorization: Bearer $TRIGGER_SECRET" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: unique-delivery-123' \
  -H 'X-Webhook-Event: issue.created' \
  -d '{"issue":{"number":123,"title":"Example"}}'
```

The body may be any valid JSON up to 1 MiB. Authentication and arbitrary request headers are not copied into the run. Fields such as `model`, `route_selections`, `selected_folder`, or `execution_options` inside the JSON remain payload data and cannot override the saved execution configuration.

For GitHub, configure Payload URL, `application/json`, and Secret in its webhook settings, then select the events to deliver. AgentWorks verifies HMAC-SHA256 over the raw body using `X-Hub-Signature-256`, records `X-GitHub-Event`/`X-GitHub-Delivery`, and acknowledges setup `ping` without starting a run. See [GitHub's signature specification](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries).

Other services can call the generic JSON endpoint directly if they support its authentication. Provider-specific challenge/signature schemes (for example Stripe or Slack) require an adapter; they are not interpreted as GitHub signatures.

## Run input

The server writes `webhooks/deliveries/<run_id>.json` inside the workflow before dispatch. An envelope contains:

```json
{
  "run_id": "generated-run-id",
  "delivery_id": "unique-delivery-123",
  "event": "issue.created",
  "received_at": "2026-09-12T10:00:00Z",
  "payload": {"issue": {"number": 123, "title": "Example"}}
}
```

Steps receive the file reference as run context. Script and agent shells also get its absolute path in `WORKFLOW_TRIGGER_INPUT_FILE`:

```python
import json
import os

with open(os.environ["WORKFLOW_TRIGGER_INPUT_FILE"], encoding="utf-8") as source:
    delivery = json.load(source)
issue_number = delivery["payload"]["issue"]["number"]
```

The execution folder guard grants this delivery file for reading. A payload reference never fills a required human_input response. Delivery files are retained in the workflow independently of rotated execution folders; there is no automatic payload expiry in this version.

## Responses and retries

- **202:** accepted, with `run_id` and `delivery_id`. Execution continues asynchronously; inspect the existing run history for results.
- **200, duplicate=true:** the delivery ID already has a durable run record. Returns its run ID and current status, including failed/interrupted status. No new run is launched.
- **200, status=pong:** authenticated GitHub setup ping.
- **401:** missing/invalid credentials or signature.
- **404 / 410:** unknown / disabled trigger.
- **400 / 413 / 415:** malformed payload or metadata / payload too large / unsupported content type.
- **503, Retry-After:** workflow could not start (for example busy or saved route unavailable). Storage unavailability also returns 503.

Reuse an Idempotency-Key for retries of the same event; GitHub uses its delivery ID. Without an ID, each request is a separate delivery. Duplicate detection survives server restarts through the scheduler run store and its unique run identity. Use a new delivery ID to intentionally run an event again after a failed or interrupted run. This is not an automatic durable delivery queue: the sender must retry rejected requests, and interrupted accepted runs remain visible in run history rather than being replayed automatically. GitHub deliveries may need manual redelivery or an external retry adapter.

## Configuration and management API

API triggers are stored beside time schedules in `workflow.json.schedules`, with `schedule_type="webhook"`, `route_selections`, `group_names`, constrained run mode, and a `webhook` configuration containing auth mode and encrypted secret. The scheduler does not register clock ticks or missed occurrences for them. It shares the workflow collision lock with time-triggered runs.

Authenticated management endpoints are `GET/POST /api/workflow-webhooks` and `PUT/DELETE /api/workflow-webhooks/{id}`. Supply `workspace_path` in mutation JSON or GET/DELETE query parameters. Writes require workflow ownership/write access; readers may inspect bindings but receive no secrets. POST/PUT accept `name`, `enabled`, `auth_mode` (`bearer`/`github`), `route_selections`, `group_names`, and optional `rotate_secret`. Responses include the relative endpoint `path`; a plaintext `secret` appears only when newly issued. Only `/api/hooks/workflow/{id}` bypasses user JWT authentication, because it performs its own trigger-specific authentication.

Incoming payloads never use the legacy `trigger_payload` session override mechanism. API triggers reject that field, schedule-local messages, session resume, dependency queues, and clock settings. Workflow duplication excludes API triggers; configure a new attachment and credentials in the copy.

## Observing webhook runs

The builder conversation panel has a **Webhooks** filter beside Recent, Schedules,
and Bots. Webhook executions appear here; the Schedules filter shows time jobs.
The feed refreshes every 10 seconds while either run filter is visible. Each
webhook run links to its execution transcript and shows its status and run folder.
New run records also retain the trigger name, event type, delivery ID, and received
time, without copying the payload or authentication secret into history responses.

The activity monitor and current-workflow header distinguish Webhook, Scheduled,
and Manual origins. Older sessions with a `schedule-webhook--` identity are still
shown as Webhook even if their legacy session metadata says `cron`.

This is execution history, not a durable inbox: busy deliveries still receive 503
and have no pending entry. Pending-delivery visibility requires extending the
existing scheduler's pending-event storage.

The workflow Schedules panel also shows the POST endpoint and a copy button on
each webhook row, including disabled triggers. Credentials remain in API trigger setup.

## Triggers in Plan

Plan displays saved schedules and webhooks above Start, with timing, state,
route selections, groups, and webhook URLs. Settings buttons open the existing
Schedules or API triggers panels. The cards refresh from the scheduler every
15 seconds while Plan is open; they are projections, not editable plan steps.

Highlight path starts at workflow entry, retaining prerequisite steps and
following saved choices at routing/branch nodes. Unspecified decisions show
possible paths. Missing route bindings are shown as unavailable. Pulse-only and
optimizer jobs appear as maintenance triggers and do not link into plan execution.
