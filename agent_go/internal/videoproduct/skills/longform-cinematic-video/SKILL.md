---
name: longform-cinematic-video
description: Direct a coherent cinematic video of any duration from story architecture through deterministic or generated sequences, continuity-controlled clips, editorial stitching, sound, and final seam review. Use as the default director for every new Video Studio production so scenes feel like one continuous authored film rather than separate generations.
---

# Direct one film, not a collection of clips

Own the production-level decisions that cross model generation and editing.
Read `video-storytelling`, `video-cinematography`, `minimax-h3-video`,
`video-provider-capabilities`, and `video-editing` for their specialist
rules. Keep those responsibilities coordinated by one sequence plan and one
editorial grammar.

## Define the cinematic contract

The user chooses the visual style: photoreal, hand-drawn, 2D/3D animation,
motion graphics, documentary, slideshow, or mixed media. Cinematic means
coherent storytelling and intentional picture/sound direction, not realism.
A photoreal host may have illustrated historical cutaways. Do not refuse or
convert those inserts to photoreal. Realistic characters are not required.

Before scripting, lock the film's dramatic question, audience, runtime,
format, emotional arc, point of view, visual language, camera grammar, color
journey, sound world, and continuity priorities. State what must remain exact
across the film: character identity, wardrobe, props, geography, time of day,
screen direction, eyelines, lighting motivation, and recurring audio motifs.

Choose a small visual vocabulary and repeat it intentionally. A cinematic
film does not need a new style, lens, camera move, or transition for every
beat. Use the user-approved medium for each sequence; a film may combine generated
footage, supplied media, and deterministic animation. H3 reference and
continuation rules apply to generated-video shots, not every visual sequence.

## Plan sequences before shots

Organize the film as chapters, sequences, scenes, and only then shots. Give
each sequence an entrance state, dramatic turn, exit state, location,
characters, time, lighting, sound, and continuity bridge into the next
sequence. A script beat is not automatically a new generated clip.

Create `longform-sequence-plan.json` before paid video generation. Include:

- chapter, sequence, scene, and shot identifiers;
- measured narration or dialogue duration covered by each sequence;
- H3 route chosen by actual controls: text-only, first-frame or minimal references;
- references and their exact semantic roles;
- incoming and outgoing character, prop, geography, motion, camera, lighting,
  and audio state;
- generation topology: one H3 take, Reference-to-Video continuation, a
  motivated H3 camera-angle change, or intentional hard cut;
- planned cut point, transition grammar, handles, and expected seam risk;
- the reason every independent generation is unavoidable.

Before expanding production, follow `cinematic-visual-development`: choose the
minimum sufficient approved references and test one representative complete
scene. A recurring human may need only a face identity image, with wardrobe,
background, movement and framing in text. Full packs, background plates and
start/exit images are conditional on the brief or an observed gap. The first
test should exercise the difficult requirements, not merely animate a portrait.

Minimize generations and seams. Prefer one H3 take within the supported limit
when action is continuous. For a later shot, distinguish required narrative
continuity from reference-media inputs: use identity alone for a new
composition, and add the accepted predecessor tail only when its state is
needed. Check joins against the agreed editorial boundary. Keep stable frames
for review even when they are not sent to the provider. Never invent an
endpoint field or generate a third bridge clip to hide a failed handoff.

## Generate for the edit

Generate sequence by sequence, not as an unrelated batch. Preserve the same
approved identity and required wardrobe, geography, lighting, motion and
audio invariants across adjoining clips. Choose the route and reference inputs
for each shot; do not blindly repeat every prior input. Describe overlapping action at a seam so the editor can cut on motion.
Ask for clean head and tail handles without filling them with new actions.

Show every new clip with `show_video` and obtain its review outcome before it
becomes an assembly input. Record accepted and rejected versions in
`longform-continuity-ledger.json`; never overwrite the last accepted clip.
For each accepted clip record its source request, actual duration, usable trim
range, incoming/outgoing state, boundary frames, audio state, and the next clip
it can legally join.

Reject a clip before assembly when identity, spatial continuity, screen
direction, motion, lighting, or action state contradicts its neighbors. An
editor cannot repair a fundamentally incompatible generation with a dissolve.

## Stitch with cinematic grammar

Normalize technical media properties first, then make editorial decisions.
Create `longform-edit-decision-list.json` with source version, in/out time,
timeline time, cut type, audio lead/trail, transition, grade, speed change,
and linked narration beat for every segment.

Use motivated cuts in the H3 prompt: state whether the next clip begins on an
action, eyeline, reaction, insert, or a deliberate scene reset. Reference-to-
Video may carry an accepted predecessor tail as Video 1 when its state is
needed; identity-only references are valid for a planned new composition. The final assembly is a direct concat: do not add
J/L bridges, dissolves, fades, cutaway repairs, grading, or audio repairs to
make separate generations appear continuous.

After each successor, confirm the downloaded clip with `ffprobe` and inspect
its stable first/last frames. Compare the join enough to catch an obvious
failure. If it fails, correct the H3 prompt or reference set and regenerate the
successor. Do not create a per-seam render, trim plan, or separate seam report.

## Inspect the delivered film once

After direct concat, run the single final `video-quality` report with the
`generated-video-quality` extension. It inspects the actual delivery MP4,
including its joins, narrative timing, picture, and sound. A visible continuity
failure returns to H3 successor regeneration; it is never hidden by an edit.
