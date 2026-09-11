---
name: minimax-h3-video
description: "Plan Video Studio's MiniMax H3 Max generation through fal.ai, including H3 Max Turbo for an initial image-controlled anchor, minimal-reference complete scenes and optional predecessor continuity, and Director for explicitly approved real-time direction. Read with video-provider-capabilities and fal-ai before any paid H3 call."
---

# Use MiniMax H3 Max deliberately

Read `video-provider-capabilities`, `fal-ai`, `video-cinematography`, and the
local prompting reference at `references/fal-h3-max-prompting.md`, then the
live machine-readable guide for the selected route before every paid call:

- `https://fal.ai/models/minimax/h3-max/text-to-video/llms.txt`;
- `https://fal.ai/models/minimax/h3-max/image-to-video/llms.txt`;
- `https://fal.ai/models/minimax/h3-max/reference-to-video/llms.txt`.
- `https://fal.ai/models/minimax/h3-max-turbo/image-to-video/llms.txt` for an initial Turbo anchor.
- `https://fal.ai/models/minimax/h3-max/director/llms.txt` for an explicitly requested real-time Director session.

## Use the guarded H3 runner

For every paid H3 Max request, use `scripts/h3-max-runner.mjs`; do not write a
new inline Fal client call or use `fal.subscribe()`. The runner permits only
the approved H3 Max endpoints plus the narrowly allowed Turbo anchor endpoint,
validates route-specific inputs before a
paid submit, saves the request ID and resolved non-secret input immediately,
and emits JSON-lines console progress plus a sibling `*.log.jsonl` file.

**Director is deliberately excluded from this runner.** It is a WebRTC live
session, not a queued request that produces a completed MP4. Do not pass its
model ID to the runner, substitute it into a normal shot job, or claim it is
available until Video Studio has a dedicated authenticated realtime-browser
implementation.

Write a non-secret job JSON with `endpoint`, `prompt`, and the selected route's
controls. Then validate, submit once, and wait/rejoin the *same* state file:

```bash
BASE="work/<production>/h3/<shot-id>"
node .claude/skills/minimax-h3-video/scripts/h3-max-runner.mjs validate --input "$BASE/job.json"
node .claude/skills/minimax-h3-video/scripts/h3-max-runner.mjs submit --input "$BASE/job.json" --state "$BASE/request.json"
node .claude/skills/minimax-h3-video/scripts/h3-max-runner.mjs wait --state "$BASE/request.json" --output "$BASE/candidate.mp4" --timeout-seconds 900
```

`SECRET_FAL_KEY`, `SECRET_FAL_AI_KEY`, `FAL_KEY`, or `FAL_AI_KEY` must be
present in the environment. Never put a secret in the job or state JSON. If a local wait ends, run `status` or `wait`
again with the existing state file; never submit a replacement. Read the
runner's `request.json` and `request.json.log.jsonl` for the exact endpoint,
resolved input, request ID, queue states, provider logs, result timings, and
download path. When `request.json` has `result.video_url`, pass that value as
`source_url` to `show_video` along with the downloaded project-relative `path`:
the Production panel prefers Fal's CDN for fast preview playback and falls back
to the retained local file. After its `completed` event, run the normal file receipt
(`ffprobe`, stable-frame inspection, then `show_video`) before accepting it.

## Choose references before preparing assets

Follow `cinematic-visual-development`: start with the minimum sufficient
references and one representative complete-scene test. For face identity,
prefer an approved face-only image plus text for wardrobe, scene and movement.
A posed/full-body image may also constrain composition; a predecessor tail may
carry motion or framing that conflicts with a new shot. Use them only for a
required control. A shot can use `reference_image_urls` without any
`reference_video_urls`; a recurring identity does not require a predecessor.
Inspect conditioning conflicts before retries or proposing compositing.

## Prepare a focused continuity tail when needed

When a direct continuation requires the predecessor state, do not send the full predecessor as Video 1. After
the predecessor is accepted, create a deterministic **2- or 3-second tail**
from its end, then pass the uploaded tail URL as Video 1 with only the still
references that have a distinct job in the successor. Default to 3 seconds;
use 2 only when it fully contains the outgoing action.

```bash
node .claude/skills/minimax-h3-video/scripts/h3-max-runner.mjs tail-reference \
  --input "$BASE/previous-candidate.mp4" --output "$BASE/previous-tail-3s.mp4" --seconds 3
```

