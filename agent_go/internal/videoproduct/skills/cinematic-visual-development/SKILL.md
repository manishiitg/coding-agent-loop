---
name: cinematic-visual-development
description: Choose the minimum visual references needed for a generated scene, test a representative complete shot, and add FLUX.2 Max references only for required identity, exact details, or demonstrated continuity failures. Use before an anchor or follow-up clip.
---

# Give the model the goal and only the necessary visual anchors

Plan what the viewer must see and what must remain consistent. Separate
**must preserve**, **may change**, and **model discretion** in the shot contract.
Text direction is a valid starting point for clothing, locations, action,
lighting and camera. An image is needed when it provides a specific control,
not merely because a subject or location appears in the script.

Read `minimax-h3-video` for route controls and request handling, and
`multi-clip-cinematic-generation` for the editorial handoff. Verify the live
schema before using an input; endpoint support does not make that input necessary.

## Start with the minimum sufficient references

| Subject type | Starting reference when consistency is needed | Add detail when required |
| --- | --- | --- |
| Human / presenter | One approved face-only identity reference | Exact costume, body details, profile fidelity or a demonstrated identity miss |
| Animal / pet | One image showing identifying markings and silhouette | A hidden side or marking that must match |
| Mascot / creature | One approved identity/style view | Required silhouette, material or geometry not shown there |
| Product / object | A view showing the required geometry and branding | Exact unseen details, scale or multiple required views |
| Vehicle / robot | One identifying view | Required shape, markings or action-specific detail |
| Environment | Text scene direction | Exact return geography, brand/period evidence or a demonstrated location miss |

These are starting strategies, not universal asset counts. Match the approved
visual style, including illustration and animation. A face-only reference
preserves an identity target; it does not prove body, wardrobe or voice fidelity.
If no visual identity or exact asset must persist, a prompt-only scene can
have an empty image-reference list. Honor user-supplied assets and explicit
requests for exact compositions or full reference packs.

Reuse an approved image or derive a local crop when it isolates the needed
identity. Preserve the original; record the parent and crop bounds, inspect
the crop and present it before footage consumes it. A crop does not require
another paid image generation.

## Prove the complete scene before expanding assets

Select one representative shot that exercises the difficult requirements
(such as identity, walking, off-center framing, visible setting and dialogue).
Within the approved budget, generate it as a complete scene with the smallest
reference set and clear text direction. Show it with `show_video` and obtain
the review outcome before expanding production.

Do not generate full-body/profile/expression packs, location plates, sequence
start/exit images, or separate green-screen performances by default. Add them
when the brief explicitly needs that control or the representative result
shows a specific gap. Compositing remains valid for an intentional layered
look or exact deterministic content; disclose its extra generation and editing
cost before changing a complete-scene plan to separate layers.

## Diagnose conditioning before another paid retry

A reference intended for identity can also influence pose, framing, clothing
and background. A predecessor video can carry unwanted composition or motion.
Inspect the actual inputs alongside the failed output. Compare their visible
cues with the requested change; stronger wording may preserve the conflict.

Within the existing retry allowance, change one meaningful factor: isolate a
face, omit an unnecessary location image, or remove a tail from a shot that
needs a new composition. Preserve genuinely required identity and handoff
constraints. Record the hypothesis, changed input and observed outcome. Stop
at the approved limit. Several failures under the same conditioning do not
prove a model-wide limitation; label local findings as observations.

Verify voice continuity by listening to adjacent clips. Identity images and
“same voice” text do not guarantee it. Use supported audio conditioning or an
approved off-camera voiceover strategy when needed, and report what remains
unverified rather than reintroducing a visually conflicting tail automatically.

## Generate additional imagery only when needed

Video Studio uses Fal's **FLUX.2 Max** for paid reference imagery:
- `fal-ai/flux-2-max` for a new master;
- `fal-ai/flux-2-max/edit` for controlled derivatives of approved masters.

Read `fal-ai` and the selected live endpoint schema and pricing before spending.
Present a bounded cost and retry allowance. Reuse approved masters instead of
regenerating identity from scratch. Use `show_character` for the identity and
`show_reference` for other actual reference images; get approval before footage
uses them. Do not substitute another image model without explicit approval.

## Record the decision and actual inputs

Write the required `*-reference-manifest.json`, even when no images are needed.
Record essential invariants, text-directed details, model discretion, and the
reason for including or omitting each proposed asset. For actual media record:
- exact path, title, subject/sequence and one semantic role;
- approval/review evidence and source provenance;
- generated endpoint/input or local crop parent/bounds;
- consuming shots and `endpoint_input: true` only for live-supported inputs;
- `editorial_target: true` for review-only assets that are not sent to H3.

A direct continuation can use an accepted predecessor tail when its state is
needed. A later shot can instead reuse identity alone with text-directed scene,
wardrobe and motion. Keep accepted predecessor frames for review even when they
are not provider inputs. Block only a missing/unapproved reference actually
required by the selected shot contract, not an unused optional asset.

After generation, perform the lightweight receipt and show the candidate.
Retain the accepted reference strategy and observed result for later shots,
including unresolved identity, wardrobe, motion or voice issues.
