# Family Learning — Architecture (working draft)

**Status:** working draft · 2026-07-19 · branch `codex/family-learning-prd`
**Companion docs:** [`family-learning-prd.html`](family-learning-prd.html) · [`family-learning-design-guidelines.md`](family-learning-design-guidelines.md)

Legend: ✅ locked · ⚠️ risk / gap to design · ❓ open decision

---

## 1. Goals & constraints

- **Separate, isolated app** from AgentWorks, but **reuse ~90% of the existing backend** as libraries — not copies.
- **MVP-first**, desktop-first, **one child** per install, **file-first** source of truth.
- **Family Learning = an AgentWorks *workflow*, different UI.** Parent chat and child tutor are workflow-style sessions (MCP **bridge-only** tools, sandboxed workspace-api, FolderGuard, file-first folder). Backend reused, not rebuilt.
- Isolation must be **real**: a child scope cannot reach parent artifacts even if the frontend is bypassed. **§5 — inherited from the workflow bridge model (FolderGuard + workspace-api sandbox); the one guardrail is requiring a bridge-only engine for child sessions.**

---

## 2. Process & module topology ✅

```
Electron (own app id, config, data-root, updater, port)
├─ Frontend: frontend/learning-app        # isolated Vite app, own entry+port,
│                                          # reuses ../node_modules (no 2nd install)
└─ Backend: agent_go/cmd/family-server     # NEW binary — own port, SQLite, data-root, logs

Go workspace modules (all reachable via replace directives in agent_go/go.mod):
├─ agent_go        github.com/manishiitg/coding-agent-loop/agent_go   # server + orchestration
├─ workspace       github.com/manishiitg/coding-agent-loop/workspace  # OS isolation + file I/O service
├─ multi-llm-provider-go   (replace ../../)                           # tmux + coding-agent CLI stack
└─ mcpagent               (replace ../../)                            # coding-agent loop lib
```

**Clean, importable reuse units** (domain-agnostic, drop-in):
`pkg/fsutil`, `pkg/common` (session config), `pkg/workspace` (client + FolderGuard + `QueryWorkflowDB`), `pkg/schedulerstate`, `pkg/costledger`, `pkg/skills`, `workspace/security` (the `Isolator`), and the entire `multi-llm-provider-go` tmux stack.

**Entangled with `cmd/server` (must re-author, not import):** the `Workflow/<name>` layout + `workflow.json` manifest, backup/publish/schedule *execution* policy, and the workflow-phase allow/deny in `tool_setup.go`.

❓ **Topology decision:** OS isolation, file I/O, and per-folder DB queries currently round-trip through the **`workspace` HTTP service** (`fsutil.WorkspaceShellRoot()`, Docker `/app/workspace-docs`). family-server can either (a) **import `workspace/security` + do local file I/O directly** (leaner, no second service — recommended for an isolated desktop app), or (b) **stand up the workspace-api service** and talk to it like agent_go does. Recommend (a).

---

## 2A. Frontend — reuse & presentation rules ✅

**Reuse:** the isolated `frontend/learning-app` (Sunlit shell, built) + AgentWorks' hierarchical **file-tree** (`PlannerFile`, `processHierarchicalFiles()`, `buildFileIndex()` in `utils/fileUtils.ts`) to render the workspace — `parent` / `child` / `shared` folders + artifacts — inside the right drawer / assets.

**Hard presentation rules (both parent and child surfaces):**
- **No terminal, ever.** No raw provider/tmux output, no logs, no shell scrollback in any surface.
- Transcript = **user messages + assistant messages only.**
- **Very selective tool calls:** surface only a *whitelisted* set as friendly result cards — e.g. Subject & Topic saved, worksheet/test created, material added & classified, attempt graded, plan updated. All other tool activity (file reads, shell ops, bridge chatter) stays hidden.
- The workspace is shown via the **tree view**, not a terminal.

**Backend implication:** family-server streams a **curated event stream** to the UI — assistant text + whitelisted tool-result events — not the raw session/terminal stream. Reuse the conversation transport, filter server-side to the whitelist. The bridge-only model makes this clean: every tool call already passes through the bridge, so the server chooses which become UI cards.

---

## 3. Engine detection & validation — Screen 1 ✅

