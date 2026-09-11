---
name: multi-clip-cinematic-generation
description: Design a coherent MiniMax H3 sequence using a reference manifest and explicit camera-transition grammar. Use before planning or creating an anchor clip or any follow-up clip in a cinematic sequence.
---

# Generate one cinematic sequence, not unrelated clips

This skill owns the decisions that must happen **before** an AI-generated
follow-up clip exists. Read `minimax-h3-video`, `video-provider-capabilities`,
and `fal-ai` before a paid call; endpoint controls are not interchangeable.
Read `video-stitching` after clips are approved to plan and verify the edit.

## Choose the sequence topology first

When the user has not set a per-clip duration, shot count, or beat boundary,
use the smallest number of H3 generation boundaries that preserves the
intended action. Those explicit user constraints win over cost and seam-count
optimisation. Select and record one route for each seam:

1. `minimax/h3-max/text-to-video` for a prompt-only standalone opening shot
   with no reference or continuity obligation;
2. `minimax/h3-max/image-to-video` when an approved start image must control
   the opening composition;
3. `minimax/h3-max/reference-to-video` with identity-only references for a new
   shot, adding an accepted predecessor tail only when its state is needed;
4. a motivated editorial cut preserving the required identity and scene state;
5. an intentional discontinuity such as a time jump, location change, or
   montage beat.

Never call separately generated clips a continuous take merely because a
dissolve can join them. Reference-to-Video conditions a new successor; it does
not append the predecessor or guarantee a frame-, mouth-, or audio-exact seam.
Keep uninterrupted on-camera dialogue and one continuous action in one H3 Max
take whenever it fits 5–15 seconds. When a new request is unavoidable, the
shot list must name an editorial boundary—a completed thought/pause, reaction,
insert, or motivated angle change—not a mid-word continuation. Do not rewrite
user-approved dialogue to fit that boundary without explicit approval. Do not
create a third bridge clip when a direct review fails; regenerate or redesign
the affected H3 successor through Reference-to-Video.

## Choose minimum references and record the manifest

Follow `cinematic-visual-development` before preparing assets. Test one
representative complete scene with the minimum sufficient references before
expanding packs. A recurring human can start with one approved face-only image
and text-directed wardrobe, location, action and camera. Exact objects,
costumes or geography may need more visual evidence. Do not require background
plates or start/exit images merely because a sequence has multiple shots.

Call `show_reference` for actual new conditioning images and obtain approval
before use. The reference manifest records delivery orientation, required
identity/wardrobe/prop/geography/sound invariants, intentional changes, model
discretion, and actual source paths with their semantic roles. Text direction
belongs in the plan; do not mislabel it as an approved image. One identity
image or an empty image list for a prompt-only shot is a valid manifest.

Keep the accepted predecessor path and stable boundary frames for review.
Distinguish **editorial continuity** from **provider conditioning**: a later
shot must fit the story, but does not automatically need the previous video
as input. Add a tail when the incoming shot needs its motion or composition;
omit it when that would constrain a planned new angle or movement. Record the
choice and check the intended join after generation.

Keep a normal film in one orientation and aspect ratio. Do not mix 16:9 and
9:16 generated footage unless the shot list explicitly calls for an in-world
phone, screen, or archival insert; crop, frame, or composite that insert in
editing rather than silently changing the film's delivery contract.

When a planned HyperFrames insert appears inside a cinematic sequence, treat
it as an intentional boundary, not as a generated continuation: define its
entry and exit frame, sound bridge, and return to the same live-action
orientation and visual world in the shot list.

## Write the handoff before generating the next clip

Record both the outgoing state of clip A and the entry state of clip B:

- subject pose, position, gaze, gesture, movement direction, and object state;
- location geometry, camera side of the action, screen direction, eyeline,
  lens family, framing, camera vector, lighting, and audio environment;
- the exact overlapping action or visual match at the cut point;
- transition type and its motivation;
- the route and reference inputs actually supported by the selected endpoint.

Use the prior clip's **last usable stable frame**, not automatically its final
decoded frame. Do not chain from blur, a blink, a half-gesture, a malformed
face, or an unstable generated frame.

## Use deliberate camera-transition grammar

Keep a continuity cut continuous: preserve the 180-degree side of action,
screen direction, eyeline, geography, subject placement, and motion direction.
Retain the lens/framing family unless a visible motivation justifies a change.
A new angle must be a new editorial shot, not a vague request to continue.

Choose one specific handoff for every new angle:

- **Cut on action:** start clip B on the same turn, reach, sit, door movement,
  or other action clip A exits on.
- **Match cut:** match the subject's pose, a shape, motion direction, framing,
  or object position across the cut.
- **Reaction:** cut from an action to an observer whose eyeline and location
  make the reaction legible.
- **Insert/cutaway:** show a meaningful hand, object, environment, or detail
  while protecting a difficult identity or geography transition.
- **Wide-to-medium-to-close progression:** change scale deliberately while
  keeping the action axis and scene state stable.
- **Reset/jump:** declare a deliberate new time, place, or visual world and
  give the edit an audible or visual separator; never imply continuity.

When changing angle, write the new lens, framing, camera position/vector, and
the matching first action explicitly. Preserve a subject crossing screen left
to right unless the shot uses an intentional, readable reversal. Do not flip
orientation, camera side, or gaze direction by accident.

## Generate, receipt-check, then advance

Create exactly one clip per generation recipe. Reuse the manifest, approved
references, and planned handoff in its request. Use H3 Max Reference-to-Video
for identity conditioning; add the accepted predecessor tail as Video 1 only
when needed for the handoff. Describe intentional changes in text. Show and inspect the MP4
before accepting it. On acceptance, record the actual output and handoff state;
on rejection, record the reason and regenerate or redesign that H3 successor.

For each preview, perform only a clip receipt: `ffprobe` the downloaded asset
and inspect its stable opening and ending frames. For a successor, compare its
opening against the predecessor's ending closely enough to catch an obvious
break. Inspect whether source framing, pose or motion conflicts with the new
shot before paying for a retry; follow cinematic-visual-development. Do not render a per-shot FFmpeg seam preview, set default trims, or
write a seam-proof document. If the boundary is visibly wrong, revise the H3
prompt/reference set and regenerate the successor; never use a bridge clip,
crossfade, blend, zoom, or reframe to conceal it. Full delivery QA happens once
on the final direct-concatenated export.