The runner verifies source/output duration and writes only the final tail.
Upload that output through the normal workspace upload flow before adding its
returned URL to `reference_video_urls`. This prepares a model input, not a
visual seam repair: do not trim accepted delivery clips, blend picture/audio,
or use the tail as a substitute for required character-identity references.
It can, however, carry the visible location, lighting, composition, and set
dressing from the predecessor. Do not automatically repeat a location still
when that same setting is already clearly visible in the tail.

Use H3 Max only. Do not substitute standard H3, another H3 Max route, or
another provider.

Every H3 Max route generates 5–15-second clips at `480P` or `768P`. Video
Studio defaults to `resolution: "480P"` and
`prompt_expansion_mode: "balanced"`. The runner uses **15 seconds only as its
fallback when a planned duration was accidentally omitted**; choose and send an
explicit duration for every planned shot. Fal recommends balanced for H3 Max;
quality can spend up to 30 seconds rewriting a prompt, so use it only when the
user explicitly requests that slower treatment. Use 768P only when the user
explicitly requests and approves the higher cost; do not offer or use 2K or
4K. Verify live rates before estimating a run.

Treat an explicit user per-clip duration as a hard creative constraint, not a
suggestion to optimise away. For example, when the user asks for 5-second
clips, request 5 seconds for every applicable H3 generation; do not replace
them with longer 10–15-second takes to reduce cost or seam count. Recommend a
duration tradeoff only when the user has not fixed one.

Choose duration from the actual scene: the measured spoken words at natural
pace, the needed performance/action, and any entrance/settling beat. Use the
shortest complete take in the 5–15 second range—do not pad a simple 5-second
reaction to 15 seconds. Prefer 15 seconds when the dialogue or action genuinely
needs that room, because one complete take removes paid calls and visible
boundaries. Use 5–10 seconds for a genuinely short approved beat, deliberate
editorial cut, or an explicit user duration.

## Plan continuity honestly before spending

Reference-to-Video conditions a **new** bounded H3 Max generation. Passing a
2- or 3-second tail of an accepted predecessor as Video 1 can carry identity, location, camera language,
performance, and audio direction, but it neither appends the old MP4 nor
guarantees its final frame, mouth shape/phoneme, timing, or audio waveform at
the new clip's first frame. Never describe it to the user as literal video
extension or promise a seamless speech continuation.

Choose the smallest topology that respects the actual dramatic action:

1. Keep one person's uninterrupted visible dialogue, an unbroken sentence, or
   one continuous physical action in a **single 5–15-second generation** when
   it fits the route limit. Do not split it into 5-second requests because the
   model is fast, because a prior clip exists, or to reduce a quote to pieces.
2. When the required uninterrupted performance exceeds one H3 Max take, stop
   before paying and propose an explicit editorial boundary: a completed
   thought and pause, reaction, insert/cutaway, or motivated camera-angle
   change. Record the exact boundary in the shot contract and obtain approval
   if it changes user-approved dialogue or the requested continuous-take
   treatment.
3. Choose reference inputs for the planned shot. Use an approved identity image
   and text for a new composition; add a 2- or 3-second predecessor tail only
   when its state is needed, with the role `continuity context`. State the
   outgoing/incoming action; never promise a mid-word mouth match.

If a boundary is rejected, redesign the editorial beat or regenerate the
successor with a clearer handoff. Never silently shorten/rewrite approved
dialogue merely to make a seam less visible. When reporting model behavior,
separate a result observed in this production from a guarantee or limitation
verified in official H3 Max route documentation.

## H3 Max Director: an explicit real-time mode

`minimax/h3-max/director` is for a user who wants to direct one continuous
live stream while it plays: send an initial `configure` message, then later
`prompt` messages to evolve the action. It is **not** a replacement for the
normal queued shot-review workflow, Reference-to-Video inputs, or the final
MP4 assembly pipeline.

Before proposing or starting Director, read its current `llms.txt` and obtain
explicit approval for the session minimum, current per-second price, selected
resolution, and the fact that a dedicated realtime session will be opened.
The published contract uses `fal.realtime.open` with WebRTC and an
application-authenticated Fal proxy; never use `fal.run`, `fal.subscribe`, or
the Queue API for this endpoint, and never expose `FAL_KEY` in the browser.

