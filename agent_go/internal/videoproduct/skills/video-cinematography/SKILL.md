---
name: video-cinematography
description: Construct the actual MiniMax H3 video prompt -- the five-aspect formula (subject, motion, scene, spatial framing, camera), precise camera vocabulary, and character consistency across shots. Use after video-storytelling has placed a beat and whenever framing, motion, lighting, or identity drifted from the brief.
---

# Cinematography and consistency for generated video

A generation model responds to specific technical direction far better than
a vague creative brief. This skill turns a storyboard beat into that
direction. In Video Studio, `video-storytelling` places the beat in the
narrative arc, `minimax-h3-video` and `video-provider-capabilities` define
the permitted H3 request, and `fal-ai` makes the call. This skill decides the
camera and visual direction inside that H3 request.

## The self-contained-prompt test

Before generating, check: could someone who has never seen the intended
shot picture the subject, scene, motion, and camera work from the prompt
text alone? If a human reader couldn't picture it from the words, a
generation model will not render it either. Vague, mood-only language
("cinematic," "epic," "moody") fails this test even when it sounds evocative
-- see "Describe the visual cause, not the emotion" below.

## The five-aspect prompt formula

Video generation models reliably render a described subject and scene, but
routinely fail on motion, spatial framing, and camera work when those are
left implicit. Structure every generation prompt to fill all five slots
explicitly, not just the ones that feel obvious:

```
Subject          type + key visual attributes + how to tell apart from
                 other subjects in frame
Subject motion   actions in temporal order; how subjects interact with
                 objects and each other; group action if more than one
Scene            setting + time of day + scene dynamics -- list any
                 on-screen overlays (titles, captions) separately, they
                 are NOT part of scene depth (see below)
Spatial framing  shot size + where the subject sits in frame + depth
                 (foreground/midground/background) + camera height
                 relative to subject -- and how any of this changes
                 during the shot
Camera           playback speed, then lens effects, then height, then
                 angle, then focus/depth-of-field, then steadiness,
                 then movement -- in that order
```

Shorter prompts leave the model more creative freedom; longer, fully-specified
prompts give more control. Neither is universally right -- match the prompt's
density to how much this specific H3 shot needs to be controlled versus
discovered. Use `minimax-h3-video` and the live H3 prompting/schema guidance
for route-specific constraints rather than selecting a different model to
accommodate the prompt.

### Ordering multiple subjects or events

When a prompt lists more than one subject or event, order them deliberately:
**temporal order** when things happen in sequence ("first X enters, then Y
reacts"), **prominence order** when timing isn't the point (people before
objects, the largest/most-centered subject first, secondary subjects after).

## Shot composition: map the beat to the frame

For every shot, resolve before generating:

- **What the shot proves.** One shot, one idea -- match the visual change to
  the story beat it serves (see `video-storytelling`). A shot trying to
  prove two things usually proves neither clearly in a few seconds.
- **Framing**: wide/establishing (location context), full/long (subject
  head-to-toe with environment), medium (waist-up, balances detail and
  context), medium close-up (chest-up, conversational), close-up (face or
  key object, emphasizes emotion or a specific proof point), extreme
  close-up (one isolated detail). Choose the tightest framing that still
  reads clearly.
- **Composition**: rule-of-thirds subject placement, leading lines, negative
  space for text/overlay if the shot will carry on-screen copy, headroom
  appropriate to the framing.

## Camera vocabulary: precise, not evocative

Name the exact technique rather than describing an effect and hoping the
model infers it -- models respond far more reliably to named, correctly
grouped camera language, and conflating two different primitives routinely
produces the wrong result.

### Camera movement, grouped correctly

Models frequently confuse camera translation, rotation, and lens-only
changes when the prompt doesn't clearly separate them. Use the right group:

- **Translation** (the camera physically moves through space): dolly
  in/out (along the lens axis), truck left/right (laterally), pedestal
  up/down (vertically).
