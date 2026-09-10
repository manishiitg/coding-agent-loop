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
- H3 route: Reference-to-Video for anchors and every normal continuation;
- references and their exact semantic roles;
- incoming and outgoing character, prop, geography, motion, camera, lighting,
  and audio state;
- generation topology: one H3 take, Reference-to-Video continuation, a
  motivated H3 camera-angle change, or intentional hard cut;
- planned cut point, transition grammar, handles, and expected seam risk;
- the reason every independent generation is unavoidable.

Before the first anchor, turn that plan into a user-approved visual-development
pack: location/background boards, recurring wardrobe and hero-prop references,
and start/exit references for every continuity sequence. References must be
real files, not just prompt language; show each one to the user and record its
role in the manifest. Carry an accepted predecessor through H3
Reference-to-Video for every normal continuation. Use first/last stable
boundary frames for direct-cut review and a corrected successor prompt when a
review fails; never invent an endpoint field or create a bridge clip.

Minimize generations and seams. Prefer one H3 take up to the live-supported
limit when the action is continuous. When a new call is necessary, use H3
Reference-to-Video with the accepted predecessor and overlapping action. Use
independent clips only for motivated discontinuity such as a montage, time
jump, location change, or deliberate cutaway.

## Generate for the edit

Generate sequence by sequence, not as an unrelated batch. Preserve the same
approved model route, reference order, seed when supported, character spec,
wardrobe, geography, lighting, motion vector, and audio bed across adjoining
clips. Describe overlapping action at a seam so the editor can cut on motion.
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
Video must carry the prior accepted clip as Video 1 so H3, rather than the
editor, makes the handoff. The final assembly is a direct concat: do not add
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
