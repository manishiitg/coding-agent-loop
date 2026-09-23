# Chief of Staff as a standalone product

Status: **REMOVED from main on 2026-08-18** (commit `413d8d32a`, "Remove what was left of Chief of Staff"). Org Pulse is removed as well. Kept as history only; nothing below describes current behavior.

Previous status: shipped (MVP scope), as of 2026-08-16. This document was
originally written as a pre-implementation requirements draft; several of its
"Decided" calls changed shape once actual implementation started (most
notably the UI scope and the LLM-selection model — see the corrections
inline below each affected section, and "What actually shipped" at the
bottom). Treat sections marked **as-shipped** as the current source of truth
over the original plan prose above them.

## Why

Chief of Staff — the user's operations hub, the multi-agent chat that runs
automations, remembers what matters across them, and surfaces what needs
attention — is not a product today. It is a sub-mode of AgentWorks, defined
purely as an *absence*: multi-agent mode with no Agent Profile attached
(`isChiefOfStaffChat := isToolBackedChat && resolvedProfile == nil`,
`agent_go/cmd/server/server.go:5285`). No directory, no `product.yaml`, no
frontend surface. Video Studio, by contrast, is a real product: a declarative
manifest, a registered Agent Profile, its own frontend surface, its own tile
in the product switcher.

The goal is to give Chief of Staff that same standing — a third top-level
product, alongside AgentWorks (which becomes Automations-only) and Video
Studio — with its own genuinely new UI, not a re-parented chat window.

## Core purpose (the MVP, everything else is an enhancement on top)

Chief of Staff's main purpose going forward: **a chat interface with
read-only access to every workflow.** Not primarily a dashboard product —
the chat itself, backed by visibility across all of `Workflow/`, is the
thing that has to work first. The custom automations-oversight dashboard
(below) is a later enhancement on top of a working chat, not a blocker for
it.

This isn't new backend behavior to invent — it matches what's already true
today (the delegation prompt already says *"Read workflow files with shell
tools, but do not modify workflow internals from Chief of Staff chat"*,
backed by the existing `workflowReadOnlyFolders` grant). The global-scoped
profile (see Backend architecture) needs to preserve exactly this: read
access across all workflows, write access confined to `pulse/`/org-owned
artifacts and its own chat history — the same shape the no-profile path
already grants today, not a new permission model.

## Decided

- **Three top-level products.** Chief of Staff gets its own tile in the
  product switcher (`frontend/src/components/ProductSurfaceSwitcher.tsx`), a
  peer to AgentWorks and Video Studio, not nested under AgentWorks.
- **Explicit, not a fallback.** Selecting Chief of Staff is deliberate, the
  same way opening Video Studio is. It's no longer the silent default for a
  profile-less multi-agent chat — though existing chats/tabs created before
  this change must keep working unmodified (see Backend architecture below).
- **AgentWorks means Automations.** After the split, AgentWorks is the
  automation/workflow product and lands there by default, not on a chat.
- **Org goals feature dropped for now.** Chief of Staff's current
  goal-tracking/goal-alignment behavior (`pulse/goals.html`, the "Org Goals"
  view, workflow-vs-goal alignment reporting) is removed from scope, not
  carried into the new product. This resolves the goals backend question
  below — there is no new `OrgGoal` typed model or JSON API to build if the
  feature itself isn't shipping. Can be revisited as a later addition once
  the underlying Pulse-goals data migration (see Data-source findings) is
  worth doing on its own merits. Practically: `OrgGoalsPanel`
  (`OrgHtmlPanels.tsx`) is removed, not ported; the org-goals guidance
  references in the delegation system prompt
  (`delegation_tools.go:697,717,728`) and `org-goals.md`/parts of
  `org-html.md` become dead prompt content to prune, not functionality to
  preserve.
