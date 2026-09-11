---
name: video-creation
description: Direct a video project from a conversational brief through a reproducible production. Use when creating a new video, deciding the production approach, turning uploaded assets into a video, scripting or storyboarding, or revising an existing video.
---

# Direct video creation

Treat the conversation as the creative brief. Do not force the user through a formal plan or expose implementation details unless asked -- with one exception, below: a production that spends the user's money on generated footage has checkpoints, and holding them is not the same as making someone fill in a form.

## Work inside the project

- Treat `uploads/` as immutable user-owned source material.
- **Working directly in chat:** use `work/` for scripts, manifests, generated shots, audio, frames, and temporary files; put final playable videos in `outputs/`. A project can hold more than one production, so give each its own folder — `work/productions/<slug>/` — and keep that production's character specs and reference images together in `work/productions/<slug>/characters/<name>.md` and `.png`. Record those exact paths in `production.json`; the panel that shows a character is told where it is rather than guessing.
- **Running as a workflow stage:** write only inside your own step folder under `runs/<iteration>/<group>/execution/<stage>/`. `work/` and `outputs/` are not yours, are normally empty, and are not where a stage's output lives — treating them as the source of truth after a stage runs reports that nothing was produced when the artifacts exist.
- Keep reusable source files and commands so this same resumed session can revise the video later.
- Never publish, share, or upload a result.

## Understand the request

Infer as much as possible from the chat and uploaded assets. Establish only the details that materially affect the result:

- intended audience and outcome;
- platform, aspect ratio, resolution, and approximate duration;
- core message, tone, visual references, and call to action;
- narration, presenter, music, captions, and brand assets;
- whether to edit supplied footage, generate new shots, or combine both.

Ask one concise question only when a missing choice would substantially change the result or cost. Otherwise make a reasonable choice and state it briefly.

**This changes when the production will generate footage.** The guidance above is calibrated for uploaded assets and deterministic composition, where a wrong guess costs a free re-render. Paid generation is different in kind: a wrong character or a misjudged scope is money already spent, and no amount of later editing recovers it. For any production that will call `fal-ai` or `google-ai`, work through the checkpoints below instead of inferring your way to a finished video.

## Checkpoints for a generated production

Direct chat has no stage gates -- the `longform` and `shortform` workflows
do, and running in chat does not mean skipping what those gates are for. It
means you hold them yourself. Stop at each checkpoint and let the user
answer before spending:

1. **Before the first paid call, choose the minimum needed controls.** Resolve
   which identities or exact assets must persist. Follow
   `cinematic-visual-development`: reuse approved references or a face crop,
   and describe clothing, scene, action and camera in text where sufficient.
   Do not require a full reference pack before trying a complete scene. When
   new reference imagery is necessary, follow Video Studio's FLUX.2 Max policy,
   verify the live cost and obtain the required spending approval. Confirm
   provider credentials, cost ceiling and retry allowance. If no subject
   recurs, record that identity references are not applicable.


2. **Show the plan before building it.** When the script and shot list
   exist, call `show_document` for each so the user reads what they are
   paying for. A shot list is cheap to change and expensive to regret.
3. **Show every recurring character before generating a single shot of it.**
   Once its spec and reference image exist (see `video-cinematography`),
   call `show_character` and wait. This is the highest-value checkpoint in
   the whole product: every later shot is conditioned on that reference, so
   an unapproved face propagates through the entire piece and can only be
   fixed by regenerating all of it.
4. **Generate one shot, then stop.** Test one representative complete scene
   before expanding assets. The first shot of a new character,
   scene, or model choice is a sample, not a commitment. Generate it alone,
   show it with `show_video` as a Preview (omit `qa_report_path` until the
   final quality pass), and wait
   for the user's reaction before generating anything else. If the batch
   has several unrelated shots, one representative sample is enough --
   commit the whole direction on one unapproved guess only when the user
   has explicitly said not to check in.