**Decision:** extract `agent_go/internal/enginedetect`; both servers import it. Coupling verdict **EASY** — all 13 detect/validate functions are already plain functions; the two HTTP handlers read **no** `StreamingAPI` field. Cut line `providerAuthConfigured(provider, keys)` already takes keys as a param → `enginedetect` stays key-source-agnostic; family-server passes **env-only** keys.

**Move in:** `buildLLMDiscovery`, `validateProviderConfig`, `validate{ClaudeCode,Codex,Cursor,Pi}CLI`, `providerRuntime`, `runtimeAvailableForProvider`, `providerRuntimeAvailable`, `providerAuthConfigured`, `providerUsable`, `discoverySetupHint`, `getSupportedProviders`, tables `supportedLLMProviders`/`providerStaticInfoMap`, discovery DTOs.
**Leave behind:** `MergedProviderAPIKeys` (workspace key store), `allProviderModelMetadata`/`buildProviderCapabilities` (full catalog).

| Method | Path (proposed) | Reuses | New |
| --- | --- | --- | --- |
| GET | `/api/engines` | `enginedetect.BuildDiscovery(ctx)`, filtered to `coding_agent && !deprecated` | friendly status map |
| POST | `/api/engines/validate` | `enginedetect.Validate(req)` — real adapter, 90s test prompt, temp dir | — |
| PUT | `/api/engine/selection` | — | persist selected engine (family SQLite) |

**Ports:** AgentWorks server = `8000`; **family-server = `8010`** (own process — restart freely, never touches AgentWorks); frontend `learning-app` = `5174`.
**Slice-1 implementation note:** `enginedetect` + `cmd/family-server` are **new files only** — a faithful copy of the leaf detect/validate logic, reusing the external `mcpagent/llm` + `multi-llm-provider-go` primitives; **no edits to `cmd/server`** → zero risk to AgentWorks. Converging `cmd/server` onto `enginedetect` (removing the small duplication) is a follow-up, not a blocker.

---

## 4. Workspace / filesystem model ✅

**Root:** `fsutil.WorkspaceDocsRoot()` (`agent_go/pkg/fsutil/atomic.go:34`) → `workspace-docs/` (env `WORKSPACE_DOCS_PATH`). Existing layout we mirror:

```
workspace-docs/
├─ _system/costs.sqlite, schedule-state.sqlite     # CENTRAL sqlite (cost ledger, scheduler state)
├─ _users/<id>/  Chats/, chat_history/, *.json      # per-user config + schedules
└─ Workflow/<name>/                                 # a "workflow folder" (server-domain)
   ├─ workflow.json                                 # manifest
   ├─ planning/plan.json, db/db.sqlite              # PER-FOLDER sqlite
   ├─ runs/iteration-N/group-N/                     # artifacts
   └─ backup/status.json, publish/status.json
   skills/<name>/SKILL.md                           # skills
```

**Family layout (proposed — we own a new top-level prefix; `Workflow/<name>` naming is server-domain):**
```
workspace-docs/Family/
├─ parent/   notes, drafts, answer keys, plans, db/db.sqlite      (parent scope)
├─ child/    tutor transcripts, attempts, approved material, db/db.sqlite
└─ shared/   material promoted "Ready for Maya", academic-map HTML
```

- **Per-folder SQLite: it's BOTH.** Central path-injectable stores `pkg/schedulerstate` + `pkg/costledger` are clean and reusable. Per-folder `<path>/db/db.sqlite` is queried **read-only via workspace-api** (`pkg/workspace/query_workflow_db.go:12`, generic over any path). → Family uses a **central family SQLite** (identity/permissions/index) + optional per-folder db per the workflow pattern.
- **Agent working-dir scoping:** `pkg/common` session config (`SessionShellConfig`) + `pkg/workspace` FolderGuard + `pkg/agentwrapper.CodingAgentWorkingDir`, all keyed by `sessionID` and **generic**. Set via `workspace.SetSessionWorkingDir` / `SetSessionFolderGuard` (read/write path lists). ⚠️ `sessionShellConfigs` is a **process-global map** (fine within one binary); the rich allow/deny in `cmd/server/tool_setup.go` is workflow-phase-specific → **re-author** a small family policy (parent scope → `Family/parent`+`Family/shared`; child scope → `Family/child`+`Family/shared` read-only).