- ~~**Single pinned LLM, like Video Studio — no more High/Medium/Low
  tiers.**~~ **Superseded by what actually shipped, see below.** The
  Chief-of-Staff-*specific* tier override was removed, but "single pinned
  LLM like Video Studio" was NOT what got built — it went the opposite
  direction, toward *more* flexibility, not less:
  - **As-shipped:** `profile.runtime.provider`/`model_id` in
    `chiefofstaffproduct/product.yaml` is a *starting default for a
    brand-new chat only*, not an authoritative pin. Chief of Staff uses the
    same full published-LLM-catalog picker every multi-agent chat already
    has in `ChatInput.tsx` — because a resolved profile's pinned model used
    to unconditionally override whatever the user picked in chat (right for
    Video Studio's project-scoped profile, wrong for Chief of Staff's
    global one), `resolveAgentProfileForQuery`
    (`agent_profile_runtime.go`) now scope-gates this: still authoritative
    for a project-scoped profile, only a default for a global-scoped one
    when the request already carries an explicit selection. See
    `TestResolveAgentProfileForQueryGlobalScopeDefersToRequestedModel` and
    its sibling tests. `provider_options` (the mechanism that would curate
    and pin a small fixed list, as Video Studio uses it) is deliberately
    NOT declared for this profile — the opposite of what Chief of Staff
    wants.
  - `DelegationTierConfigModal.tsx` was **kept**, not deleted — it turned
    out to be shared infrastructure, not Chief-of-Staff-exclusive: the same
    `virtualtools.DelegationTierConfig` / `workflowtypes.PresetLLMConfig`
    data model also backs Pulse and Goal-Advisor (Maintenance) LLM routing
    for workflows. Its only *UI trigger* happened to live in
    Chief-of-Staff-only chrome (`ChatTabs.tsx`), which made it look
    CoS-scoped, but the modal itself isn't. Only the `chief_of_staff`
    slot/field was removed from the tier config; Main/High/Medium/Low/custom
    tiers and the modal stayed intact.
  - The `ChiefOfStaffLLM` field and
    `ResolveProviderProfileChiefOfStaffConfig` cascade
    (`pkg/workflowtypes/types.go`) — removed, confirmed dead once the
    CoS-specific tier slot was gone.
  - `llmConfigSourceScheduledChiefOfStaff` (`server.go`) — confirmed dead
    and removed: the built-in Org Pulse job (the only scheduled
    Chief-of-Staff run that needed it) was removed fully alongside goals
    (see "Scheduled tasks" below). Scheduled Chief of Staff runs now resolve
    their model the same way interactive chats do.
  - `reasoning_level` on `delegate(...)` was left alone — moot once tiers
    weren't collapsed to one pinned model.
- **`multi-agent` mode is untouched.** This is not a new mode. It is the
  shared execution substrate every product already runs on — Chief of Staff,
  the workflow-builder chat, and Video Studio all execute through
  `agent_mode: "multi-agent"` today (Agent Profiles are hard-rejected unless
  `agent_mode == "multi-agent"`, `agent_profile_runtime.go:156`). Chief of
  Staff becomes a *profile within* multi-agent mode — `agentProfileId:
  'chief-of-staff'` alongside the unchanged `mode: 'multi-agent'` — the exact
  relationship Video Studio already has. The name refers to the delegation
  capability (`delegate`/`query_agent`/`terminate_agent`/`list_agents`), not
  to Chief of Staff specifically; a fossil proves its age —
  `normalizeAgentMode` still maps the legacy value `"simple"` →
  `"multi-agent"` at the request boundary.
