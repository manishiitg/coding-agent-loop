# Browser QA evidence capture

Capture video, console logs, and network logs as attempt-scoped evidence. Screenshots and traces can use the same contract. The customer's policy decides which evidence is always retained, retained only on failure, sampled, or disabled.

When no customer policy exists, propose capturing all three for every attempt, retaining the initial setup smoke video and all failure videos, retaining redacted console errors/warnings and network failures/navigation summaries for every attempt, disabling network response bodies, and retaining traces on failure. Apply explicit storage limits and retention periods before production use.

## Capture ownership and lifecycle

Every artifact belongs to one application, environment, run, case, attempt, and phase such as `setup`, `original`, `diagnostic`, `candidate`, or `canonical`. Start collection before the relevant navigation or action, stop it in cleanup even after failure or cancellation, then finalize and inspect files before recording them as available.

Use one capture owner per browser context:

- A saved Playwright run owns its video, console listeners, network/HAR collection, screenshots, and trace lifecycle.
- A managed-browser investigation uses the current `agent_browser` diagnostic and capture commands. Use only commands supported by the deployed mode; do not assume CDP or bundled capture support.
- Do not start overlapping recorder/HAR systems against the same context. Temporary Browser-panel replay is useful for observation but is not durable test evidence.

## Durable files

Copy retained files only after they are finalized into a stable path such as:

```text
db/assets/browser-qa/<run-id>/<case-id>/<attempt-id>/
├── video.webm
├── console.jsonl
├── network.har
├── trace.zip
└── screenshots/
```

Adapt filenames and formats to the runner while keeping their artifact types explicit. `console.jsonl` should preserve timestamp, level/type, message, page URL/source, and stack when available. Network evidence may be HAR or structured JSONL and should preserve timestamp, method, sanitized URL, resource type, status/failure, duration, and selected safe headers. Response bodies are disabled by default.

Store one durable artifact row for each expected and observed artifact. Suggested fields are:

- artifact ID; run, case, attempt, and phase IDs;
- `video`, `console_log`, `network_log`, `trace`, or `screenshot` type;
- source such as Playwright runner or managed browser;
- durable relative path or null, MIME/format, byte size, and checksum when available;
- capture start/end, status (`complete`, `partial`, `missing`, `failed`, or `not_requested`), validation result, and missing/failure reason;
- redaction status/version and retention/expires-at policy;
- source, profile, suite, journey, and application build revisions needed for provenance.

Declare the table, primary key, indexes, writer, and idempotent upsert in `db/README.md`. Files go in `db/assets/`; metadata goes in `db/db.sqlite`. Do not use volatile `runs/...` files as report sources.

## Redaction and scope

Treat console and network evidence as sensitive. Before persistence or display:

- remove cookies, authorization headers, tokens, passwords, secret values, signed URL parameters, and storage-state data;
- avoid request and response bodies unless an approved diagnostic need requires a bounded allowlist;
- minimize personal and customer content in URLs, console messages, DOM snapshots, and screenshots;
- keep unredacted temporary capture inside its authorized execution boundary and delete it after producing the permitted durable artifact;
- record redaction failure as missing/failed evidence rather than publishing unsafe output.

The report must never embed unrestricted HAR bodies, authenticated page dumps, secret-bearing console output, base64 media, or expiring signed URLs.

## Validation and status

Validate more than path existence:

- video opens, has nonzero duration, and matches the intended attempt;
- console/network files parse in the declared format and contain timestamps within the attempt window;
- network evidence includes the relevant navigation/request or records why it was unavailable;
- checksums and byte sizes match recorded metadata when present;
- all retained content passed the configured redaction check.

An application assertion can still pass when optional diagnostics are unavailable. Record the evidence gap separately. When policy marks an artifact required for readiness, investigation, or healing verification, missing or unsafe evidence makes that result incomplete rather than passed/healed.

## Dashboard behavior

For the selected run/case/attempt, show video playback and separate Console, Network, Trace, and Screenshots panels. Load video lazily with `window.report.mediaUrl(path)` and use supported file access for logs/downloads. Show source, phase, capture status, time range, size, validation/redaction status, and the actual missing reason.

Console views should filter level and search text without executing content. Network views should filter method/status/type and summarize failures without rendering captured HTML. Escape all log text and URLs. Keep the selected attempt stable across refresh and never substitute evidence from a different attempt.