Configure a single original scene with a prompt, optional exact-first-frame
`image_url`, resolution (`480p` or `768p`), aspect ratio (`16:9`, `9:16`, or
`1:1`), seed when appropriate, and a deliberate `memory` window for prior
prompts. Director's schema does not make it a substitute for the normal
Reference-to-Video character, audio, or predecessor-tail request. Do not
invent those fields; if the live schema does not expose a needed control, keep
the production on the queued H3 route.

Treat the stream as live playback, not proof of a durable deliverable. Track
`prompt_pending` / `prompt_applied` with their `prompt_version`, preserve
chunk metrics for diagnosis, and surface `deadline_missed` honestly: its
documented behavior is a video freeze and silent audio until the next chunk is
ready. Do not describe the mode as guaranteed gapless. Confirm how a durable
recording/export is retained before treating any Director result as an
assembly input or a final delivery.

## Prompt from an H3 Max shot contract

Before writing an H3 prompt, write `shot-contracts/<shot-id>.md` and present it
with `show_document` for review. It is a required pre-generation contract, not
a post-hoc explanation. Include route, exact duration, aspect ratio, each
reference asset and its job, subject identity and invariants, and the sections
below. Then apply the eight rules in `references/fal-h3-max-prompting.md`. Do
not turn a user's precise camera or dialogue direction into a looser generic
cinematic prompt.

### Required shot-contract detail

- **Purpose and success:** the story beat, observable acceptance criteria and
  priorities if essential requirements compete.
- **Must preserve:** approved identity and exact details required by the brief,
  exact dialogue/language when applicable, delivery format and intended continuity.
- **May change:** desired wardrobe, setting, pose, framing, motion or angle
  changes. Describe the whole scene H3 should produce.
- **Model discretion:** incidental set dressing, microgestures and other
  unspecified details. Do not invent rigid locks for every object or body part.
- **Request and review:** route, duration, reference paths and each one's job,
  audio direction, cost/retry allowance and checks for accepting the output.

Use timed blocks when meaningful action or dialogue changes need timing;
otherwise a concise single-beat direction is sufficient. Specify a precise
camera or emotional performance when it matters to the brief. More detail and
more references are not automatically stronger control.

The visible contract is the approval boundary: do not send a paid request until
the user approves it, unless the user has explicitly asked to skip that review
for the current shot.

For a single static dialogue beat, use one timed block rather than a long
unstructured paragraph. State the speaker, exact spoken line, performance,
room tone, and prohibited sound separately. Do not put a second, competing
gesture or action into a tightly constrained performance unless the user asked
for it. If the required dialogue cannot be spoken naturally in the requested
duration, flag that before spending rather than silently rushing, truncating,
splitting it mid-sentence, or inventing new words.

## Choose the route by control need

1. **Prompt-only establishing shot:** use
   `minimax/h3-max/text-to-video` only when no approved visual, motion, audio,
   identity, or continuity reference needs to control the result. Set the
   delivery `aspect_ratio` explicitly (21:9, 16:9, 4:3, 1:1, 3:4, or 9:16).
2. **Initial image-controlled anchor:** prefer
   `minimax/h3-max-turbo/image-to-video` when an approved `image_url` defines
   the first shot's first frame. It is an economical launch/anchor option, not
   a continuity route: do not supply reference images/video/audio, a predecessor
   tail, or `aspect_ratio`; output follows `image_url`. Read its live rate as
   promotional prices can change. Use `minimax/h3-max/image-to-video` when the
   full H3 Max route's control is specifically required. Add `end_image_url`
   only when a specific ending frame must be reached. Never use either route to
   fabricate a bridge between generated clips.
3. **Identity, motion, voice, or continuation:** use
   `minimax/h3-max/reference-to-video` whenever a shot needs subject/style
   locking, a predecessor's motion or performance, reference audio, or any
   multi-asset conditioning. If the shot needs predecessor motion/composition, send the runner-extracted
   2- or 3-second tail in `reference_video_urls` as Video 1 and describe the
   handoff. Otherwise omit the tail and direct the new shot in text. Reference each asset in prompt order:
   Image 1, Video 1, Audio 1 — never arbitrary JSON labels.

