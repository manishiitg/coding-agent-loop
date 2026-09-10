---
name: cinematic-visual-development
description: Create and present the user-approved visual evidence that a cinematic sequence needs before video generation: FLUX.2 Max character/background masters and controlled derivatives, subject reference packs, locations, wardrobe, props, lighting, camera language, and start/exit/bridge frames. Use before an anchor or follow-up clip.
---

# Build the evidence before the footage

A cinematic sequence cannot be held together by a text prompt alone. Before a
video call, create a compact, reviewable visual-development pack and record
which approved files are actual endpoint inputs versus editorial targets.

Read `video-provider-capabilities`, `video-cinematography`,
`multi-clip-cinematic-generation`, and the selected provider/model skill.
Never invent an API field merely because a planned end image would be useful.

## Generate the reference pack with FLUX.2 Max

Video Studio uses Fal's **FLUX.2 Max** for all paid character and background
reference imagery. Read the live documentation before each paid call:

- `https://fal.ai/models/fal-ai/flux-2-max/llms.txt` for a new character or
  environment master;
- `https://fal.ai/models/fal-ai/flux-2-max/edit/llms.txt` for a controlled
  derivative from one or more approved masters.

Use `fal-ai/flux-2-max` to generate one reviewable master character sheet or
location/background plate. Once the user approves it, use
`fal-ai/flux-2-max/edit` to derive different camera angles, poses, wardrobe
or prop states, return angles, and character-in-location composites while
preserving the approved subject and world. State which input is the character,
which is the location, and exactly what may change. Do not make a recurring
character or return location from text alone after its master exists, and do
not replace FLUX.2 Max with another still-image model without the user's
explicit approval.

Present and approve the master before creating derivatives. Treat a poor
derivative as a failed reference image to revise, not as evidence that the
character or background has changed. Record each actual endpoint, exact
non-secret input, source references, and approved output in the manifest.

## Classify the recurring subject

Match every reference to the approved style, including illustrated or animated
characters, locations, and objects. Realistic skin, anatomy, lighting, or
materials are criteria only for a requested realistic treatment. For stylized
subjects, preserve the approved shapes, linework, palette, and identity.

Give every recurring subject one explicit type, then build the appropriate
reference pack. A subject can be a person, but it does not have to be.

| Subject type | Minimum recommended references |
| --- | --- |
| Human / presenter | front, three-quarter, profile, full-body, sequence wardrobe, expression or action pose |
| Animal / pet | face, both sides, full body, markings/fur close-up, movement pose |
| Mascot / creature | front, side, silhouette, material/texture, expression or action pose |
| Product / object | front, side, detail/logo, material/scale, in-scene placement |
| Vehicle / robot | hero angle, side/profile, identifying details, material/lighting, action or orientation pose |
| Environment | establishing wide, return angle, practical-light/time-of-day state, key geography or prop |

For a human, do not accept a single portrait as a complete identity pack. A
face alone cannot lock body, wardrobe, profile, or action. For a product,
preserve exact geometry, logo/markings, material, and scale; do not call it a
character merely to make it appear in the Characters panel.

## Build a sequence pack

For each continuous sequence, create and present:

1. the approved subject pack for every recurring subject;
2. a location/background board with return geography, time of day, weather,
   practical light sources, palette, and the side of the action;
3. sequence wardrobe and hero-prop evidence;
4. a planned start frame;
5. a planned exit/end-state frame for every sequence that has a successor;
6. an optional bridge frame when the selected endpoint accepts multiple image
   references or an end-frame control.

Use `show_character` for the recurring subject identity presentation and
`show_reference` for locations, wardrobe, props, start frames, exit frames,
and continuity images. A generated image is not approved merely because it
exists on disk. Wait for the user's approval before footage consumes it.

## Record a usable manifest

Write the pipeline's required `*-reference-manifest.json`. Each entry must
include:

- exact path, title, subject/sequence, and semantic role;
- approved status and review evidence;
- provider/model and prompt used to create it;
- which later shots consume it;
- `endpoint_input: true` only after the selected endpoint's live schema has
  been verified to accept that role; otherwise `editorial_target: true`;
- aspect ratio/orientation, composition, screen direction, eyeline, camera
  family/vector, lighting, palette, and sound-world constraints that persist.

Use only the minimum references that carry a clear purpose. More references
can compete with one another. For a continuation, include the accepted prior
video and its selected last usable stable frame when the chosen endpoint
supports them.

## Gate generation

Block the anchor or continuation when any required reference is missing,
unapproved, inconsistent with the chosen sequence, or unsupported as the
claimed endpoint input. A model change is a new sequence boundary unless the
handoff pack and target-model references are explicitly approved.

After a follow-up clip, perform its lightweight receipt: confirm the file with
`ffprobe` and inspect stable opening/ending frames. If the new opening visibly
breaks continuity, revise the H3 prompt/reference set and regenerate that
successor. Visual development makes a good H3 handoff possible; it does not
authorize an FFmpeg seam repair.