- **Rotation** (the camera stays in place and pivots): pan left/right
  (yaw), tilt up/down (pitch), roll clockwise/counter-clockwise (Dutch /
  Z-axis).
- **Lens-only** (neither the camera nor its position changes): zoom in/out
  (focal-length change), rack focus / pull focus / focus tracking
  (focal-plane change).
- **Hybrid / signature moves**: dolly zoom (the vertigo effect -- camera
  moves while the lens zooms to compensate, subject size stays constant
  while the background compresses or expands; a strong, specific effect,
  use only for genuine visual tension, not generic "cinematic" flavor),
  arc/orbit, crane/jib (vertical move, often combined with a pan -- reveals
  scale or transitions between a wide and a grounded view), whip pan,
  tracking/follow, handheld.
- **Stillness**: static (strictly zero movement, zero focus change, zero
  zoom -- if any of those occur, it isn't static, name the actual
  primitive instead), micro-shake, locked-off.

**dolly is not zoom** -- dolly is the camera physically moving; zoom is a
focal-length change with the camera fixed. **pan is not truck** -- pan
rotates in place; truck moves laterally. Models follow whichever word is
present, so using the wrong one produces the wrong shot, not a close
approximation.

### Camera height and angle

State both independently -- height is where the camera physically sits
(aerial, overhead, eye-level, hip-level, ground-level, water-level,
underwater); angle is the camera's relationship to the subject (bird's-eye
is strict top-down, not the same as merely "aerial" altitude; high angle
looks down on the subject; level angle matches subject height; low angle
looks up, reading as powerful/dominant; worm's-eye looks straight up; Dutch
angle tilts the horizon, fixed or rolling, for unease or tension).

### Focus and depth of field

Deep focus (everything sharp, foreground to background), shallow
depth-of-field (subject sharp, background soft), extremely shallow (a razor
focal plane), rack focus (a snap shift between two focal points mid-shot),
pull focus (the same shift, slower and more gradual than rack), focus
tracking (focus follows a moving subject). When focus changes during a
shot, state both the starting and ending focal plane.

### Playback speed

Time-lapse (much faster than real time), fast-motion (roughly 1-3x real
time), slow-motion, stop-motion (discrete frame-by-frame movement),
speed-ramp (a mix of fast and slow within one shot), time-reversed. These
are distinct primitives, not synonyms for "fast" or "slow."

## Lighting and atmosphere

State lighting direction as part of the prompt, not as an afterthought:

- **Key light direction and quality**: front, side, back, or top; hard
  (sharp shadows, high contrast, dramatic) or soft (diffused, low contrast,
  flattering). Three-point lighting (key, fill, rim/back) is the reliable
  default for a clear, well-modeled subject when the brief doesn't call for
  something more stylized. Rembrandt lighting (a small light triangle on
  the cheek) is a specific, recognizable portrait look; film noir (deep
  shadow, stark highlight) and volumetric (visible light rays through fog
  or dust) are others worth naming explicitly rather than approximating.
- **Color temperature and mood**: warm (golden hour, incandescent/tungsten)
  vs cool (overcast, moonlight, daylight/fluorescent) sets emotional
  register before a single word of copy appears on screen.
- **Practical vs motivated light**: whether light sources are visible in
  frame (practicals: a window, a lamp, a neon sign) or implied off-frame --
  affects how "real" vs "produced" a shot reads.
- Keep lighting continuity across shots in the same scene/location unless a
  time or mood change is deliberate and telegraphed (e.g. a transition beat
  explicitly moving from day to night). Never state conflicting lighting in
  one prompt ("bright noon" plus "dark dramatic shadows") -- the model
  picks one and silently drops the other.

## Describe the visual cause, not the emotion

Replace emotional or mood adjectives with the concrete visual detail that
would actually cause that emotion in a viewer -- "inspiring," "epic,"
"powerful," and "moody" don't constrain a single pixel, so models render
their own generic default for them:

