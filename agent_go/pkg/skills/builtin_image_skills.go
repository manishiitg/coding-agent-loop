package skills

import (
	"fmt"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const imagePromptingSkillContent = `# Image prompting — image_gen and image_edit

Read this before calling ` + "`image_gen`" + ` or ` + "`image_edit`" + `. Neither tool has a
structured control for quality, exact size or background — their ` + "`quality`" + `/
` + "`size`" + `/` + "`background`" + ` arguments are folded straight into the prompt as plain
text (see each tool's own description) and honored on a best-effort basis by
whatever model actually runs. A vague prompt gets a vague, generic result far
more often than a bad model does; writing the prompt well is most of the job.

## The shape of a good prompt

Four things, in this order — most prompts that come out wrong are missing
one of these, not badly worded:

1. **Name the subject and what it's for.** A hero image, a labelled diagram,
   an icon, a character reacting to something — say which, don't make the
   model guess from a vague description alone.
2. **Composition.** What's in frame, roughly where it sits, what the aspect
   ratio should be (use the ` + "`aspect_ratio`" + ` argument for this rather than
   describing it in words — it's the one thing that IS a structured control).
3. **Visual details that actually matter.** Materials, lighting, colour
   palette, style ("flat colourful illustration", "soft watercolour",
   "cartoon", "photorealistic" — not just "nice" or "colourful", which
   describe nothing the model can act on). Say "photorealistic" explicitly
   when a real-looking photo is genuinely wanted; otherwise favour a clearly
   illustrated style so it doesn't read as an uncanny fake photo.
4. **People or characters, specifically.** If a figure appears, say what
   it's doing and how: "reaching toward the console, focused expression"
   beats "a person looking at something." Vague action reads as a stiff,
   generic pose.

Exact text that must appear on the image (a label, a sign, a logo's words)
goes in quotes, placed and sized: ` + "`the word \"Mars\" in bold white letters across the top`" + `.
Spell anything unusual letter-by-letter if it keeps getting garbled, and
double-check the result — text legibility is the single most common failure.

## Editing (image_edit) needs the opposite instinct

For ` + "`image_gen`" + ` you're building a scene up from nothing. For ` + "`image_edit`" + `
you're doing the reverse: **say what changes, then list everything that must
NOT.** "Add a small red backpack on the character's back — keep the face,
pose, the background and the lighting exactly as they are" gets a targeted
edit. "Add a backpack" alone is a coin flip on whether anything else about
the image quietly changes too. Restate what must stay the same on every
edit, even a second or third pass on the same image — it does not carry
over from the previous instruction.

## Which tool

- **` + "`image_gen`" + `** — making something from nothing: an illustration, a scene,
  an icon, a stylised cover image.
- **` + "`image_edit`" + `** — revising an image that already exists: one a user
  supplied, one you made, or the output of a previous edit.
- **Neither**, when a real photo, chart of real data, or geometric figure is
  needed — fetch or render those instead of generating a stand-in; a
  generated image of something that's supposed to be real (a person, a
  place, live data) reads as fake and can mislead.

## One generation per idea

Don't loop chasing perfection. If the first result is usable, use it —
each retry costs another generation for a marginal gain, and a materially
different result usually needs a materially different prompt, not a
repeat of the same one.
`

func init() {
	err := RegisterBuiltin(&llmtypes.Skill{
		Name:        "image-prompting",
		Description: "How to write an image_gen/image_edit prompt that actually gets what you asked for — prompt structure, the generate-vs-edit distinction, and editing technique (state what changes, then list what must stay the same).",
		Content:     imagePromptingSkillContent,
		Source:      llmtypes.SkillSource{Origin: "builtin"},
	})
	if err != nil {
		panic(fmt.Sprintf("register built-in image-prompting skill: %v", err))
	}
}
