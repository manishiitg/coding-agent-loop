# SparkQuill design guidelines

Use this guide for every SparkQuill React screen, agent-generated HTML view, and future design skill. It is the shared visual and interaction contract for the desktop app.

## 0. Brand foundation

- **Product name:** SparkQuill — always written as one word with a capital S and Q.
- **Guide identity:** Quill may be the friendly name of the learning guide inside the product.
- **Working tagline:** Learning that grows with you.
- **Brand idea:** a small spark of understanding becomes a learning trail that the child, parent, and guide can continue together.
- Keep the magical-school feeling subtle. A spark, quill stroke, page, or trail is appropriate; wizard hats, wands, owls, crests, and direct franchise references are not.
- **Working logo direction:** a simple navy quill rising toward a yellow spark, with a restrained coral point and a navy SparkQuill wordmark.
- **Horizontal logo:** `family-learning-assets/sparkquill-logo-working-concept.png` for onboarding and other wide brand surfaces.
- **Compact product mark:** `family-learning-assets/sparkquill-compact-mark-working.png` for the chat rail, compact headers, and loading states.
- **Electron app icon:** `family-learning-assets/sparkquill-electron-icon-working.png` for development builds and desktop identification. Its cream-on-navy treatment is an approved high-contrast adaptation of the same mark.
- **LLM loading animation:** `family-learning-assets/sparkquill-loader.svg` for meaningful agent waits. The quill writes a short line and the spark appears; reduced-motion users receive the static final frame.
- Treat all three rasters as a direction, not production artwork. Rebuild one consistent source as SVG, refine the typography, simplify small details, test at 16px through 1024px, and export platform-specific Electron assets from that source before shipping.
- Existing `--fl-*` token names and `family-learning-*` filenames remain temporarily as internal implementation names. Do not expose them as the customer brand.
- Earlier concept images may still contain the previous working label. Use them only for layout and interaction reference until branded screens are regenerated.

## 1. Product feeling

SparkQuill should feel warm, clear, and capable.

- A child should feel they are entering a friendly learning space, not an AI tool or school administration system.
- A parent should feel informed and in control, without needing to configure a complex system.
- Prefer one useful next step over a dense dashboard.
- Keep technical language, provider names, file paths, logs, model settings, and agent internals out of child-facing screens.

## 2. Core rule: agent-led, not CRUD-led

The parent normally tells the agent what they want in natural language. The agent proposes or creates a result; the parent reviews and approves it for the child.

Do not create manual add/edit/delete screens for learning content, plans, progress, materials, or tests.

Direct UI controls are reserved for:

- first-time child setup;
- Parent PIN and access boundaries;
- approving a child-visible result;
- privacy, export, deletion, and other safety-critical settings.

Subject and Topic remain structured facts, but the parent does not manage them through a form. The agent confirms and maintains them through the protected Subject & Topic tool while the conversation stays primary.

## 3. Visual language: SparkQuill Sunlit Canvas

Use a bold, playful learning-focused style with deep navy structure, sunlit yellow primary actions, coral moments of encouragement, and soft blue/mint content panels.

| Token | Value | Use |
| --- | --- | --- |
| `--fl-navy` | `#0B1D42` | Headings, primary text, selected states |
| `--fl-ink` | `#10224A` | Main body text |
| `--fl-yellow` | `#FFC91B` | Primary action, selected parent action |
| `--fl-coral` | `#FF6F61` | Attention, small warm accents |
| `--fl-cream` | `#FFFDF8` | Page background |
| `--fl-blue-soft` | `#EEF7FF` | Evidence and information panels |
| `--fl-mint-soft` | `#EFFBF4` | Positive/reassuring panels |
| `--fl-line` | `#DCE4EF` | Borders and dividers |

Use soft shadows sparingly. Rounded cards should generally use a 16–22px radius. Avoid glass effects, gradients as the main structure, tiny dense controls, or a dark technical-console appearance.

## 4. Typography and hierarchy

