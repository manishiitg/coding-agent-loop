---
name: video-model-selection
description: Choose which fal.ai or Google Gemini/Veo model fits one shot's requirements -- text-to-video vs image-to-video, character/subject consistency, duration limits, native audio, cost. Use before the first generation call in a long-form production, and again whenever a shot's requirements differ from the ones already resolved. Read alongside fal-ai and google-ai, which own the client mechanics once a model is chosen; this skill owns the choice itself.
---

# Choosing a generation model

`fal-ai` and `google-ai` teach how to call a model once you've picked one.
This skill teaches how to pick it. Read `references/model-capabilities.md`
whenever quoting model duration, resolution, aspect ratio, audio, language, or
reference support. Read `references/cost-guidance.md` whenever the user
compares models, asks about price, or must approve a paid plan.
Neither this skill nor either of those
pins a model catalog or a "best model for X" table -- both providers'
catalogs, capabilities, and pricing change independently and on their own
schedule, and a fixed ranking here would go stale exactly the way a fixed
model ID would.

## Resolve production-level unknowns before the first paid call

`video-creation` already asks one compact question only when a missing choice
would substantially change the result or cost (see "Understand the request").
A long-form AI-generated production has its own unresolved-question shape,
and getting it wrong is more expensive to unwind than a typography choice: a
wrong shot is a paid regeneration, not a free re-render. Before the first
generation call, resolve whatever the brief hasn't already answered:

- **Provider/model preference.** Does the user already know they want
  fal.ai, Google, or a specific model? If not, state the tradeoff briefly
  (see "Decide by requirement" below) rather than picking silently on their
  behalf for a paid call.