---

## 5. Isolation & permissions ✅ (inherited from the workflow bridge model)

**Model:** Family Learning runs like an AgentWorks **workflow** — the coding agent uses **MCP bridge-only tools**, not its own native `Bash`/`Read`/`Write`. So **all** file & shell I/O flows through `execute_shell_command` → the **workspace-api**, which runs every op under a dynamic **Seatbelt/`sandbox-exec`** profile generated from the session's **FolderGuard** (`workspace/security/isolator.go`, applied in `workspace/handlers/shell.go`).

**How the sandbox works** (already in production for workflows):
- **macOS:** per-op Seatbelt profile → `sandbox-exec -f /tmp/sandbox-*.sb sh -c <cmd>`; `(allow default)` then **deny the project root** and re-allow only the FolderGuard read/write subpaths; symlinks canonicalized (`/var`→`/private`).
- **Linux:** `unshare -m` + tmpfs overlay hides everything, bind-mounts read paths ro / write paths rw. Env secrets scrubbed.

**So isolation is inherited, not invented** — set the FolderGuard per scope and the OS sandbox enforces it, same user or not:
- **Parent session** → read/write `Family/parent` + `Family/shared`.
- **Child session** → read/write `Family/child`, `Family/shared` read-only, **`Family/parent` denied** (not in the profile → invisible to the kernel).

This resolves the earlier worry: that concern assumed the agent could use *native* tools that bypass the sandbox; the bridge-only model removes native tools, so there is no unsandboxed file path.

**The one guardrail** ✅ (verified): all four engines — Claude Code, Codex, Cursor, Pi — declare `UsesMCPBridge: true` + `SupportsBridgeOnlyTools: true` (`multi-llm-provider-go/coding_agent_contract.go:185–362`), and `coding_agent_certification.go` carries **real certification tests** proving bridge-only mode disables built-in shell/file tools (e.g. Codex `--no-builtin-tools`; Pi/Agy deny-hooks; *"Claude Code bridge-only mode does not expose internal shell/file tools"*). So the requirement is **not** "pick a special engine" — it's: **family-server must launch every session (parent and child) with the hard lever `WithBridgeOnlyTools(true)`**. With that set, all agent I/O routes through the sandboxed bridge + FolderGuard on any of the four engines. Isolation is sound.

**PIN verifier:** mirror the existing `0600`/`0700` secure-file pattern (used today for OAuth tokens) or add real keychain. ❓ open.

---

## 6. tmux infrastructure reuse ✅ / ⚠️

| Subsystem | Verdict | Where |
| --- | --- | --- |
| **tmux sessions + CLI launch** | ✅ reuse directly | `multi-llm-provider-go` adapters + `tmuxstartup/capture/input/size`, `tmuxlaunch` — generic shared module. agent_go wrappers (`internal/terminals/store.go`, `terminalleases`, `cmd/server/terminal_*.go`) are usable but tangled with terminal-UI event streaming → **fork/trim**. |
| **Backup** | ⚠️ pattern, not drop-in | Zip export `workspace/handlers/backup.go` is generic. Policy is manifest-config + status.json + **agent-driven** → re-author for family (maps to PRD export/archive). |
| **Schedule** | ✅ engine / ⚠️ binding | `pkg/schedulerstate` (SQLite state machine, central `_system/schedule-state.sqlite`) clean + reusable. `cmd/server/scheduler.go` execution is tied to `ScheduleContext` + workflow manifest → rework (maps to PRD study plans / reminders). |
| **Publish** | ⚠️ different meaning | `workflow_publish.go` = deploy HTML artifacts to a **public URL**. Family's "Approve for Maya" is an **internal** parent→child promotion, not public publish — reuse the status-model pattern, not the deploy. |
| **Skills** | ✅ reuse directly | `pkg/skills` is self-contained over a `skills/<name>/SKILL.md` convention → `llmtypes.Skill` (`runtime_loader.go`). Directly reusable for the family skills: Academic Map, Tutor, Progress, Study Material, Test Creator, Intake. |

---

## 7. Data ownership rule ✅ (from PRD)