Reference-to-Video accepts at most 9 images, 3 videos, and 3 audio clips, with
12 files total. Reference videos and audio are each 2–15 seconds with a
15-second combined limit; audio cannot be the only reference, so accompany it
with an image or video. Do not use Image-to-Video's prompt-only fallback when
Text-to-Video states the intent more accurately, and do not use an edit route
as a fallback for a broken source shot.
4. **Live, continuously directed scene:** consider
   `minimax/h3-max/director` only when the user explicitly prefers real-time
   direction over queued per-shot review, accepts its session-minimum cost,
   and Video Studio's authenticated WebRTC Director integration is available.
   It receives live prompts during a single stream; it is not a queued H3
   continuation or a direct MP4 stitching operation.

## Assign every reference one job

Build a reference ledger before the request. For each image, video, or audio
input, record its order, accepted media constraints, and purpose: subject
identity, start composition, end state, motion, camera language, environment,
voice, music, ambience, or style. Use the exact positional tokens and ordering
shown by the live endpoint schema.

For a direct continuation that needs the predecessor's state, a tail can
carry its visible setting and motion; omit duplicate stills. For a new shot
with the same person, start with the approved identity image and text direction.
Add a location, costume or motion donor only for an explicit requirement or a
specific failure observed in a simpler attempt. Do not attach a posed image or
prior clip solely because it exists. Inspect unwanted composition/pose cues in
the inputs before another paid retry. Voice continuity needs its own audible
check; a face match or “same voice” prompt is not proof.

Current H3 documentation describes a unified multimodal context with images,
video clips, and audio tracks, but its published counts and combined-duration
limits are discovery hints rather than a frozen request contract. Validate them
against the selected endpoint immediately before generation. Audio references
may require an accompanying image or video; never send audio alone unless the
live schema explicitly permits it.

Use start and end frames for temporal boundaries, not as substitutes for a
subject reference. Use reference media for identity or performance, not as an
unexplained pile of examples. Remove near-duplicates and conflicting donors.

## Prompt motion, camera, and sound

State one principal action, its start and end state, environment, camera
framing and movement, lighting, and continuity obligations. Refer to supplied
media using the endpoint's exact tokens rather than prose such as “the third
image” when tokens are available.

H3 can generate native stereo audio on supported routes. Specify dialogue,
speaker, language, delivery, foley, ambience, and score separately. If using a
voice or performance reference, say what transfers and what must not transfer.
Do not trust native speech for exact regulated or brand-critical wording
without transcription and human review.

For localized editing, name the smallest intended change and list invariants:
subject identity, wardrobe, framing, camera path, timing, background, lighting,
and audio. Treat every edit as a new candidate that must be reviewed; do not
overwrite the last accepted version.

## Continue and review

For a planned successor, preserve the editorial state in the ledger and choose
only the references needed for the incoming shot. A direct continuation may
use a 2- or 3-second accepted predecessor tail as Video 1. A new composition
may use identity-only references and text without the tail. Record why inputs
are retained or omitted, then check the result against the planned handoff.
H3 Max produces a new bounded file; it does not append footage or guarantee an
exact vocal/mouth state at the boundary.

Persist the queue request ID immediately and rejoin it after timeouts. Download
the result once and perform a clip receipt: `ffprobe` the actual file and
inspect its stable opening and ending frames. For a visible-dialogue boundary,
also watch the join and check the mouth/phoneme and audible speech handoff;
still frames alone cannot prove it. Call `show_video` for each reviewable
candidate. Do not run a full contact-sheet, black/freeze, audio, prompt, and
seam audit for every preview. If a visible boundary is wrong,
regenerate or redesign the successor through Reference-to-Video with a more
specific prompt/reference set; do not hide it with a bridge clip, crossfade,
blend, reframe, zoom, or other creative FFmpeg repair. The full inspection is
run once on the final direct-concatenated delivery MP4.

## Treat speed and playback honestly

H3 Max may report a very short `inference` duration, but that is model time
only. Do not turn it into a fixed end-to-end promise: queueing, reference-media
upload, output download, local validation, and human review also take time.
Record and report those intervals separately.

The API returns a completed MP4. It does not stream partial playable footage,
append a continuation onto the predecessor file, or guarantee that a successor
will be ready before the current clip ends. A UI may begin playing each
completed clip immediately and can provide gapless playlist playback only when
the next completed clip is already buffered. For Reference-to-Video, wait until
the immediate predecessor exists; do not pretend the next request can start
from footage that has not been returned. Video Studio requires a lightweight
boundary receipt before accepting a successor, and one full QA pass only after
the final assembly exists.