5. **Never let stitching be the first look.** Do not generate several clips
   and hand the user a combined preview as their first view of any of them
   -- by then every clip in it was made without feedback, and rejecting the
   combination means rejecting all of them at once. Show each new clip on
   its own before it is added to an assembly with anything else.
6. **Then generate the rest**, staying inside the agreed ceiling, and
   report what was spent against it.

Skipping these because the user seemed to be in a hurry is the wrong trade:
they are what makes a paid production correctable while correcting it is
still cheap. A user who genuinely wants no checkpoints will say so, and
that is a decision they get to make explicitly rather than one you make for
them by staying quiet. Generating a whole batch and presenting only the
finished, stitched result is exactly the failure mode this section exists
to prevent -- it is not a shortcut, it is the checkpoint being skipped.

## Inspect before producing

1. List the files in `uploads/` and inspect their type, dimensions, duration, frame rate, audio streams, and orientation with local tools such as `file`, `ffprobe`, and sampled frames.
2. Never infer asset contents from filenames alone.
3. Preserve logos, faces, product appearance, and brand colors from supplied references. Do not invent exact brand claims.
4. Prefer editing existing assets and deterministic composition before paying to regenerate usable material.

## Shape the story

- Give every video one clear job and one primary audience.
- For short-form video, prefer `hook -> value/proof -> action`.
- Keep each shot responsible for one idea. Match the visual change to the spoken beat.
- Put exact wording, logos, UI, prices, and captions into the editing/overlay layer, never into an AI-generated shot.
- For presenter reels, a reliable pattern is host hook, screenshot-worthy value card while narration continues, host close/CTA, then a short end card.
- Begin every fresh video with `longform-cinematic-video`, regardless of runtime, so the cinematic contract, continuity, sound world, and final edit are designed as one film. For a short piece, scale its chapter/sequence artifacts down rather than switching to a less coherent visual grammar.

## Make the work resumable

In direct chat, create or update `work/production.json` before substantial media work. Record:

- target specifications and creative direction;
- source assets and their roles;
- script or beat list;
- shot status and generated filenames;
- for an AI-generated production, every recurring character/subject: the path to its spec and reference image under `characters/`, and which model and provider its arc is committed to (see `video-cinematography`);
- music, caption, and overlay decisions;
- final output versions and QA status.

Use stable, descriptive filenames. Never overwrite an approved output; write `outputs/<slug>-v01.mp4`, then increment the version.

As a workflow stage, the equivalent record is your stage's own artifact (research.md, proposal.md, script.md, scene-plan.md, or the asset/edit/render/QA files) — the next stage reads it as its dependency, so put in it whatever that stage needs to avoid re-deriving your work. Other skills refer to this record as `production.json` for brevity; read that as `work/production.json` in direct chat and as your stage's own artifact when running as a workflow stage. Never write `production.json` or `characters/` outside your step folder as a stage.

## Choose the production path

- Use local editing for trims, crops, concatenation, audio, captions, and supplied footage.
- Use programmatic overlays for exact text, branded cards, product UI, or repeatable templates.
- Use `longform-cinematic-video` as the director for every new production. It coordinates story, cinematography, continuity, provider capabilities, generation when approved, editing, and seam review around one sequence plan.
- Honor the user's chosen medium and style, including hand-drawn illustration, animation, documentary, motion graphics, slideshow, photoreal footage, and mixed media. Realistic characters are not required. HyperFrames can create entire approved sequences or inserts; keep their story, pacing, and sound coherent with the production. Do not replace requested illustrated historical cutaways with photoreal footage.
- Use the `video-editing` skill for assembly, captions, audio, and exports.
- Use the `video-quality` skill before presenting a version as complete.

Avoid paid multi-variant generation unless the user requests alternatives. Cache every successful generation and never repeat a paid call merely to inspect its response.

## Finish the turn

In direct chat, when work produces a video, report its relative path under `outputs/`, summarize the creative result in plain language, and mention any unverified requirement. Do not include provider or command details unless asked.

As a workflow stage, finish by writing your required artifact and nothing else — the product reports progress to the user from stage state, not from your reply text.