- Use one familiar humanist system sans stack for the main product UI. Avoid making Inter the visible personality of the product.
- The left conversation/history rail uses the same humanist system sans as the rest of the UI, for one clean, consistent voice (decision, 2026-07-19 — the rounded nav stack was tried and dropped). The warmer rounded system stack (`ui-rounded, "SF Pro Rounded", "Avenir Next", system-ui, sans-serif`) is optional and reserved only for the small SparkQuill wordmark, never for rail navigation text.
- Headings are dark navy, bold, and compact. Keep letter spacing no tighter than `-0.04em` and balance intentional line breaks.
- Use a fixed product type scale: `0.75rem` caption, `0.875rem` secondary UI, `1rem` body, `1.25rem` subheading, `2.875rem` or `4.25rem` screen heading depending on window size.
- Main explanatory copy is at least `1rem` with a `1.5–1.65` line height. Compact navigation and metadata may use `0.875rem`.
- The numbered onboarding label is allowed because onboarding is a real sequence. Write it in sentence case and do not repeat tiny uppercase tracked eyebrows as decoration elsewhere.
- Never use an unexplained score as the primary headline. Prefer evidence and a recommended next action.

## 5. Layout

Desktop is the initial target.

- Do not use a persistent sidebar for the two-screen onboarding. Use a light top header with the brand and a quiet `Step 1 of 2` / `Step 2 of 2` indicator.
- The top header is the only setup navigation area. Name the current step, show its position in the two-step sequence, and show **Back** there on step 2. Do not repeat Back inside the page content.
- After setup, the app becomes a full-height three-column operating shell inspired by modern AI chat workspaces: a narrow left conversation/history rail, the flexible central agent conversation, and a collapsible right assets/evidence drawer.
- Do not add a separate full-width page header or large hero heading inside the operating shell. Back, mode, conversation title, and rail controls live in the left rail or the compact local chat toolbar.
- The left rail organizes the current child, new conversation, recent parent conversations, and recent child sessions. It is not feature navigation and should not become a dashboard menu.
- The central chat is always the largest surface. Keep the transcript at a comfortable reading measure, keep the composer visible at the bottom, and let the chat expand when either rail closes.
- The right drawer contains current context, school sources, generated work, plans, review items, evidence, and visibility state. It supports the conversation and must not compete with it.
- Keep one primary action visible at a time. A parent should never have to search for “what next?”.
- On small windows, stack content; hide decorative elements before hiding useful text or actions.

## 6. Reusable screen patterns

### Onboarding

Onboarding has exactly two screens: choose the learning engine, then add one child. Use a simple progress indicator instead of a sidebar. After the child is created, enter the real Parent Learning conversation. Subject and Topic appear only as a compact tool confirmation. Prototype screens should read like the product and must not display backend, fixture, or implementation disclaimers inside the interface.

Both setup screens use one constant frame:

- the same centred `940px` maximum content width;
- the same left and right content edges;
- a stable heading and supporting-copy region;
- the primary action anchored to the same bottom-right position;
- status or reassurance text anchored to the same bottom-left position;
- a minimum-height content column so shorter screens do not move the action upward;
- button labels may vary in width, but their right edge must remain fixed;
- on smaller windows, release the fixed vertical position and stack the footer without hiding the action.
- keep Back and setup progress in the shared header so they do not shift the content frame.

This consistency is functional, not decorative: a parent should be able to move through setup without visually searching for the next button on every screen.

### Parent Learning Guide

The conversation is central. Keep current Subject and Topic in a compact contextual view. Offer **Understand progress**, **Create study material**, and **Create a test** as suggested prompts that fill or start the conversation—not as separate dashboard destinations. Keep a visible **Open child learning space** handoff.

On a wide desktop window, use a full-height application frame. The left conversation rail is approximately `220–260px`, the centre conversation is flexible, and the open right assets drawer is approximately `320–380px`. Both side rails can collapse, with their width returned to the chat. The composer spans the usable centre column and stays visible. On smaller windows, close the right drawer by default and allow the left rail to collapse.

Generated material should appear as a preview, not a complicated editor. The parent can approve it or tell the agent what to revise.

### LLM loading state

Use the SparkQuill loader when the agent is performing visible work such as reading uploaded material, interpreting handwriting, analysing progress, grading an attempt, or generating a learning asset.