| Instead of | Write |
|---|---|
| "sad character" | "tears on the cheek, shoulders slumped, staring at an empty chair" |
| "epic reveal" | "wide aerial pull-back; subject silhouetted against the rising sun" |
| "cinematic mood" | "low-key Rembrandt key light, shadows lifted two stops, 35mm anamorphic, crushed blacks" |
| "powerful music swell" | "music drops to silence at the cut, holds for 1.5s, returns with a low drum at half tempo" |

This applies to narration/on-screen copy as much as to generation prompts --
a viewer reads "epic" as filler; they respond to what's actually on screen.

## Subject transitions

When a shot introduces a new subject, removes one, or hands focus from one
subject to another, name the transition explicitly rather than leaving it
for the model to infer:

- **Revealing**: a new subject enters or becomes visible mid-shot, by
  either subject movement or camera movement (a door opens, the camera
  pans to find them, fog clears).
- **Disappearing**: an existing subject leaves frame or is removed (walks
  out, fades, is occluded).
- **Switching**: focus jumps from one subject to another, typically via a
  cut, a rack focus, or a camera whip.
- **Complex-alternating**: multiple subjects trade focus repeatedly within
  one shot (a debate, cross-cutting, ensemble action).

Name the mechanism too ("by camera movement," "via rack focus") -- this is
what actually unlocks reliable reveal-style camerawork rather than a random
guess at how the transition should look.

## Overlays are not scene depth

On-screen text, captions, titles, HUD elements, and watermarks are not part
of a scene's foreground/midground/background depth axis -- list them
separately with their exact content and placement, never as "overlay in the
foreground." Exact text, logos, and prices belong in a deterministic
overlay layer added in `video-editing`, not baked into the H3 generation
prompt -- models render text unreliably and exact copy belongs in a
deterministic layer.

## Define every recurring character before generating its first shot