- **SQLite (family db):** identity, permissions, schedules, operational state, audit, file/version indexes.
- **Filesystem artifacts:** originals, extracted content, attempts, transcripts, summaries, academic-map HTML, generated views, exports.
- **tmux/provider state:** replaceable operational state — **never** the source of truth.

---

## 8. Screen-by-screen backend reuse map

| Screen | Backend need | Reuse | New |
| --- | --- | --- | --- |
| 01 Connect engine | detect + validate | `enginedetect` ✅ | status map, persist selection |
| 02 Add child | child + workspace root | `fsutil` + folder create + FolderGuard ✅ | deterministic create endpoint, family SQLite row, `Family/child` scaffold |
| Parent chat | provider session, files, Subject&Topic | `multi-llm-provider-go` sessions + `pkg/workspace` + `pkg/skills` ✅ | Subject&Topic op, parent skills |
| Handoff / PIN | scope + isolation | FolderGuard + `workspace/security` sandbox ✅ (all I/O via bridge) | PIN verifier, sign-off, require bridge-only engine for child (§5) |
| Child tutor | scoped session, hints skill | sessions + `pkg/skills` ✅ | tutor skill, child-safe facade, child FolderGuard (deny `Family/parent`), curated event stream |
| Upload (03B) | intake, classify, organize | upload/extraction + `pkg/skills` ✅ | intake skill, visibility rules |
| Schedules/reminders | scheduler | `pkg/schedulerstate` ✅ | family schedule binding, parent-only WhatsApp routing |

---

## 9. Open decisions ❓

1. ~~Child-session engine gate~~ **RESOLVED (§5):** all 4 engines are certified bridge-only. Locked requirement: family-server launches **every** family session with `WithBridgeOnlyTools(true)`; FolderGuard denies cross-scope. No provider restriction needed.
2. ✅ **LOCKED — workspace-api topology (§2):** family-server imports `workspace/security` + does local file I/O (no second service).
3. ✅ **LOCKED — PIN verifier storage:** mirror the existing `0600`/`0700` secure-file pattern for the pilot (keychain later).
4. ✅ **LOCKED — Family SQLite:** one central family db + optional per-folder `db/db.sqlite` like workflows.

---

## 11. Agent-runtime lifecycle & AgentWorks reuse ✅ / ⚠️

**Gold standard (locked):** the AgentWorks **chief-of-staff chat** and **automations / workflows** in `mcp-agent-builder-go` are the reference SparkQuill replicates and reuses (see the PRD "Runtime source of truth" callout). Chief-of-staff chat → the Parent Learning chat (same runtime for Child tutor + WhatsApp); automations/workflows → agentic, file-system-based **skills**. **Where SparkQuill diverges from how AgentWorks does it, that is a bug to fix, not a new design** — reuse before inventing.

**Session & bridge lifecycle ✅ (LOCKED — verified against AgentWorks):**
- **One process-global executor / MCP bridge**, started once and shared by every conversation and skill run — mirrors AgentWorks, whose bridge is the main server's own route set wired once at startup (`mcp-agent-builder-go/.../server.go:1556`, env `:1490`, single listener `:2010`). In family-server this is `agentsession.ensureSharedBridge` (sync.Once); it is **never torn down per turn**.
- **Per-turn `Session`** (`New` + `defer Close` in every handler). `Close()` disposes **only** the per-turn agent — `mcpagent.Agent.Close()` explicitly leaves the interactive (tmux) session alive (*"connections persist in session registry"*, `mcpagent/agent/agent.go:3148`). So warm resume is owned by the **provider's owner registry** keyed by `SessionID`, reaped on an **idle timeout (~3h default)** — **no LRU, no size cap**, matching AgentWorks (`multi-llm-provider-go/.../owner_registry.go`, `codingtimeout/policy.go`).
- ⚠️ **Anti-pattern (fixed):** an earlier family-server build stood up a **fresh bridge on a random port per HTTP request** and closed it each turn — the resumed CLI then called the previous turn's dead port (`connection refused` / clipped reply). This is exactly the deviation the gold-standard rule forbids; the shared-bridge refactor removed it.
- `/api/reset` closes warm tmux via the provider's owner-scoped close (`agentsession.CloseAllInteractiveSessions`); absent that the provider reaps on idle anyway.