- Delay its appearance by about `400ms` so quick responses do not flash a loader.
- Always pair it with concise, task-specific live status text. Prefer “Reading Maya’s worksheet” to “Thinking” or “Loading”.
- Update the message only when the underlying agent phase changes; do not rotate decorative phrases or show invented progress percentages.
- After about eight seconds, acknowledge the longer wait and offer a safe **Stop** action without discarding already-saved input.
- Keep the loader compact inside the central conversation. It must not replace the transcript or block access to existing assets.
- Announce status changes through an appropriate polite live region. The animation itself is decorative once equivalent status text is present.
- Respect `prefers-reduced-motion`; the supplied SVG automatically becomes a static quill, writing line, ink dot, and spark.

### Child learning space

The tutor conversation is central. Use encouraging language, hints before answers, readable evidence/source references, and one clear next action. Never show Parent Mode drafts, answer guides, private notes, or technical controls.

## 7. Parent and child modes

Parent Mode is clearly marked and protected by a PIN. Child Mode is calm and direct.

- Parent Mode has one persistent handoff control: **Open child learning space**.
- Handoff clears parent-only UI state before showing the child view.
- Returning to Parent Mode always asks for the PIN.
- Do not rely on colour alone to distinguish modes; use a visible label and distinct navigation context.

## 8. States and feedback

- Loading: explain what is being prepared in plain language.
- Empty: say what is not known yet and offer the smallest useful next action.
- Uncertain: say what is unclear and what would help; never guess handwriting or school context.
- Error: preserve parent input and give a simple retry path. Never expose raw provider or terminal output.
- Success: use a small check, clear confirmation, and an obvious next step—not a celebratory modal for every action.

## 9. Accessibility baseline

- Meet a 4.5:1 contrast ratio for normal text.
- Keep visible keyboard focus states.
- Use real buttons, labels, and form controls.
- Give every icon a text label or accessible name.
- Do not make colour the only signal for status, confidence, or parent/child mode.
- Support keyboard navigation throughout onboarding and the parent-to-child handoff.

## 10. Implementation checklist

Before adding a screen or generated HTML view, check:

- Is the purpose understandable in five seconds?
- Does it have one primary next action?
- Is it agent-led instead of a manual management interface?
- Is parent-private content excluded from child views?
- Does it use the Sunlit palette and rounded-card hierarchy consistently?
- Does it have loading, empty, uncertain, and error states where relevant?
- Does it work in a smaller desktop window without horizontal overflow?
- Does the typography use the documented role rather than a new arbitrary size or weight?

## 11. Current references

- Parent Guide screen: `family-learning-assets/parent-learning-guide-screen.png`
- Tutor conversation concept: `family-learning-assets/chat-centered-conversation-hub.png`
- Parent conversation and saved-context concept: `family-learning-assets/academic-map-option-1-live-split.png`
- Chat shell — three-rail workspace: `family-learning-assets/chat-shell-three-rail-workspace.png`
- Chat shell — chat-first studio: `family-learning-assets/chat-shell-chat-first-studio.png`
- Chat shell — evidence desk: `family-learning-assets/chat-shell-evidence-desk.png`
- Upload — drop and discuss: `family-learning-assets/upload-drop-and-discuss.png`
- Upload — visual intake review: `family-learning-assets/upload-visual-intake-review.png`
- Upload — guided capture conversation: `family-learning-assets/upload-guided-capture-conversation.png`

Treat these as visual direction, not pixel-perfect specifications. The working React screens are the source of truth as the product becomes real.

## 12. Impeccable workflow

Impeccable is installed locally in this worktree at `.github/skills/impeccable/`. Use it as a refinement vocabulary around this guide, not as permission to replace the Sunlit identity.

- `/impeccable typeset frontend/src/learning` for typography and hierarchy.
- `/impeccable layout frontend/src/learning` for spacing and alignment.
- `/impeccable polish frontend/src/learning` for a final interaction and consistency pass.
- `/impeccable audit frontend/src/learning` before a release-quality handoff.

When we are ready to formalize product context, run `/impeccable init` to create portable `PRODUCT.md` and `DESIGN.md` context files. Until then, this file and the current React implementation remain the source of truth.