- **Character/subject consistency requirements.** Does any character,
  presenter, or product need to recognizably recur across multiple shots?
  This determines which models are even viable (see
  `video-cinematography`'s consistency section) and must be known before the
  first shot of that subject is generated, not discovered after three
  independently-drifting attempts.
- **Shot count vs. budget.** A true long-form (8-15+ minute) production is
  assembled from dozens of separately-generated clips. Do the arithmetic
  for the actual model in hand rather than quoting a fixed number: a
  10-minute piece is 600 seconds of finished runtime, so a model producing
  5-second clips needs ~120 calls, one producing 15-second clips ~40, and
  one producing 30-second clips ~20 -- before accounting for re-prompts,
  which the regeneration tolerance below multiplies on top. Per-call
  duration is therefore a budget decision as much as a creative one. If the
  user hasn't stated a cost ceiling or a maximum acceptable number of paid
  generation calls, ask once, compactly, showing the call-count arithmetic
  for the model actually being considered -- do not silently generate an
  open-ended number of variants across a production this large.
- **Regeneration tolerance.** If a shot doesn't match the brief on the first
  attempt, how many re-prompts is reasonable before stopping to ask the user
  rather than continuing to spend on the same shot? State a default (e.g.
  two re-prompts) and get confirmation rather than assuming unlimited
  budget for one stubborn shot.

Ask these as part of `video-creation`'s existing request-understanding pass,
not as a second separate interview later.
Write the resolved answers into `production.json` so a revision does not
re-ask what was already decided.

## Character references: lock the approved reference model before the first image

First follow `cinematic-visual-development`: reuse approved imagery or a reviewed
local crop and test a representative complete scene before expanding assets.
A human identity may need only a face-only image; exact products or costumes
may need richer evidence. When a new generated reference is actually needed,
Video Studio's system policy selects `fal-ai/flux-2-max` for the first approved
character or background master and `fal-ai/flux-2-max/edit` for controlled
derivatives. Verify the current Fal schemas and billing before the first
reference-image call, present the bounded reference-pack cost for approval,
and record the exact endpoint, pack size, retry allowance, and billing evidence
alongside the character. Do not offer a model-selection menu or substitute a
different still-image model unless the user explicitly asks to change the
policy.

Record the reference-image provider/model separately from the H3 video route.
Reuse the approved identity; choose the minimum reference inputs each shot
needs. A posed image, full-body sheet or previous video is not automatically
required for a later appearance of the same character.

## Present a costed choice before spending

Before the first paid generation, give the user at most three viable model
choices. For each, state in plain language: provider and exact model, why it
fits the shot, output duration/resolution, live billing unit and rate, planned
cost, and the maximum cost after the approved retry allowance. Recommend one
choice. Recommend the least-expensive option that satisfies the shot's hard
requirements; do not treat a premium tier, a newer version number, or the word
"cinematic" as a reason by itself. Price must not override a required
continuity, lip-sync, reference, duration, or resolution capability, but an
optional quality preference must not silently override the user's budget
either. State the exact capability that justifies every price increase.

Use this practical routing order after checking the live endpoint schemas:

1. Start with a budget-capable endpoint such as Veo 3.1 Lite when it satisfies
   the shot's duration, resolution, reference, and synchronized-audio needs.
2. Move to Kling Standard or Pro only when its structured multi-shot prompts,
   custom character/object elements, longer take, voice control, or output
   resolution materially benefits this shot. Try Standard before Pro when its
   controls are sufficient.
3. Move to Seedance 2.5 when a 15--30 second continuous take or its large mixed
   image/video/audio reference set avoids costly seams or continuity failures.
   Do not use it as the default for ordinary short shots.
4. Use Veo Fast/Standard, 4K routes, or another premium endpoint only for a
   requirement the cheaper viable routes cannot meet.

This is a routing heuristic, not a frozen ranking. A recurring character does
not automatically require Kling, and lip-sync alone does not automatically
require Kling: compare every current endpoint that supports the approved
speech design. Read `references/cost-guidance.md` for the maintained comparison
and recheck all rates before presenting them.

Do the capability check before presenting the shot count or total runtime, not
after the user approves the plan. Convert the exact route's allowed duration
enum into the shot plan. For example, a route limited to 4, 6, or 8 seconds
cannot be represented as seven 10-second calls; choose supported durations,
re-space the beats, and show the resulting call count and cost before approval.
Never quote a family-wide limit when the selected mode or tier has a narrower
contract.

**A dollar total is allowed only when the live billing unit can be mapped to
the planned output without guessing.** If fal returns `seconds` for the exact
endpoint, calculate from the requested output seconds. If it returns `video`,
calculate from the planned call count. If it returns `compute seconds`,
`tokens`, credits, GPU time, or another non-output unit, do **not** multiply it
by the requested video duration and do not present a made-up “base” or “max”
total. State the live unit rate, say that the provider has not supplied a
reliable output-duration conversion, and offer a small capped paid test or ask
for a user-approved dollar cap. This is especially important for the current
fal MiniMax H3 reference route (compute seconds) and Seedance 2.5 reference
route (tokens). Never call such a number an estimate, recommendation, or
price quote.

Use `references/cost-guidance.md` for the calculation, published anchors, and
provider-specific price lookup. A static table is only orientation. fal prices
must be resolved from the current account-facing pricing API for the exact
endpoint; Google prices must be rechecked on the official pricing page; direct
Seeddance estimates must use its available credit information. If a provider
does not expose enough information for a reliable dollar estimate, say that
plainly and offer a bounded low-cost test instead of converting credits by
guesswork.

Record the comparison and the user's approved option in `production.json` and
the generation ledger. A user approving a storyboard is not approving an
unbounded spend increase caused by a different model or retry count.

## Decide by requirement, not by brand loyalty

For each shot, resolve these dimensions before choosing a model. Get the
answers from the provider's own current model/capability listing (fal.ai's
model explorer, Google's Gemini API model reference), not from memory:

- **Input mode**: text-to-video, image-to-video (animating a reference
  frame), or image+text combined. A shot that must match a specific
  reference image or an established character needs image-to-video support,
  not just a strong text-to-video model.
- **Duration and shot length**: confirm the model's actual max clip length
  against what the storyboard needs for this shot. No model generates an
  8-15+ minute clip in one call -- a true long-form piece is always
  assembled from many short generations, so do the arithmetic explicitly
  (total runtime ÷ per-shot duration = roughly how many generation calls
  the production needs) rather than assuming duration is a one-time choice.
  A model with a longer native per-call duration (e.g. Seedance 2.5's
  native 30-second generation, versus a model capped at 5-8s) reduces the
  call count and the number of stitch points for the same total runtime,
  which is a real cost and consistency advantage at this scale, not just a
  convenience. A generated clip rarely comes out at exactly the beat's
  needed length -- see `video-editing`'s "Stitching independently
  AI-generated clips" for how to trim, normalize, and concatenate dozens of
  generated clips into the final assembly.
- **Subject/character consistency across shots**: if the same character,
  product, or object must recognizably recur across multiple generated
  shots, check whether the model supports reference-image conditioning or a
  character-consistency feature before committing to it for that whole arc
  -- switching models mid-arc after shots are already generated is expensive
  rework. See `video-cinematography` for the production-side techniques that
  apply regardless of which model you pick.
- **Native audio**: some video models generate synchronized audio (dialogue,
  ambience, SFX) directly; others produce silent video that needs a separate
  voice/music generation pass (see `fal-ai`'s and `google-ai`'s audio
  capabilities) plus assembly in `video-editing`. Before selecting between
  them for a production with people/characters and spoken content, use
  `video-look-sound` to present visible native dialogue, off-camera TTS voiceover,
  and hybrid as explicit user choices. Explain their lip-sync,
  voice-continuity, cost, model, and edit-complexity tradeoffs. An endpoint
  without native audio must never silently turn a speaking character into a
  non-speaking performance with narration. Decide the resulting audio mode per shot
  rather than once for the whole production, since a narrated hook and a
  silent B-roll cutaway may want different models -- but only for shots
  with no recurring character or subject in them. Any shot featuring a
  recurring subject is bound to that subject's committed model and provider
  (see "Keep the whole character arc on one model and provider" in
  `video-cinematography`). Confirm audio support before committing that model.
  If the user later changes an approved recurring character from voiceover to
  visible dialogue and its committed model cannot produce synchronized speech,
  revisit the model choice and obtain explicit approval for the continuity,
  cost, and regeneration consequences; do not manufacture lip sync or silently
  keep the character mute.
- **Cost and generation time**: video generation is priced and timed very
  differently from image generation, and cost typically scales with
  duration and resolution. Confirm current per-call pricing before
  generating anything expensive at full resolution -- a cheap low-res test
  pass first is usually worth it for an unfamiliar model (see the cost
  discipline in `fal-ai`/`google-ai`).
- **Aspect ratio and resolution support**: confirm the model natively
  supports the production's required aspect ratio and resolution rather than
  planning to crop/pad a mismatched output after the fact, which degrades
  framing decided at generation time.
- **Open vs. closed weights**: most hosted models are closed API-only. A
  minority ship open weights, meaning the model can also be self-hosted or
  fine-tuned rather than only called as a paid API -- relevant if the brief
  has licensing constraints, or the user asks specifically for an
  open-weight option rather than assuming every model is equivalent on this
  axis.

## Notable options as of 2026-08-17 -- a starting point, not a source of truth

This list was checked against fal.ai's and Google's own docs on 2026-08-17. It
will go stale -- confirm the exact model ID and current
capabilities against the provider's own live reference before calling
anything, per "Never invent a model ID" in `fal-ai` and `google-ai`. Do not
extend this list from memory in a later session; re-check instead.

- **Video, hosted on fal.ai**: Seedance 2.5 is live under
  `bytedance/seedance-2.5/text-to-video`, `.../image-to-video`, and
  `.../reference-to-video`. Current schemas expose `auto` or 4-30 second
  clips, native synchronized audio, first/last frames on image-to-video, and
  up to 30 images, 10 videos, and 10 audio references (50 total) on the
  reference route. Seedance 2.0 remains available in Standard and Fast route
  families and can be the better choice for its separate price, latency, and
  current 4K options. Compare the exact live schemas rather than treating 2.5
  as a universal replacement. MiniMax
  H3 (`minimax/hailuo-03/text-to-video`, `.../image-to-video`,
  `.../reference-to-video`)
  is the notable open-weights option -- self-hostable/fine-tunable, not
  only a closed API -- generating 2K, 5-15s clips at 24 FPS with native
  stereo audio and rich multimodal reference input (up to 9 images for
  subject/style, 3 video clips for motion, 3 audio clips), which also makes
  it a strong reference-conditioning option for character consistency. Also
  available: Veo 3.1 / Veo 3.1 Lite (native audio, lip-synced dialogue),
  Kling 3.0 Pro (cinematic, native audio, multilingual lip-sync), Wan 2.6,
  LTX 2.0. Gemini Omni Flash is also hosted on fal.ai under
  `google/gemini-omni-flash` with image, reference, and edit routes; use
  `gemini-omni-video` for its current route-specific controls rather than
  assuming the landing page's multimodal claims appear as fields on every
  endpoint.
- **Image, hosted on fal.ai**: FLUX, and Google's Nano Banana models
  re-hosted alongside fal.ai's own catalog.
- **Voice/audio, hosted on fal.ai**: ElevenLabs (text-to-speech, voice
  cloning), plus separate music-generation models.
- **Image, direct via Google's Gemini API**: Nano Banana 2 =
  `gemini-3.1-flash-image` (most versatile); Nano Banana 2 Lite =
  `gemini-3.1-flash-lite-image`.
- **Video, direct via Google's Gemini API**: Veo 3.1. Do not infer that the
  fal-hosted `google/gemini-omni-flash` endpoint ID is accepted by Google's
  direct API; provider routing and model IDs are separate contracts.
- **Narration/TTS, direct via Google's Gemini API**: Gemini TTS, in preview
  at the time of writing under ids of the shape
  `gemini-3.1-flash-tts-preview` and `gemini-2.5-flash-preview-tts` /
  `gemini-2.5-pro-preview-tts`. Output is raw PCM needing WAV wrapping, and
  multi-speaker is capped at two -- see `google-ai` for the mechanics. This
  matters for provider routing, not just capability: it means Google alone
  can carry a narrated production end to end, so a missing fal.ai key is
  not a blocker for anything except a generated music bed.

One real overlap worth knowing: Veo 3.1 is reachable through both fal.ai
(hosted) and Google's own API (direct). Where a model is available through
both, the choice is about aggregation convenience and pricing, not
capability -- fal.ai gives one unified surface across many vendors' models
under one key; going direct to Google skips that layer for Google's own
models specifically.

Seedance also has two provider paths in this product. fal.ai hosts Seedance
under its aggregator key; the independent Seeddance API uses its own key and
direct model IDs such as `seedance-2.5`. Read `seeddance-api` before choosing
the direct path. The same model family name does not make schemas, account
access, pricing, task IDs, or credentials portable between those providers.

## Prompt length and structure are model-specific

Once a model is chosen, `video-cinematography`'s five-aspect formula
(subject, motion, scene, spatial framing, camera) still applies, but how
much of it to spell out, and how long the prompt should run, varies by
model and changes as models version forward -- re-confirm rather than
reusing these numbers indefinitely. As last checked (dated, may have
shifted with newer versions):

- **Seedance** is unusually literal about camera language and rewards a
  fully-specified, long, structured prompt -- roughly 200-400 words for a
  hero shot, 80-150 for a cutaway/insert. It also supports an explicit
  multi-shot structure inside one generation call (`Shot 1 (...)`,
  `Shot 2 (...)`, ...), which tends to hold subject identity more reliably
  across those shots than separate calls do, since the identity anchor is
  interpreted once per call instead of cold-started per shot -- prefer this
  for a tight sequence of the same subject when the model supports it.
- **Veo** responds to a comprehensive structured prompt (subject, action,
  scene, camera angle, camera movement, lens/optical effects, lighting,
  tone, style, ambiance, pacing, audio, editing terms, negative prompt) and
  natively generates dialogue and synchronized ambient audio/music when
  asked for it in the prompt. Sweet spot is roughly 100-250 words; longer
  prompts tend to stop helping past that. **Veo may add unrequested
  subtitles by default for dialogue** -- add "no subtitles, no captions, no
  text overlays" to the negative prompt to prevent this (see also
  "Overlays are not scene depth" in `video-cinematography`).