**What we reuse from AgentWorks (inventory):** the entire agent stack is imported, not reimplemented — `mcpagent` (agent, executor/MCP bridge, session registry, custom tools, code-execution mode), `multi-llm-provider-go` (all coding CLIs + persistent interactive tmux sessions + owner registry + idle reaper + tmux-control), `coding-agent-loop/workspace` (sandbox/isolator), plus the patterns in §2–§8 (bridge-only isolation, skills, file-first, tree view). The refactor above was *"stop deviating and adopt AgentWorks' shape."*

**Adoption backlog — proven in AgentWorks, not yet in SparkQuill (reuse, do not rebuild):**

| AgentWorks capability | Priority | SparkQuill today | Plan |
| --- | --- | --- | --- |
| **Streaming responses** (progress over SSE) | **DEFERRED** | blocking `Ask` (spinner only) | ⚠️ **Investigated 2026-07-20 — not a clean reuse in our mode.** The `mcpagent.AddEventListener` seam works, but in **warm-resume (tmux) mode** the provider emits **no clean token deltas and no `ToolCall*` events** — only raw **terminal pane snapshots** (`StreamingChunkEvent kind="terminal"`, which §2A forbids showing) plus token/cost telemetry (`StreamingStatusLineEvent`). AgentWorks can token-stream because it *also* renders a terminal panel; SparkQuill deliberately doesn't. The only honest option is **descriptive phase status emitted from our own bridge tool handlers** (e.g. read_image → "Reading the image…"), a SparkQuill-specific adaptation, not an AgentWorks reuse. Deprioritized — a spinner is acceptable for MVP. |
| **Periodic tmux orphan reaper** (2-min ticker) | MED | rely on provider idle-TTL (~3h) only | Port `coding_agent_tmux_reaper.go` cleanup tick; cheap robustness net beyond idle timeout. |
| **Rate-limit watchdog** (30s; frees CLIs parked on a provider rate-limit wall) | MED | none | Reuse `coding_tmux_watchdog.go`; avoids silent hangs. |
| **FolderGuard on the coding agent's own file access** | MED | `security.Isolator` for Child mode (§5) | Extend to parent answer-key isolation via the same FolderGuard lever. |

Streaming was investigated and **deferred** (see the table note): clean token-streaming isn't available in warm-resume/tmux mode without exposing the terminal, so it's not a straight AgentWorks reuse. The remaining items (reaper, watchdog, parent FolderGuard) stay reuse-first.

## 10. Change log

- 2026-07-19 — Draft. Locked topology (§2), `enginedetect` for Screen 1 (§3), data ownership (§7).
- 2026-07-19 — Investigation folded in: workspace model (§4), isolation mechanism (§5), tmux/backup/schedule/publish/skills reuse verdicts (§6), reuse map (§8).
- 2026-07-19 — **Correction (bridge-only model):** Family Learning runs as an AgentWorks *workflow* — agent uses MCP **bridge-only** tools, so all I/O flows through the sandboxed workspace-api + FolderGuard. The earlier "unsandboxed interactive agent" gap does **not** apply; isolation is inherited. New single guardrail: child sessions must use a `supports_bridge_only_tools` engine (§5, §9.1).
- 2026-07-19 — Added §2A frontend rules: reuse the AgentWorks tree view; **no terminal**; transcript = user + assistant only; **whitelisted** tool-results as cards; backend streams a curated event stream.
- 2026-07-19 — Verified in code: all 4 engines declare `SupportsBridgeOnlyTools: true` with a real certification suite. §5 guardrail resolved → §9.1 closed; the rule is simply "set `WithBridgeOnlyTools(true)` on every session."
- 2026-07-20 — Added §11 (agent-runtime lifecycle & AgentWorks reuse). Locked the AgentWorks chief-of-staff / automations gold standard and the session/bridge lifecycle: **one process-global bridge, per-turn Session, warm resume via the provider owner registry + idle reaper, no LRU/cap** — verified against `mcp-agent-builder-go` and shipped as the `ensureSharedBridge` refactor (removed the per-request-bridge anti-pattern). Recorded the adoption backlog; **streaming** is HIGH and to be **reused** (AgentWorks `StreamingAPI` + event listeners), realizing §2A.
