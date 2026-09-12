# Fal MiniMax H3 Max prompting reference

Source: Fal's [MiniMax H3 Prompting Guide](https://fal.ai/learn/devs/minimax-h3-prompting-guide), consulted 2026-09-01. This is an operational summary, not a replacement for the selected route's current schema or `llms.txt`.

## Video Studio operating strategy

The minimal-reference strategy in `cinematic-visual-development` comes from
local production feedback, not a guarantee in the provider documentation. In
one reviewed production, a face-only image plus text-directed clothing, setting
and walking succeeded after posed references and prior-shot video had failed.
Use this as a starting hypothesis; exact products, costumes, geography or direct
continuations may need richer inputs. Test a representative complete scene
before preparing a full pack. Keep required details explicit and leave
incidental detail to the model.

## Apply these rules to every paid H3 Max prompt

1. **Assign every reference exactly one job.** Name it by input order and say
   whether it controls identity, wardrobe, location, composition, camera
   motion, performance, voice, sound, or a successor handoff. Do not attach a
   reference without a stated role.
2. **Use a timed shot list when the clip has more than one beat.** Divide a
   5–15-second clip into simple time blocks. Each block should say what changes
   in picture, performance, camera, and sound. A one-beat static shot can use a
   single `[0–N seconds]` block.
3. **Direct sound as deliberately as picture.** State speaker, exact language
   and dialogue, lip-sync requirement, voice/performance character, ambience,
   foley, music, and silence. Specify excluded sounds such as music,
   voice-over, other speakers, captions, or subtitles when relevant.
4. **State specific negatives.** H3 responds well to clear prohibitions. Name
   the actual failure to prevent: unwanted body parts in a true-POV shot,
   reframing, zooming, camera drift, text, watermarks, a hard cut, a morph,
   artefacts, or a genre/style shift. Do not use vague negatives such as
   "avoid bad quality."
5. **Lock identity and invariants explicitly.** List the visible features that
   must remain unchanged, such as face or an exact prop. Separate them from
   intentional wardrobe, set, motion and framing changes. Connect an invariant
   to a reference only when that media is actually needed.
6. **For an edit or continuation, name the change and the constraint together.**
   For example: "Reyes begins speaking; preserve Video 1's final eye-level
   composition, location, wardrobe, and distance." Never say merely
   "continue" or "make it seamless."
7. **Use precise film language.** State shot scale, camera height and side,
   lens character only when helpful, movement or explicitly no movement,
   focus, lighting, exposure behavior, pace, and framing. Literal human-eye
   POV should be described as a human-eye viewpoint, not mixed with an
   unrelated stylised lens instruction.
8. **Describe a transition as a physical event.** For a motivated change of
   angle or scene, describe the action and visual handoff; never rely on labels
   such as "cinematic transition." For a locked static continuation, explicitly
   say that no transition or reframing occurs.

## Suggested prompt order

Use this order unless the live route guide requires another syntax:

1. Reference ledger (`Image 1` / `Video 1` / `Audio 1` and each role).
2. Delivery contract (duration, aspect ratio, realism/style).
3. Time-coded picture and performance beats.
4. Camera and continuity constraints.
5. Native audio contract.
6. Explicit exclusions and invariants.

## Static true-POV dialogue pattern

```text
Image 1 defines [subject identity and wardrobe]. Image 2 defines [location].
Video 1 defines [the immediate predecessor's exact accepted state].

[0–N seconds] True first-person human-eye POV through [viewer]. [Viewer] is
never visible: no hands, arms, shoulders, reflection, shadow, or silhouette.
[Subject] is [distance] away at exact eye level, [framing]. The camera remains
static: no pan, tilt, zoom, dolly, reframing, or drift. [Subject performance].

Native [language] lip-synced dialogue from [speaker], exact words: "...".
Sound: [ambience]. No music, subtitles, captions, narration, or other voices.
Preserve [identity, wardrobe, location, lighting, screen direction, framing].
```

## Pre-submit check

- A reviewed `shot-contracts/<shot-id>.md` distinguishes must preserve, may
  change and model discretion, with essential scene/action/camera/audio direction.
- The contract names a shot purpose, measurable success criterion, ranked
  conflict priority, approved retry limit, and acceptance checks.
- Duration is one of the selected endpoint's supported values and matches the
  user's explicit constraint.
- Spoken words fit naturally inside the requested seconds; do not silently
  rewrite user-approved dialogue.
- Every reference has one named role and no conflicting jobs.
- One dominant action is requested per time block.
- When a predecessor tail is needed, name it Video 1 and state what transfers.
  Identity-only Reference-to-Video does not require a predecessor video.
- The request records endpoint, input, and exact prompt before it is submitted.