- Lighter-weight or lower-latency models tend to plateau on prompt length
  much sooner (well under 100 words) and reward simple, motion-focused
  language over exhaustive detail -- check the specific model's own
  guidance rather than assuming a longer prompt is always safer.

## Provider is a routing decision, not a preference

- Use `fal-ai` when the brief needs a specific third-party model (Kling,
  Runway, Seedance, Hunyuan, and similar) that fal.ai hosts, or when you want
  one aggregator surface across many vendors' models under one key.
- Use `google-ai` when the brief specifically calls for a Google-native
  model -- Gemini image generation, Veo, or Gemini TTS -- rather than a
  third-party one.
- Use `seeddance-api` when the brief selects Seedance and the direct service's
  account access, price, or limits are preferable to fal. It generates
  Seedance video only; image creation, narration, and music still require an
  appropriate additional provider.
- **Which credentials actually exist is part of this decision.** Confirm
  which provider keys are configured before planning a production around
  one. fal.ai or Google alone covers video, image, and narration, so either
  key can carry a complete piece; the direct Seeddance key covers only its
  Seedance video surface. Only a generated music bed is fal.ai-only (see
  `google-ai`'s note on music). Plan within the keys
  available, or say plainly which key is missing and what it would add --
  do not design a production around a provider the user cannot call.
- The same production can legitimately use both: e.g. Google's model for a
  product shot that needs Gemini's specific strengths, fal.ai for a stylized
  B-roll shot from a different vendor's model. Record which model produced
  which shot in `production.json` (see `video-creation`) so a revision knows
  which client/skill to reuse for that specific shot.

## Record the decision, not just the output

For every shot, write down in `production.json`: the resolved model ID, the
provider (`fal-ai`, `google-ai`, or `seeddance-api`), the exact input/arguments
used, and why
that model was chosen over alternatives if the choice was non-obvious. A
revision to one shot should not have to re-derive this reasoning from
scratch, and a viewer noticing an inconsistency between two shots (different
apparent style, different character appearance) is easiest to diagnose when
the model-per-shot record already exists.

## Where this fits

- Use `video-creation` to plan the shot list and own the overall brief.
- Use `video-storytelling` to structure the narrative arc and pacing before
  a shot list exists to choose models for.
- Use this skill to choose a model per shot.
- Use `video-cinematography` to turn the creative intent for that shot into
  the actual prompt/camera/lighting direction handed to the chosen model.
- Use `fal-ai`, `google-ai`, or `seeddance-api` (per the chosen provider) to
  make the call.
- Use `video-editing` to assemble the results, `video-quality` before
  presenting any version as complete.