- ~~**A genuinely new UI, like Video Studio's**~~, with a bespoke home
  screen, custom domain-object panels, and inline presentation rendering —
  **this did not happen.** What actually shipped
  (`frontend/src/products/chief-of-staff/ChiefOfStaffSurface.tsx`) is much
  closer to a thin wrapper than the original plan intended, and
  deliberately so once the MVP framing ("Core purpose" above) was taken
  literally:
  - **No home screen, no project list.** There's exactly one singleton
    Chief-of-Staff conversation, not a list to choose from — the surface
    finds-or-creates that one tab and adopts a legacy no-profile tab in
    place if one already exists, rather than presenting a Video-Studio-style
    landing screen.
  - **Chat left, a tabbed utility aside right** — `ChatArea` directly
    (`fullTurnStreaming`, `showConversationUsage`) in a
    `grid-cols-[minmax(0,2fr)_minmax(0,3fr)]` layout (chat gets 40%), with an
    `<aside>` holding Tasks (always on), and Schedules / Secrets / Files
    panels shown when the profile's `ui_panels.{schedules,secrets,files}`
    say so. This mirrors Video Studio's own chat-left/panel-right shape at
    the layout level, but the panels themselves are **reused generic
    components**, not bespoke Chief-of-Staff domain UI:
    - Tasks → `ChiefTasksPanel` (`components/org/OrgHtmlPanels.tsx`), the
      same HTML-report iframe viewer that predates this product split.
    - Schedules → `MultiAgentSchedulesPopup`, given a new `embedded` prop so
      it renders inline instead of only as a modal — the same
      list/CRUD/enable-disable-trigger-delete component AgentWorks already
      had.
    - Files → `Workspace` (unscoped — no `scopedWorkspacePath`, since Chief
      of Staff has no single project), the same file browser AgentWorks'
      own multi-agent files view already used.
    - Secrets → `SecretSelectionDropdown` + `SecretsManagerModal`, the same
      controls Video Studio's header already offers.
  - **No custom domain objects got built.** None of "Domain concepts a
    custom UI could be built around" below (a live delegation roster, a
    structured goals view, a bespoke automations-oversight dashboard, a
    scheduled-work timeline) shipped — see "Deferred" at the bottom.
  - Every one of these panels is gated by a genuinely wire-exposed
    `agentprofiles.Profile.UIPanels` field (`Secrets`/`Schedules`/`Files`
    bools, serialized via `GET /api/agent-profiles/{id}` under
    `profile.ui_panels:`), *not* each product's older per-product-local
    `ui:` block (`files_panel`/`workflow_panel`/`secrets`/`streaming` in
    `product_config.go`) — that block was discovered to be **vestigial**:
    parsed for manifest-identity validation only, never read by any
    endpoint or frontend consumer. It's why `chiefofstaffproduct/
    product.yaml`'s own `ui.files_panel: false` looks stale next to a real
    Files panel that ships and works — that field is dead, `ui_panels.files:
    true` is what actually drives it.

  This is a real, acknowledged scope reduction from the original plan, not
  an oversight: chat access across every workflow was the stated MVP (see
  "Core purpose"), and reusing already-correct, already-tested generic
  components got that shipped with far less risk than designing bespoke
  domain UI up front. The bespoke-UI ambition isn't abandoned, just deferred
  — see "Deferred" at the bottom.

## Backend architecture (investigated, holds regardless of UI scope)

This part of the plan is independent of how much new frontend gets built, and
was verified against the live code:

- **Agent Profiles are project-shaped; Chief of Staff is global.** A resolved
  profile today collapses folder-guard grants to one project root and
  **drops the `pulse/` write grant** the profile-less path has
  (`server.go:4787-4810`, confirmed by reading the actual branch — its own
  comment says *"Do not inherit the Chief-of-Staff chat-wide grants"*). Naively
  giving Chief of Staff a Video-Studio-shaped profile would silently break its
  ability to write `pulse/goals.html`/`pulse/org-pulse.html`.
  **Resolution:** add a declared `scope: global | project` property to
  `agentprofiles.Profile` (default `project`, so Video Studio is untouched),
  and make the ~7 narrowing branches in `agent_profile_runtime.go`/`server.go`
  skip project-only requirements (folder, project title, secrets scoping) for
  global-scoped profiles.
- **Safe inversion via widening, not migration.** Legacy Chief-of-Staff tabs
  (no `agent_profile_id` sent) keep resolving `resolvedProfile == nil` exactly
  as today — unchanged code path. New explicit tabs resolve the real
  `chief-of-staff` profile and take the *same* branch via
  `isChiefOfStaffChat := isToolBackedChat && (resolvedProfile == nil ||
  isGlobalScopedProfile(resolvedProfile))`. No forced tab-metadata migration,
  no window where an in-flight chat breaks.
- New backend package `agent_go/internal/chiefofstaffproduct/`, mirroring
  `videoproduct/` structurally but much smaller: no per-project provisioning,
  no embedded skills.
- **The three CoS-only tools move to `profile.tools[]`/`ToolFactory`** —
  corrected from an earlier draft, which recommended leaving them manually
  registered in `server.go` since `newProductToolGate` gates them correctly
  regardless of registration path. True but beside the point: the actual
  requirement (see "Old Chief-of-Staff code in AgentWorks") is getting
  CoS-specific code out of `server.go`, not just correct gating. Checked and
  confirmed feasible: `ToolRuntimeContext` (`agentprofiles/types.go:216-225`)
  already carries `UserID`/`SessionID` — everything
  `registerActivityStatusTool(agent, currentUserID)`,
  `registerMultiAgentNotificationTool(agent, userID)`, and
  `registerWorkflowCreatorTool(agent)` need. Convert each to a `ToolFactory`
  (`func(ToolRuntimeContext, json.RawMessage) (ToolSpec, error)`), same
  pattern as `video.show-video`'s `showCharacterFactory`/
  `showDocumentFactory` in `internal/videoproduct/presentation_tools.go`,
  declared in `chiefofstaffproduct`'s `profile.tools[]`.
- **Delegation (`delegate`/`query_agent`/`terminate_agent`/`list_agents`)
  stays exactly where it is — it was never CoS-specific.** Verified:
  registered for ALL multi-agent chat (`if !isWorkflowPhase`,
  `server.go:4971`), not gated by `isChiefOfStaffChat`. Video Studio already
  gets this today. Nothing to move — already correctly platform-owned,
  generic infrastructure. Distinct from the *scheduled* background runs (Org
  Pulse firing as an async session), which genuinely is CoS-specific — see
  "Scheduled tasks" below.
- Keep the *dynamic* system-prompt assembly
  (`GetMultiAgentDelegationInstructionsWithUser` +
  per-request spawn capabilities + delegation-tier config +
  `AttachReferenceSurface`) rather than porting it into a static
  `product.yaml` prompt template — it depends on runtime parameters the
  static-template model can't express. Manifest's `prompt.file` is a short
  placeholder satisfying validation only.
- **As-shipped, two more real mechanisms not in the original plan:**
  - `profile.commands:` — Chief of Staff's own slash commands (`/notify`,
    `/org-backup`) moved out of the old `chiefOfStaffOnly`-gated entries in
    `frontend/src/commands/builtin-commands.tsx` into
    `chiefofstaffproduct/product.yaml`'s `commands:` section, each with its
    own prompt file under `commands/`, resolved by the existing
    `agentprofiles.ResolveCommandPrompts` — the same mechanism Video Studio
    already used, just newly reused here rather than invented.
    `/workflow-builder` was **not** migrated — deleted outright, since this
    profile is read-only over `Workflow/` (no `create_workflow` tool bound).
    This also made the old `chiefOfStaffOnly` command-filtering flag in
    `frontend/src/commands/types.ts`/`registry.ts` fully dead code, removed:
    `setProductCommands`'s existing per-surface scoping (only one product
    surface is ever mounted at a time) already did the job once
    Chief-of-Staff's own commands moved into that same dynamic mechanism.
  - `agentprofiles.Profile.UIPanels` (`Secrets`/`Schedules`/`Files` bools,
    `json:"ui_panels"`) — a new, genuinely wire-exposed field (serialized
    via `GET /api/agent-profiles/{id}`) letting a product declare which
    optional panels its surface should offer, read by the frontend instead
    of hardcoded. See "A genuinely new UI" above for why this exists
    alongside (and is distinct from) each product's older, vestigial
    per-product `ui:` block.

Full technical detail (exact file:line changes, the widened condition at every
call site, test plan) lives in the implementation plan at
`~/.claude/plans/iridescent-snuggling-starfish.md`; that plan's frontend
section is now superseded by the UI scope decision above and needs a rewrite
once the questions below are answered.

## Scheduled tasks: product.yaml needs a schedule concept it doesn't have

Chief of Staff keeps its own scheduled tasks — this isn't new. But today's
mechanism doesn't fit the product model at all: `DefaultBuiltinSchedules()`
(`agent_go/cmd/server/builtin_schedules.go`) is a hardcoded Go function
returning `[]WorkflowSchedule`, entirely separate from `agentprofiles`/
`product.yaml`. Video Studio's manifest has no schedule concept either —
nothing in the product system today lets a product *declare* a built-in
schedule; it's Chief-of-Staff-specific special-casing.

~~**Decided: add this as a real product.yaml capability**~~ — **not built.**
`DefaultBuiltinSchedules()` (`agent_go/cmd/server/builtin_schedules.go`)
stays a hardcoded Go function, unchanged in shape from before this
migration; it now simply returns `[]WorkflowSchedule{}` since Org Pulse (the
only builtin it ever returned) was removed. A generic `schedules:`
product.yaml section, mirroring `workflows:`, was never built — there was no
second product needing it yet to justify the generalization, and Chief of
Staff itself ships with zero built-in schedules, so the immediate need
disappeared along with Org Pulse. `chiefofstaffproduct/product.yaml` has a
comment recording this explicitly as a real, deliberately-not-yet-built gap,
distinct from the (separately shipped) `ui_panels.schedules` panel, which
just lists whatever a user has actually scheduled — it doesn't require a
product to declare any schedule of its own. Revisit if a second product ever
wants a built-in schedule.

> **Revisited 2026-09-03.** SparkQuill was that second product: its Pulse is
> a recurring check-in, i.e. a product schedule. `profile.schedules` now
> exists in `product.yaml` (`pkg/productschedule` for the definition,
> `cmd/server/product_schedules.go` for the platform runner). See
> `docs/workflow/workflow_scheduling.md`, "Product Schedules".

**Resolved and shipped: remove the built-in Org Pulse job fully**, not
rewrite or defer it. `builtinOrgPulseQuery`/`builtinOrgPulseMessages`
(`agent_go/cmd/server/builtin_schedules.go`) was goal-alignment-centric end
to end — every step read/wrote `pulse/goals.html`, compared workflows
against org goals. With goals dropped, there was nothing left to salvage by
rewriting it; it was removed entirely, consistent with the goals decision
above — `DefaultBuiltinSchedules()` now just returns an empty slice (see the
correction directly above: the `schedules:` product.yaml capability itself
was *not* built, so this isn't "ships with zero declared schedules for now"
so much as "the declaring mechanism doesn't exist yet at all").

## Not a reassembly of existing AgentWorks UI — reversed by what shipped

The rejected direction, as originally planned: relocating the existing
scattered `Org*` components (`OrgDashboard`, `OrgHtmlPanels`/
`OrgGoalsPanel`/`OrgPulsePanel`/`ChiefTasksPanel`, `OrgPulseControl`,
`OrgBackupPublishControls`, `WorkflowNotificationPopup`,
`MultiAgentSchedulesPopup`, `DelegationTierConfigModal`) into a new host
component — still AgentWorks UI, just re-parented, not a custom product.

**What actually shipped is much closer to that rejected direction than the
plan intended** (see "A genuinely new UI" above): `ChiefTasksPanel`,
`MultiAgentSchedulesPopup`, and the unscoped `Workspace` file browser are
literally reused as Chief of Staff's Tasks/Schedules/Files panels, not
reimplemented. This wasn't a change of heart on custom domain UI being
better — it's that the recurring investigation pattern below made "relocate
into a new host" and "reuse in place, unmodified" converge into nearly the
same outcome once it turned out most of these components were never
Chief-of-Staff-exclusive to begin with.

**Recurring pattern found during implementation — verify before deleting
"old Chief of Staff UI":** the original removal task list (below) was
written early, before deep investigation, and repeatedly assumed things were
Chief-of-Staff/goals/Org-Pulse-exclusive when they weren't:
`chief-task-report`/`ChiefTasksPanel`, `DelegationTierConfigModal.tsx`, and
`OrgDashboard.tsx`/`MultiAgentSchedulesPopup.tsx` all turned out to be
general-purpose infrastructure still needed elsewhere in AgentWorks. In each
case the component's only *UI trigger* happened to live in
Chief-of-Staff-only chrome, which made it look CoS-scoped, but the
underlying data/feature wasn't. See "Old Chief-of-Staff code must be
removed" below for the corrected, as-shipped disposition of each item.

The custom-domain-UI ambition (goals/automations-oversight/delegation
roster as purpose-built views, not reused generic components) isn't
abandoned — it's deferred, see the bottom of this document. **What can still
be reused** is the data/API layer underneath those components
(`agentApi.getBuilderDoc`, `schedulerApi.*`, the `pulse/*.html` read paths)
if and when that custom UI gets built.

### Domain concepts a custom UI could be built around

**None of these shipped.** The MVP UI reuses existing generic components
instead (see "A genuinely new UI" above). This section is preserved as-is
below as a starting point for the deferred custom-UI work, not as a record
of what exists today.

Video Studio's structure exists because video production has real, distinct
objects (a project, its videos, its characters, its documents). Chief of
Staff's domain has its own candidate objects, none of which have a
purpose-built visual today — they're either invisible (delegation) or
presented as raw agent-authored HTML (goals/pulse/tasks):

- **Delegations** — `delegate`/`query_agent`/`terminate_agent`/`list_agents`
  currently happen entirely inside chat text. A live roster of
  running/completed sub-agents, as first-class visual objects, doesn't exist
  anywhere today.
- **Goals** — currently an iframe of agent-authored `pulse/goals.html`.
  Could become a structured, purpose-designed goal-tracking view instead of
  rendered HTML.
- **Automations oversight** — health/goal/cost status per workflow (today:
  `OrgDashboard`'s HTML-card-scraping triage board) as a custom-designed
  status view.
- **Scheduled work / history** — recurring jobs and their run history (today:
  `MultiAgentSchedulesPopup`'s CRUD table) as a purpose-built timeline.

This list is a starting point for discussion, not a decision — see Open
questions.

### Data-source findings for the two chosen focus areas

Investigated whether real structured data exists to build custom UI on, or
whether new backend work is needed first (research only, no edits made):

**Automations oversight — real data already exists, no new backend work
needed.** Pulse has typed, SQLite-backed state (`PulseModuleState`,
`PulseFindingLifecycle`, `PulseGoalObservation`, `PulseImpactAssessment`,
`PulseAgentMetricRecord` — `agent_go/cmd/server/pulse_worklist.go`), already
served as JSON via real REST routes (`/workflow/pulse-module-state`,
`-findings`, `-reviews`, `-agent-metrics`, `-impact`, `-context`,
`server.go:2024-2029`), already has frontend TS types
(`services/api-types.ts:414-752`) and client calls
(`services/api.ts:1506-1556`), and already has a live precedent UI built on
exactly this data: `frontend/src/components/workflow/PulseWorkspace.tsx`
(972 lines), scoped to one workflow today. A custom automations-oversight
dashboard is "call the same per-workflow endpoints across all workflows and
aggregate" — proven pattern, not new plumbing.

**Bug found in passing, unrelated to this migration:** `OrgDashboard.tsx`
does not use any of the above. It reads `builder/card.health.html` /
`card.progress.html` / `card.cost.html` via `getBuilderDoc` — a pathway
nothing currently writes. The Pulse finalizer prompt explicitly forbids
writing them (*"Do not write a separate presentation artifact in this
turn"*, `guidance/templates/system/pulse-finalizer.md:5-6`), the only
remaining Go reference is a skipped test
(`t.Skip("Pulse no longer snapshots dashboard artifacts")`), and
`docs/workflow/org_dashboard_design.md` is marked historical. Today's org
dashboard is silently stale, independent of whether this product split
happens. Worth a separate fix regardless.

**Goals — no structured data exists; new backend work is required first.**
`pulse/goals.html` is freeform prose-in-HTML. A schema is *documented*
(`org-html.md`/`org-goals.md`: status, KPI targets, contributing workflows,
evidence — `org-html.md:240-337`) but unenforced — no Go struct, no DB table,
no JSON serialization, nothing validates an agent actually followed it on any
given write. A real (non-iframe) goals UI needs, at minimum: a typed
`OrgGoal`/`GoalTarget` struct, persistence, and new JSON read/write
endpoints — built by **extending** Pulse's existing
`PulseGoalObservation`/`PulseIntervention` types (already carry
`metric`/`value`/`status`), not a separate parallel model. This is not a new
pattern: module state, findings, reviews, agent metrics, and impact all
already made the same HTML-card → SQLite-backed-typed-state move. Goals is
the one piece of org-level state that never got that migration — this
finishes it rather than inventing a fifth approach.

## Old Chief-of-Staff code in AgentWorks — as-shipped disposition

The original plan (below, struck through per item) called for deleting most
of this outright. **What actually happened was much narrower**, per the
recurring pattern noted above: almost everything on the original "delete
entirely" list turned out to be general-purpose AgentWorks infrastructure
that Chief of Staff's *old* chrome merely triggered, not owned. Investigate
before deleting anything here further — don't trust the "delete entirely"
framing below at face value; it's kept as history, not as a live todo.

**Confirmed genuinely Chief-of-Staff-specific — removed:**
- `frontend/src/components/OrgPulseControl.tsx` — deleted (org-pulse
  feature dropped).
- `OrgGoalsPanel` / `OrgPulsePanel` inside
  `frontend/src/components/org/OrgHtmlPanels.tsx` — deleted (goals/pulse
  dropped). The file itself, and its `ChiefTasksPanel` export plus the
  shared `OrgHtmlPanel` primitive, were **not** deleted — see below.
- The "Chief of Staff" mode pill and its Ctrl+2 shortcut in
  `ModePresetBar.tsx`/`App.tsx` — deleted; this was the actual legacy
  affordance for switching AgentWorks itself into multi-agent mode.
- `ChiefOfStaffLLM`/tier-cascade backend code — see "Single pinned LLM"
  correction above.
- Org Goals / Org Pulse / org-publish end to end: backend routes, handlers,
  reference docs, prompt text; frontend slash commands, panels,
  `OrgPulseControl`, publish UI — all removed together as one pass.

**Turned out to be shared/general-purpose — kept, not deleted:**
- ~~`frontend/src/components/org/OrgDashboard.tsx`~~ **kept** — still used
  by `EmployeeDashboard.tsx`'s "Organization" sidebar section in AgentWorks'
  own Automations overview. Not goals/Org-Pulse-coupled; a working
  cross-workflow health/cost dashboard in its own right.
- ~~`ChiefTasksPanel` / the shared `OrgHtmlPanel` primitive~~ **kept, and
  actively reused** — this is Chief of Staff's own new Tasks panel (see "A
  genuinely new UI" above), not orphaned AgentWorks chrome. `App.tsx`'s
  legacy `multiAgentRightPanelView` toggle (below) also still renders it for
  old tabs.
- ~~`frontend/src/components/org/OrgBackupPublishControls.tsx`~~ **kept** —
  still used by `ChatTabs.tsx`'s legacy Chief-of-Staff toolbar (below).
- ~~`frontend/src/components/scheduler/MultiAgentSchedulesPopup.tsx`~~
  **kept, and actively reused** — general-purpose, not
  goals/Org-Pulse-coupled; gained a new `embedded` prop (skips modal
  chrome) so it can render inline in Chief of Staff's Schedules tab, while
  `ChatTabs.tsx` keeps using it as a modal for legacy tabs. Its dangling
  `/pulse-setup` reference for the now-stale `builtin-org-pulse` entry was
  fixed in passing (now just an ordinary toggleable built-in schedule
  entry, not special-cased).
- ~~`frontend/src/components/DelegationTierConfigModal.tsx`~~ **kept** —
  shared infra backing Pulse/Goal-Advisor LLM routing too; see "Single
  pinned LLM" correction above.
- ~~The "Organization" sidebar section inside `EmployeeDashboard.tsx`~~
  **kept** — it's `OrgDashboard`'s only mount point, and `OrgDashboard`
  itself is general-purpose (see above).
- ~~The multi-agent right-panel split-view logic in `App.tsx`~~ **kept
  deliberately, as backward-compat plumbing** — an already-open legacy
  multi-agent tab (no `agentProfileId`, i.e. created before this product
  split) still renders through `App.tsx`'s original
  `multiAgentRightPanelView` files/tasks toggle. A *new* tab opened via the
  product switcher takes the `productSurface === 'chief-of-staff'` branch
  (`App.tsx`) instead, bypassing this entirely — the same
  `productSurface === 'video-studio'` split the original plan called for,
  it just coexists with the old path rather than replacing it outright.
- ~~The Chief-of-Staff-specific toolbar wiring in `ChatTabs.tsx`~~ **kept
  deliberately, same reason** — serves an already-open legacy multi-agent
  tab; a new Chief-of-Staff tab never reaches `ChatTabs.tsx` at all.

**Generic/shared, unaffected either way:**
- `frontend/src/components/WorkflowNotificationPopup.tsx` — also used by
  `WorkflowToolbar.tsx` with `scopeKind="workflow"`; stays, loses its
  `scopeKind="chief-of-staff"` usage (moved to `OrgBackupPublishControls.tsx`
  via the /notify command, see product.yaml `commands:`).
- Backend Pulse/scheduler/notification machinery Chief of Staff uses but
  doesn't own (`scheduler.go`, `multiagent_notifications.go`,
  `report_human_inputs.go`) — unaffected, still serves other callers.

## Resolved questions (kept for history, not currently open)

1. ~~What does the home screen show?~~ **Resolved:** chat is the core
   surface (see "Core purpose"); the automations-oversight dashboard is an
   enhancement on top, not the primary screen. Goals dropped, so it's no
   longer a candidate home-screen section.
2. ~~Is chat primary or secondary?~~ **Resolved: primary.** "Mainly purpose
   of chief of staff can be a chat interface... like video studio, not
   agentworks" — `ChatArea`-direct, no right-panel split.
3. ~~Which scattered org components get absorbed?~~ **Resolved, then
   reversed by implementation: several, reused in place rather than
   removed.** The plan was "none — not a reassembly of existing AgentWorks
   UI," but `ChiefTasksPanel`, `MultiAgentSchedulesPopup`, and the unscoped
   `Workspace` file browser all ended up directly powering Chief of Staff's
   Tasks/Schedules/Files panels. See "Not a reassembly... — reversed by what
   shipped" and "Old Chief-of-Staff code... as-shipped disposition" above.
4. ~~Multi-tab or single view?~~ **Resolved: single, like Video Studio.**
   `ChatTabs.tsx` is bypassed entirely for Chief of Staff, same
   `productSurface`-level branch shape as Video Studio
   (`App.tsx:1243`) — confirmed Video Studio never imports `ChatTabs`.
5. ~~How does chat relate structurally to the dashboard?~~ **Superseded —
   there is no dashboard yet, and no separate screen at all.** The plan
   called for the dashboard as a home screen in front of chat, two-screen
   like Video Studio. What shipped instead: chat is the *only* screen — a
   single view, chat on the left and a tabbed utility aside (Tasks/
   Schedules/Secrets/Files) on the right, no home/workspace split, no
   project-list step, nothing to enter before you're chatting. The
   automations-oversight dashboard remains deferred (see bottom); if it
   ships later, this question should be revisited then, not assumed
   resolved by this answer.

## Deferred (deliberately, not forgotten — revisit later)

- **A real automations-oversight dashboard as Chief of Staff's home
  screen.** The single biggest deferred item from the original plan — see
  "A genuinely new UI" and "Domain concepts a custom UI could be built
  around" above. Chat is the MVP surface; this is a later enhancement on
  top, not a blocker (per "Core purpose"). `OrgDashboard.tsx` is flagged as
  a plausible starting point since it's already a working cross-workflow
  health/cost dashboard, reused elsewhere in AgentWorks. The "Data-source
  findings" above (automations-oversight data already exists via typed
  Pulse state; goals would need new backend work) are still valid research,
  not yet acted on.
- **A `schedules:` product.yaml capability** for a product to declare its
  own fixed built-in schedule *content* (like the old Org Pulse: a specific
  cron cadence + prompt shipped automatically for every user) — see
  "Scheduled tasks" correction above. `DefaultBuiltinSchedules()` stays
  hardcoded; revisit only if a real default schedule is wanted for some
  product. Investigated 2026-08-16: this is narrower than it first sounds.
  Whether the *agent* can create/update/delete/trigger its own
  user-created schedules (the actual live concern raised — "the agent
  should have the capability to add/update schedules") is **already fully
  solved, not deferred**: `create_multiagent_schedule`/
  `update_multiagent_schedule`/`delete_multiagent_schedule`/
  `list_multiagent_schedules`/`trigger_multiagent_schedule`/
  `get_multiagent_schedule_runs` (`cmd/server/multiagent_schedule_tools.go`)
  already register through `llmAgent.RegisterCustomToolWithTimeout`, the one
  chokepoint `productToolGate` (`product_tool_gate.go`) enforces — so a
  product opts out by declaring `tool_policy: {mode: allowlist}` without
  these six names in `enabled:`, or gets them for free (as Chief of Staff
  does) by leaving `tool_policy` unset. No new product.yaml field or code
  needed for that part. Only the "one product-declared default schedule
  ships automatically" idea remains actually unbuilt.
- **MCP servers via product.yaml.** No working precedent exists anywhere in
  the codebase (Video Studio's `dependencies.mcp_servers: []` is an unused
  placeholder; `resolveAgentProfileForQuery` never touches
  `EnabledServers`/`SelectedServers`). Genuinely new design needed, not
  urgent for the first version — `delegate(servers)` already covers
  per-sub-agent MCP scoping today, which is enough to ship without this.
- **The stale `OrgDashboard` bug** (reads dead `builder/card.*.html` files
  nothing writes anymore — see Data-source findings). Real, independent of
  this migration, but not blocking it. Revisit separately.

## TODO — raised 2026-08-16, not yet designed

Four follow-ups the user flagged in one pass. None investigated in depth yet
beyond the grounding note under each; treat these as a raw list to pick up
next, not decisions. **Scope note:** these are written from Chief of
Staff's vantage point since that's what this doc covers, but all four are
actually cross-product gaps (Video Studio has the same de facto MCP/browser
mechanism, the same generic LLM picker, and would hit the same
bot-connector routing question) — the user flagged this explicitly and does
not want that broader cross-product design work started yet, just noted.
When picked up, scope the investigation across every product, not only
Chief of Staff.

1. **A proper design for MCP server access**, not the current de facto
   mechanism. Today Chief of Staff gets MCP servers exactly like any
   multi-agent chat: a single global, persisted frontend store
   (`useMCPStore`'s `chatSelectedServers`, not per-product-surface) feeds
   `req.EnabledServers` on every request (`server.go:3072`), with zero
   profile/product-level involvement. `agentprofiles`' own
   `EnabledServers`/`SelectedServers` fields and product.yaml's
   `dependencies.mcp_servers: []` exist but are genuinely unused
   (`resolveAgentProfileForQuery` never reads them) — this is the same gap
   as "MCP servers via product.yaml" above, now explicitly re-raised as a
   TODO rather than just deferred.
2. **A proper design for LLM selection**, beyond the defer-to-request fix
   already shipped (see "Single pinned LLM" correction above: full
   published-catalog picker, profile's pinned model is only a starting
   default for a new chat). That fix solved the immediate
   pin-overriding-user-choice bug; whether the overall selection *design*
   (no per-profile default persistence beyond a brand-new chat, generic
   `ChatInput` picker with no Chief-of-Staff-specific curation) is actually
   right long-term is still open.
3. **Properly enabling browser access.** Same shape of gap as MCP:
   `req.BrowserMode`/`caps.BrowserMode` (`server.go:919-1097`) is a
   per-request field the frontend toggle in `ChatInput.tsx` sets (shared,
   so it works inside Chief of Staff's own chat input too), defaulting to
   `"none"` unless a user manually enables it each time. No product.yaml
   declaration lets a product default browser access on (or off) the way
   `ui_panels`/`tool_policy` do for other capabilities. `workflows:
   browser_mode` exists in product.yaml today but is a different thing —
   consumed only by Video Studio's own per-project workflow-manifest
   generation (`videoStudioWorkflowManifest`), not by interactive chat.
4. **WhatsApp/Slack.** These are real, already-shipped account-level bot
   connectors (`frontend/src/components/settings/BotConnectorModal.tsx`,
   backend `whatsapp_routes.go`) — interactive channels a user can chat with
   their assistant *through*, separate from the outbound notification
   webhooks already covered elsewhere in this doc. Not yet checked: whether
   an inbound WhatsApp/Slack message correctly resolves to the Chief of
   Staff profile (global scope, right persona/commands/tools) rather than
   falling back to the legacy profile-less path, and whether anything
   Chief-of-Staff-specific (its own commands, notification config) behaves
   differently when reached this way vs. the web UI.

## What actually shipped (summary)

- Core scaffolding: `agentprofiles.Profile.Scope = global`,
  `chiefofstaffproduct/` package, `ChiefOfStaffSurface.tsx`, widened
  `isChiefOfStaffChat` covering both legacy no-profile and new
  profile-resolved tabs, skills registered individually.
- Org Goals / Org Pulse / org-publish removed entirely, backend and
  frontend, as planned.
- Chief-of-Staff-specific LLM tier override removed; scheduled CoS runs
  resolve their model the same way interactive chats do. LLM *selection*
  itself went to a full published-LLM-catalog picker with the profile's
  pinned model as only a starting default — not the single-pin design
  originally planned (see "Single pinned LLM" correction above).
  `DelegationTierConfigModal.tsx` kept as shared infra.
- `/notify`/`/org-backup` moved into `product.yaml`'s `commands:`;
  `/workflow-builder` deleted outright, not migrated.
- UI: chat-left/tabbed-utility-aside-right (Tasks always on; Schedules,
  Secrets, Files gated by the new `agentprofiles.Profile.UIPanels`), built
  by reusing `ChiefTasksPanel`, `MultiAgentSchedulesPopup` (new `embedded`
  prop), and the unscoped `Workspace` file browser — not the bespoke
  domain-object UI originally planned (see "A genuinely new UI" and "Not a
  reassembly" corrections above).
- Old-AgentWorks-side cleanup was much narrower than planned: most
  components on the original "delete entirely" list turned out to be
  general-purpose infrastructure and were kept — see "Old Chief-of-Staff
  code — as-shipped disposition" above for the full corrected list.
- Remaining, deliberately deferred: the automations-oversight dashboard
  (this document's biggest open item), the `schedules:` product.yaml
  capability, MCP servers via product.yaml, and the stale `OrgDashboard`
  bug.

This document is the current source of truth for the actual state of the
Chief of Staff product split. The original implementation plan at
`~/.claude/plans/iridescent-snuggling-starfish.md` is now historical —
its backend section matches what shipped; its frontend section does not
(superseded by "A genuinely new UI" above) and should not be used to plan
further work without checking this document first.