This is the hardest part of a multi-shot AI-generated production and the
most common source of a viewer-visible defect (a character's face, outfit,
or a product's exact appearance shifting between shots). Do not discover a
character's appearance opportunistically shot by shot -- for every
character, presenter, or product that recurs across more than one shot,
produce both deliverables below **before generating any shot of that
subject**, not after an inconsistency is already noticed:

1. **A written character spec.** One canonical, detailed description --
   face, build, exact outfit, product's exact colors/markings -- saved once
   to `characters/<character-name>.md` and referenced from
   `production.json`. Pick 3-6 specific, disambiguating visual attributes
   (e.g. "bald, blue arrow tattoo on forehead, orange-and-yellow robes")
   and reuse that exact phrase, verbatim, in every prompt for that subject.
   Pronouns and phrases like "the same character as before" do not carry
   identity across independent generations -- each generation is evaluated
   cold, as if it had never seen the earlier one. Do not let the
   description drift by paraphrasing it differently per shot.
2. **The minimum sufficient approved identity reference.** Reuse a supplied or
   approved image before generating anything new. For a recurring human,
   prefer a face-only reference if face identity is what must persist;
   describe wardrobe, setting, action and framing in text. Other subjects may
   need markings, geometry or an exact costume visible in their reference.
   Follow `cinematic-visual-development` for local crops, conditional FLUX.2
   Max imagery and one representative complete-scene test. A reference is an
   identity target, not a guarantee; verify the resulting footage. Save the chosen
   reference as `characters/<character-name>.png` beside its spec.

Both live at `work/productions/<slug>/characters/`, namespaced per production
because one project can hold several. Keep the spec and its reference image
side by side under the same character name -- a spec whose reference image has
to be hunted for is a spec that gets paraphrased instead of reused.

Call `show_character` as soon as both exist and **before generating any
shot that uses them**. Every later shot is conditioned on that reference,
so an unapproved face propagates through the whole piece and can only be
undone by regenerating all of it -- this is the cheapest moment in the
production to be told the character is wrong.

Reuse the approved identity consistently through reference-image conditioning.
Keep identity descriptors stable while allowing the planned outfit, pose,
setting and camera changes. Do not pass a full posed image when a face crop
provides the needed identity with fewer competing composition cues.

## Keep the whole character arc on H3

Generate every video shot of a recurring subject through MiniMax H3's
Reference-to-Video route on fal.ai. Use the same approved character image and
canonical identity spec; add an accepted predecessor tail as Video 1 only
when the shot needs its motion or composition. New shots can use identity
alone with text-directed changes. Do not switch video providers or models mid-arc.

## Other consistency techniques

In addition to reference-image conditioning and the single-model/provider
default above:

- **Seed reuse where supported.** Some models expose a seed parameter that
  improves consistency across generations sharing it. This alone is usually
  not sufficient for a recognizable recurring character; combine it with the
  reference image, never use it as a substitute for one. Lock a seed once a
  shot's composition reads well, and reuse it for controlled variations of
  that same shot.
- **Shot-to-shot anchoring.** When an H3 continuation needs prior motion or composition, add an accepted
  predecessor tail as Video 1 alongside the approved identity reference. Write the
  two adjoining prompts so the final described moment of clip N matches the
  first described moment of clip N+1. Use an extracted stable boundary frame
  to inspect a failed direct cut and correct the Reference-to-Video successor
  prompt; do not use it to generate a third bridge clip.

**When consistency still fails**, inspect input conditioning conflicts and
record the observed failure before proposing another paid retry. Do not call
it a model-wide limitation from repeated attempts using the same conflicting
references. If the approved retry allowance is exhausted, report the remaining
issue and offer a concrete revision rather than hiding it in assembly.

## Prompt iteration strategy

1. Start simple: subject, action, setting. See what the model produces
   before adding constraints.
2. Add one element at a time -- camera, then lighting, then style. Adding
   everything at once makes it hard to tell which change caused a result to
   improve or misfire.
3. If a shot misfires, strip back rather than piling on more adjectives:
   simplify the action, freeze the camera, remove one variable at a time.
4. For consistency across clips in one sequence, repeat the same
   style/lighting/grade description alongside the character reference, not
   just the subject description.

## Verify before calling a shot done

After generating, check the result against what was asked for -- a
successful API response is not proof the shot is right:

- Does the framing/composition match what was specified, not a plausible
  alternative interpretation?
- Does the camera movement match what was named, or did the model default
  to static/generic motion?
- For a shot in an established sequence: does the subject's appearance
  actually match the reference/previous shots, checked by eye against the
  reference image or prior frame, not assumed from a matching prompt?

Re-prompt with more specific, technically-named direction (not just a
stronger adjective) when a shot misses -- most drift traces back to an
under-specified prompt rather than an unreliable model.

## What to avoid

| Don't | Why | Do instead |
|---|---|---|
| "Beautiful scene" | No visual information to render | "Wet cobblestone street, warm streetlamp glow reflecting in puddles" |
| "Person moves quickly" | No visible, specific action | "She sprints three steps and vaults over the railing" |
| Four or more simultaneous actions in one shot | Motion coherence tends to collapse | Split into a multi-shot sequence instead |
| Readable text or logos inside the generated clip | Text rendering is unreliable across models | Keep exact text in the overlay layer (see "Overlays are not scene depth") |
| Complex physics (explosions, crowds colliding) | Chaotic motion causes visible artifacts | Keep physics simple; walking/gesturing is reliable, destruction is risky |
| Multiple characters speaking in one shot | Multi-person dialogue sync tends to break | One speaker per shot, or use reaction shots instead |
| Everything specified at once on a first attempt | Hard to diagnose which element caused a miss | Layer in complexity one element at a time (see "Prompt iteration strategy") |

## Where this fits

- Use `longform-cinematic-video` to plan the shot list and own the overall brief.
- Use `video-storytelling` to place each beat in the narrative arc before
  writing its shot.
- Use this skill to turn the beat into the actual generation prompt --
  camera, lighting, consistency -- before calling H3 through `fal-ai`.
- Use `video-editing` to assemble the results, `video-quality` before
  presenting any version as complete.
