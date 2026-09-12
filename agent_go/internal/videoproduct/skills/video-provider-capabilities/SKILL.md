---
name: video-provider-capabilities
description: Resolve an AI-video model's live official API schema and turn it into a durable request, continuity, cost, and review plan before any paid video call. Use for Kling, Seedance, Veo, or any other video model when selecting a model, preparing or retrying a request, assigning image/video/audio references, using first or last frames, creating multi-shot video, extending footage, chaining clips, or deciding whether separate clips are necessary.
---

# Resolve provider capabilities before generation

Do not create a paid video request from a generic template or model-family
memory. Before the first video call for a production, read the selected
endpoint's current official API reference. For fal-hosted models use that
exact endpoint's fal API page; for direct Google models use the current
Google Gemini/Veo documentation. A model list discovers candidates; the
endpoint schema controls the request. Models and higher-level workflows are
different contracts. Do not infer one from the other.

Read these bundled references when their phase begins:

- `references/capability-record.md` before resolving or refreshing a model.
- `references/continuity-planning.md` before writing the shot list or request.
- `references/execution-review.md` before spending, retrying, assembling, or
  presenting outputs.
- `video-model-selection/references/cost-guidance.md` when comparing models or
  giving a user a costed choice.

## Required artifacts

Write the following under the production folder in direct chat, or inside the
current workflow-stage folder:

- `capabilities/<provider>-<model-slug>.json`: the live endpoint contract.
- `continuity-plan.json`: scene boundaries, reference lineage, seams, and the
  reason for every independent generation.
- `generation-ledger.json`: cost estimate, exact submitted request, provider
  adjustments, job state, output, validation, presentation, and review.

These are required inputs and evidence for every paid video request. The
capability record begins with:

```json
{
  "schema_version": 2,
  "provider": "fal-ai or google-ai",
  "model_id": "exact resolved model id",
  "execution_mode": "queue or realtime",
  "checked_at": "RFC3339 timestamp",
  "official_docs": ["official API URL"],
  "input_contract": {
    "required": [],
    "optional": [],
    "media_roles": {"start_frame": {"min": 0, "max": 0}, "end_frame": {"min": 0, "max": 0}, "image_references": {"min": 0, "max": 0}, "video_references": {"min": 0, "max": 0}, "audio_references": {"min": 0, "max": 0}},
    "duration_seconds": {"min": 0, "max": 0},
    "multi_shot": {"supported": false, "field": "", "shape": {}},
    "extension": {"supported": false, "input_role": ""},
    "seed": {"supported": false, "field": ""},
    "native_audio": {"supported": false, "field": "", "values": []},
    "aspect_ratios": [],
    "resolutions": [],
    "mutually_exclusive": [],
    "output_contract": {}
  },
  "request_shape": {"exact field names": "copied from the official schema"},
  "pricing": {"source": "", "checked_at": "", "currency": "", "unit": "", "unit_price": null, "base_estimate": null, "retry_reserve": null, "maximum_approved_cost": null, "estimate_available": false},
  "continuity_fit": {"strategy": "...", "why": "..."}
}
```

Record only capabilities confirmed by the current official schema. Omit an
unsupported feature rather than guessing a field name. Treat undocumented
automatic coercion as a changed request: record it and stop before accepting
the result when it alters duration, ratio, resolution, references, audio, or
continuity. Preserve the exact request and media-role manifest in both the
generation ledger and `production.json` next to the output path.

For a realtime model, the capability record must additionally name the session
transport, browser receive/send tracks, authenticated proxy boundary, live
prompt/update protocol, current session-minimum billing rule, and how a durable
recording is exported. Realtime media is not a queue result: do not apply
request-ID polling, pretend it has a downloadable output URL, or commit a
live stream to assembly until the documented export has been retained and
reviewed.

## Choose a continuity plan before calling the model

Treat one continuous scene as a continuity problem, not a list of storyboard
bullets. Minimize independent generations and stitch points.

1. Prefer one generation when its supported duration covers the continuous
   action. Narrative beats are not automatically clip boundaries.
2. If the endpoint supports multi-shot prompts, use one request with deliberate
   internal shots when that preserves the scene. Record the exact prompt-array
   or timestamp contract; do not flatten it into prose.
3. If multiple requests are unavoidable, choose conditioning for the actual
   handoff. Use predecessor media when its state is needed; use identity-only
   references and text for a new shot whose composition or motion must change.
4. Preserve required character, wardrobe, object, location, style and audio
   invariants. Reuse approved identity deliberately, but do not copy every
   reference into every request. Label each supplied reference's semantic role.
5. Independent requests can support planned editorial cuts with recurring
   identity. Inspect the intended continuity; do not claim an uninterrupted
   take. Follow cinematic-visual-development before expanding references.

For every seam, record the preceding output, extracted boundary frame, next
input/reference, overlapping action in both prompts, expected transition, and
why a new generation is necessary. A 30-second continuous interaction is not
five independent clips merely because its script has five beats.

For image-to-video, describe motion and change rather than redundantly
redescribing the anchored frame. For several references, write a manifest
(`reference 1 = approved character`, `reference 2 = location`, and so on) and
preserve that order. A style donor supplies visual attributes, not permission
to copy its person, logo, or exact composition.

## Spend, review, and presentation gate

Before submission, validate required fields, closed enums, media counts and
roles, mutually exclusive inputs, output type, duration, ratio, resolution,
audio behavior, safety constraints, and estimated cost where the provider
offers it. Reuse uploaded handles instead of uploading the same asset for
every call. Obtain the user's approval when the planned paid-call count or
cost exceeds the approved production budget. The approval must name both the
base estimate and the maximum including the agreed quality-retry allowance;
never present a per-second rate without the actual project total.

After completion, download the output and verify it locally with `ffprobe` and
visual evidence. Show every newly generated clip with `show_video` as a
Preview immediately after it is verified playable. Do not assemble a clip
marked `pending_user_review`, and do not present a stitched cut as the user's
first view of its component clips. Record the presentation ID and review
outcome in the ledger and `production.json` before the clip can become an
assembly input.

## Request construction

Build requests from the capability record's exact `request_shape`, not a
lowest-common-denominator helper. Check local inputs, upload or encode them
using the selected provider skill's documented mechanism, and preserve the
provider's exact field names and nesting. If the request cannot meet the
continuity plan within documented limits, stop and explain the constraint;
do not silently downgrade the model, remove references, or fall back to
unrelated independent clips.

Poll or rejoin asynchronous jobs by their durable job ID. Retry only the failed
unit, with a bounded retry budget and a recorded reason. Do not resubmit a job
merely because the local wait timed out. A successful provider response is not
evidence that continuity, identity, prompt adherence, or review passed.
