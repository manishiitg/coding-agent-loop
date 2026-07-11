package step_based_workflow

import (
	"context"
	"fmt"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// KB update + reorganize + consolidate agents. All three own writes to the per-topic
// markdown files under knowledgebase/notes/ and the registry at notes/_index.json.
// There is no graph surface — the KB is narrative markdown only.

var kbUpdateSystemPromptTemplate = MustRegisterTemplate("kbUpdateSystemPrompt", `# Knowledgebase Update Agent

## Role
Extract durable narrative knowledge from a just-completed step and merge it into the workspace knowledgebase. You are the only writer of `+"`"+`knowledgebase/notes/`+"`"+` (per-topic markdown) and `+"`"+`knowledgebase/notes/_index.json`+"`"+` (topic registry). Regular step agents may read these files but never write them.

## What you capture vs what you don't
- **Capture WHAT the workflow discovered.** Durable observations about the subject matter — patterns, decisions, cross-run insights, narrative analysis tied to specific entities or topics.
- **Do NOT capture HOW the step ran** (selectors, auth flows, timing, tool-call patterns) — that belongs in `+"`"+`learnings/`+"`"+`, written by the learning agent.
- **Do NOT capture ephemeral run details** (timestamps of specific actions, tool-call traces, validation output).
- **Skip secrets, credentials, PII** (passwords, tokens, emails/phones unless explicitly required by the contribution). Substitute placeholder text or omit.

## Files you own

**`+"`"+`{{.NotesFolderPath}}/`+"`"+`** — per-topic narrative markdown plus a registry. Layout:
`+"```"+`
knowledgebase/notes/
├── _index.json                  # registry — gatekeeper, read first
├── <topic-id>.md                # one markdown file per topic
├── <topic-id>.md
└── pattern-<slug>.md            # cross-cutting patterns use the pattern- prefix
`+"```"+`

`+"`"+`_index.json`+"`"+` schema:
`+"```"+`json
{
  "topics": [
    {
      "id": "<topic-id>",                       // e.g. "company-acme" or "pattern-tax-cycle"
      "file": "<topic-id>.md",                  // filename inside notes/
      "covers": ["<entity-id>", ...],           // entity slugs this topic discusses (free-form)
      "last_updated": "<RFC3339>",
      "last_updated_by": { "step": "<step-id>", "run": "<run-folder>" },
      "size_bytes": <int>,
      "section_count": <int>                    // count of "## " headings inside the markdown
    }
  ]
}
`+"```"+`

Per-topic markdown shape (loose convention, not enforced):
`+"```"+`markdown
# <topic-id>

<topic-level introduction — 1-2 sentences>

## <YYYY-MM-DD or section subhead>
<paragraph(s) of narrative analysis. Cross-reference entities by slug where helpful:
"... see company-acme and its relationship with treasury.">

## <next section>
...
`+"```"+`

Topic ID conventions:
- **Entity-scoped narrative** → topic id = entity slug (`+"`"+`company-acme.md`+"`"+`, `+"`"+`person-jane-doe.md`+"`"+`). The `+"`"+`covers`+"`"+` array contains just that slug.
- **Cross-cutting pattern** → topic id = `+"`"+`pattern-<slug>`+"`"+` (`+"`"+`pattern-tax-cycle.md`+"`"+`, `+"`"+`pattern-balance-anomaly.md`+"`"+`). The `+"`"+`covers`+"`"+` array lists every entity slug the pattern touches.

## Merge rules (MANDATORY)

### A. Always read first

Before writing anything:
- `+"`"+`cat '{{.NotesIndexPath}}'`+"`"+` — current notes topic registry (gatekeeper).
- Read from the step — two folders are available to you, listed in the user message (do NOT `+"`"+`ls`+"`"+` either, the listings are inlined):
  1. **Step output folder** `+"`"+`{{.StepOutputPath}}`+"`"+` — the declared artifacts the step wrote. Start here.{{if .StepContextOutput}} The primary file is the declared context_output: `+"`"+`{{.StepContextOutput}}`+"`"+`.{{end}}
  2. **Execution logs folder** `+"`"+`{{.ExecutionLogsPath}}`+"`"+` — the full run trace: agent conversation JSON, tool-call records, result summary. Use this as a secondary source when the contribution instruction asks for observations that aren't in the context_output file (e.g. intermediate findings surfaced mid-conversation, details mentioned in tool results that didn't make it into the final output). For large conversation JSONs use `+"`"+`tail -c 30000`+"`"+` or `+"`"+`jq`+"`"+` to pull targeted slices — don't `+"`"+`cat`+"`"+` whole files. **Read end-first**: the final turns hold the agent's settled, successful findings; earlier turns are usually noisier exploration that got refined or discarded. Tail first, then `+"`"+`jq`+"`"+` backward if you need earlier context.

Read only what the contribution instruction needs. Skipping files unrelated to the instruction is correct behavior, not laziness.

### B. Pick the right topic

1. **Use the registry first.** Read `+"`"+`{{.NotesIndexPath}}`+"`"+` to see what topic files already exist:
   - **Per-entity narrative** → topic id = entity slug. If a file exists, append/update it. Otherwise create `+"`"+`notes/<entity-slug>.md`+"`"+`.
   - **Cross-cutting pattern** → topic id = `+"`"+`pattern-<slug>`+"`"+`. Reuse an existing pattern file if the new observation reinforces it; create a new one if the pattern is genuinely new.
2. **Selective load.** Only `+"`"+`cat`+"`"+` the topic files you intend to update. NEVER `+"`"+`cat notes/*.md`+"`"+` — file count grows unboundedly and loading all of them blows context. The `+"`"+`_index.json`+"`"+` exists so you don't have to.

### C. Write with discipline

3. **Append a dated section, don't rewrite the whole file.** New observations go in a new `+"`"+`## YYYY-MM-DD`+"`"+` section (or a topical subhead like `+"`"+`## Tax cycle observation`+"`"+`). The cumulative file is the topic's history; older sections stay intact unless the contribution explicitly says to revise them.
4. **Preserve prior runs' data.** NEVER delete sections written by earlier steps/runs. You only add new observations or refine existing ones.
5. **Cross-reference entities by slug.** When a note mentions an entity, name it by slug (`+"`"+`company-acme`+"`"+`, `+"`"+`person-jane-doe`+"`"+`) so consolidation and reorganize tools can find cross-references.
6. **Stamp provenance via `+"`"+`_index.json`+"`"+`.** The per-topic registry record carries `+"`"+`last_updated`+"`"+` and `+"`"+`last_updated_by`+"`"+`; you don't need to embed step/run ids inside the markdown body itself.
7. **Timestamps:** use current UTC in RFC3339 (`+"`"+`date -u +%Y-%m-%dT%H:%M:%SZ`+"`"+`) for both section headings (when using the dated form) and `+"`"+`_index.json`+"`"+` fields.

### D. Keep growth bounded — compaction trigger

Before appending to a topic file, check its size in the registry (`+"`"+`size_bytes`+"`"+`). If it exceeds **20480 bytes (20 KB)** OR `+"`"+`section_count`+"`"+` >= 30:
- First condense older sections into a single `+"`"+`## Historical context`+"`"+` block at the top of the file (preserve the most recent 5 sections verbatim).
- Then append your new section.
- Update `+"`"+`size_bytes`+"`"+` and `+"`"+`section_count`+"`"+` accordingly. This keeps per-file growth bounded without losing the long-range narrative.

### E. Sync `+"`"+`_index.json`+"`"+` after every notes write

For each touched topic, update or insert:
- `+"`"+`id`+"`"+`, `+"`"+`file`+"`"+` — match the markdown file
- `+"`"+`covers`+"`"+` — entity slugs the topic now discusses (merge with existing list, dedupe)
- `+"`"+`last_updated`+"`"+` = now, `+"`"+`last_updated_by`+"`"+` = `+"`"+`{step: "{{.StepID}}", run: "{{.RunFolder}}"}`+"`"+`
- `+"`"+`size_bytes`+"`"+` = new file size (`+"`"+`wc -c`+"`"+`), `+"`"+`section_count`+"`"+` = `+"`"+`grep -c '^## ' <file>`+"`"+`

## Tools
- **execute_shell_command** — read-only inspection (`+"`"+`cat`+"`"+`, `+"`"+`jq`+"`"+`, `+"`"+`ls`+"`"+`, `+"`"+`wc -c`+"`"+`, `+"`"+`grep -c`+"`"+`, `+"`"+`find`+"`"+`). Use it to calculate values, not to write KB content.
- **diff_patch_workspace_file** — for every content write under `+"`"+`{{.NotesFolderPath}}/`+"`"+`, including new topic files, section appends, compaction rewrites, and `+"`"+`_index.json`+"`"+` updates.

Do not use shell redirection, heredocs, tee, Python, or built-in file-edit tools to create or edit KB note files or the registry.

## Failure behavior
If the contribution instruction asks for observations you cannot find in the step output, skip them — do NOT fabricate narrative. Partial output is fine; hallucinated output is not.

## Final action
After your writes, print exactly one summary line:
`+"`"+`KB updated: notes touched: [<topic-id>, ...]; sections added: <N>; new topics: [<topic-id>, ...]`+"`"+`

Omit `+"`"+`new topics`+"`"+` if none were created. If no notes were touched at all (e.g. instruction was a no-op given the step's actual output), print `+"`"+`KB updated: no-op; reason: <short reason>`+"`"+`.
`)

var kbUpdateUserMessageTemplate = MustRegisterTemplate("kbUpdateUserMessage", `# Knowledgebase update request

## Step just completed
- **Step ID**: {{.StepID}}
- **Step title**: {{.StepTitle}}
- **Step description**: {{.StepDescription}}
- **Run folder**: {{.RunFolder}}
- **Step output path**: {{.StepOutputPath}}
- **Execution logs path**: {{.ExecutionLogsPath}}
{{if .StepContextOutput}}- **Declared context_output** (the step's primary output spec — start here):
  > {{.StepContextOutput}}
{{end}}
## Step output files (pre-enumerated — do NOT `+"`"+`ls`+"`"+` again)
{{.StepOutputFilesListing}}

## Execution logs files (pre-enumerated — do NOT `+"`"+`ls`+"`"+` again)
Full run trace: agent conversation, tool calls, result summary. Consult when the context_output doesn't contain what the contribution instruction asks for. Use `+"`"+`tail -c 30000`+"`"+` or `+"`"+`jq`+"`"+` on large files — don't `+"`"+`cat`+"`"+` them whole.

{{.ExecutionLogsFilesListing}}

## User's contribution instruction (what to extract)
{{.ContributionInstruction}}

## Your task
Apply the merge rules from the system prompt:
1. Read `+"`"+`{{.NotesIndexPath}}`+"`"+`. Then read step artifacts: start with the declared context_output file; fall back to execution logs (conversation/tool-call files) if observations the instruction needs aren't there. Skip files whose names clearly don't relate to the contribution instruction.
2. Pick the topic file(s) the contribution implies (entity-scoped or pattern-); append a dated section; compact if the file is over 20KB or has 30+ sections.
3. Sync `+"`"+`{{.NotesIndexPath}}`+"`"+` for every touched topic.
4. Print the final summary line.
`)

// KBUpdateAgent is the post-step knowledgebase update agent. It wraps a standard
// BaseOrchestratorAgent and supplies its own system + user prompts.
type KBUpdateAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewKBUpdateAgent constructs the agent. Matches the signature expected by
// CreateAndSetupStandardAgentWithConfig so it can plug into the existing factory.
func NewKBUpdateAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *KBUpdateAgent {
	// Reuses the learning agent type label — orchestration/event layer treats both as
	// background post-step analysis. Behavioral split lives in prompts, not the enum.
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config, logger, tracer, agents.TodoPlannerSuccessLearningAgentType, eventBridge,
	)
	return &KBUpdateAgent{BaseOrchestratorAgent: baseAgent}
}

// Execute runs one KB update pass. templateVars must include: StepID, StepTitle,
// StepDescription, RunFolder, StepOutputPath, ContributionInstruction,
// NotesFolderPath, NotesIndexPath.
func (agent *KBUpdateAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	systemPrompt := renderKBUpdateSystemPrompt(templateVars)
	userMessage := renderKBUpdateUserMessage(templateVars)

	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	type empty struct{}
	return agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, empty{}, systemPrompt, true)
}

func renderKBUpdateSystemPrompt(templateVars map[string]string) string {
	var result strings.Builder
	err := kbUpdateSystemPromptTemplate.Execute(&result, map[string]interface{}{
		"StepID":            templateVars["StepID"],
		"RunFolder":         templateVars["RunFolder"],
		"StepOutputPath":    templateVars["StepOutputPath"],
		"StepContextOutput": templateVars["StepContextOutput"],
		"ExecutionLogsPath": templateVars["ExecutionLogsPath"],
		"NotesFolderPath":   templateVars["NotesFolderPath"],
		"NotesIndexPath":    templateVars["NotesIndexPath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb update system prompt template execution failed: %v", err))
	}
	return result.String()
}

// Reorganize: applies a user-directed transformation (merge topics, drop topics,
// compact, rename) to the notes-only knowledgebase. Unlike update, it may delete
// or restructure prior notes — but only when the user's instruction explicitly
// calls for it.

var kbReorganizeSystemPromptTemplate = MustRegisterTemplate("kbReorganizeSystemPrompt", `# Knowledgebase Reorganize Agent

## READ-ONLY REVIEW OVERRIDE
This maintenance agent reviews a proposed KB reorganization. Do not create,
edit, move, merge, rename, compact, or delete files. Do not update _index.json.
Use shell only for read-only inspection and do not call
diff_patch_workspace_file. Treat every later mutation instruction as an audit
criterion. Return: verdict, ordered findings, exact evidence, bounded
recommended edits for the Pulse Fixer, and whether user judgment is required.

## Role
Apply a user-provided transformation to the workspace knowledgebase. You own reads and writes to the per-topic narrative files under `+"`"+`{{.NotesFolderPath}}/`+"`"+` and to `+"`"+`{{.NotesIndexPath}}`+"`"+` for this one operation.

Unlike the post-step KB update agent (which only adds observations), you MAY delete, rename, merge, or restructure existing notes — that is the whole point. But only when the user's instruction explicitly calls for it.

## Files

**`+"`"+`{{.NotesFolderPath}}/`+"`"+`** — per-topic narrative markdown files plus a registry:
`+"```"+`
notes/
├── _index.json                  # registry — `+"`"+`{{.NotesIndexPath}}`+"`"+`
├── <topic-id>.md                # one markdown file per topic (entity-slug or pattern-<slug>)
└── ...
`+"```"+`

`+"`"+`_index.json`+"`"+` schema:
`+"```"+`json
{
  "topics": [
    {
      "id": "<topic-id>",
      "file": "<topic-id>.md",
      "covers": ["<entity-slug>", ...],
      "last_updated": "<RFC3339>",
      "last_updated_by": { "step": "<step-id>", "run": "<run-folder>" },
      "size_bytes": <int>,
      "section_count": <int>
    }
  ]
}
`+"```"+`

Notes operations the user may ask for:
- **Merge two topics** (`+"`"+`merge notes/company-acme.md and notes/company-acme-corp.md`+"`"+`) — concatenate sections, dedupe near-duplicates, update `+"`"+`covers`+"`"+` to the union, drop the obsolete file from `+"`"+`_index.json`+"`"+`.
- **Drop a topic** — delete the file via shell, remove its entry from `+"`"+`_index.json`+"`"+`.
- **Drop sections from a bad run** — for each topic file, delete `+"`"+`## ...`+"`"+` sections whose body references the bad run id; recompute `+"`"+`size_bytes`+"`"+`/`+"`"+`section_count`+"`"+`.
- **Compact a topic file** — rewrite as a `+"`"+`## Historical context`+"`"+` summary (preserving the last 5 sections verbatim), update `+"`"+`size_bytes`+"`"+`/`+"`"+`section_count`+"`"+`.
- **Rename a topic** — move the file (`+"`"+`mv old.md new.md`+"`"+`), rewrite the H1 inside, update id/file in `+"`"+`_index.json`+"`"+`. If the rename corresponds to an entity-slug change, also rewrite cross-references inside other topics' markdown bodies.

## Transformation rules

1. **Read first, plan, then write.** Before any edit:
   - `+"`"+`cat '{{.NotesIndexPath}}'`+"`"+` and the topic files the instruction names.
   - Summarize what you found (topic counts, sizes, ids that match the instruction).
   - Decide exactly which topics/sections change and how. Write a brief plan before editing.
2. **Stay within the instruction.** Apply ONLY the transformation the user asked for. Do not opportunistically clean up or restructure other parts of the knowledgebase.
3. **Preserve narrative history.** When merging topic A into topic B, concatenate their sections (dedupe near-duplicates, keep the earlier section's body when content overlaps). When dropping sections from a bad run, surgically target only the sections whose body references that run id.
4. **Update `+"`"+`covers`+"`"+` when entities move.** If a merge changes which entities a topic discusses, update the `+"`"+`covers`+"`"+` array in `+"`"+`_index.json`+"`"+` to match.
5. **Preserve schema shape.** `+"`"+`_index.json`+"`"+` must remain valid JSON matching the schema above. Do not add extra top-level fields; do not rename standard fields.
6. **Sync `+"`"+`_index.json`+"`"+` after every notes change.** For every topic you mutated, update `+"`"+`size_bytes`+"`"+`, `+"`"+`section_count`+"`"+`, `+"`"+`last_updated`+"`"+`, `+"`"+`last_updated_by`+"`"+`. For deleted topics, remove the entry. For renamed topics, update both `+"`"+`id`+"`"+` and `+"`"+`file`+"`"+`.
7. **Notes scope discipline.** Read `+"`"+`{{.NotesIndexPath}}`+"`"+` first, then `+"`"+`cat`+"`"+` only the topic files the instruction names or implies. NEVER `+"`"+`cat notes/*.md`+"`"+` — file count grows unboundedly and loading all of them blows context.
8. **Entity-slug rename propagation.** If the instruction renames an entity slug that is referenced inside other topics' markdown bodies (e.g. `+"`"+`company-acme`+"`"+` → `+"`"+`company-acme-corp`+"`"+`), rewrite those cross-references too so future readers and consolidation tools can still resolve them.
9. **Idempotency.** If the transformation was already applied in a prior run (e.g. you were asked to dedupe two topics that are already merged), explain in the summary and make no changes.

## What NOT to do
- Do not invent topics or sections the knowledgebase did not already contain.
- Do not purge all topics "to start fresh" unless the instruction literally says so.
- Do not rename fields in the `+"`"+`_index.json`+"`"+` schema; agents and frontends depend on the shape.
- **Do NOT touch `+"`"+`knowledgebase/context/`+"`"+`** — that folder holds user-supplied runtime business context captured via the `+"`"+`capture_context`+"`"+` tool. It is a sub-section of the knowledgebase but is excluded from this reorganize pass and from any consolidate pass. The contents are user-owned and must never be rewritten by the optimizer. Restrict every read and every write to `+"`"+`knowledgebase/notes/`+"`"+` only.

## Tools
- **execute_shell_command** — read-only inspection (`+"`"+`cat`+"`"+`, `+"`"+`jq`+"`"+`, `+"`"+`wc`+"`"+`, `+"`"+`grep`+"`"+`, `+"`"+`find`+"`"+`) plus explicit file operations only when the requested transformation needs them (`+"`"+`mv`+"`"+` for rename, `+"`"+`rm`+"`"+` for drop). Do not use shell redirection/heredocs/tee/Python to write note content or the registry.
- **diff_patch_workspace_file** — for every content write inside topic files and `+"`"+`_index.json`+"`"+`, including compaction rewrites and new canonical topic files.

## Final action
Print exactly one summary line:
`+"`"+`KB reorganized: <short description of what changed>; notes touched: [<topic-id>, ...]`+"`"+`

If no notes were touched (e.g. instruction named topics that don't exist), print `+"`"+`KB reorganized: no-op; reason: <short reason>`+"`"+`.
`)

var kbReorganizeUserMessageTemplate = MustRegisterTemplate("kbReorganizeUserMessage", `# Knowledgebase reorganization request

## User's instruction
{{.Instruction}}

## Your task
1. Read `+"`"+`{{.NotesIndexPath}}`+"`"+` and `+"`"+`cat`+"`"+` only the topic files the instruction names — never glob `+"`"+`notes/*.md`+"`"+`.
2. Decide what changes the instruction implies. State the plan briefly.
3. Describe the exact transformation the Pulse Fixer should apply. Do not apply it.
4. Resync `+"`"+`{{.NotesIndexPath}}`+"`"+` for every touched topic.
5. Print the structured review result.
`)

type KBReorganizeAgent struct {
	*agents.BaseOrchestratorAgent
}

func NewKBReorganizeAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *KBReorganizeAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config, logger, tracer, agents.TodoPlannerSuccessLearningAgentType, eventBridge,
	)
	return &KBReorganizeAgent{BaseOrchestratorAgent: baseAgent}
}

// Execute runs one reorganization pass. templateVars must include: Instruction,
// NotesFolderPath, NotesIndexPath.
func (agent *KBReorganizeAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	systemPrompt := renderKBReorganizeSystemPrompt(templateVars)
	userMessage := renderKBReorganizeUserMessage(templateVars)
	inputProcessor := func(map[string]string) string {
		return userMessage
	}
	type empty struct{}
	return agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, empty{}, systemPrompt, true)
}

func renderKBReorganizeSystemPrompt(templateVars map[string]string) string {
	var result strings.Builder
	err := kbReorganizeSystemPromptTemplate.Execute(&result, map[string]interface{}{
		"NotesFolderPath": templateVars["NotesFolderPath"],
		"NotesIndexPath":  templateVars["NotesIndexPath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb reorganize system prompt template execution failed: %v", err))
	}
	return result.String()
}

func renderKBReorganizeUserMessage(templateVars map[string]string) string {
	var result strings.Builder
	err := kbReorganizeUserMessageTemplate.Execute(&result, map[string]interface{}{
		"Instruction":     templateVars["Instruction"],
		"NotesFolderPath": templateVars["NotesFolderPath"],
		"NotesIndexPath":  templateVars["NotesIndexPath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb reorganize user message template execution failed: %v", err))
	}
	return result.String()
}

func renderKBUpdateUserMessage(templateVars map[string]string) string {
	var result strings.Builder
	err := kbUpdateUserMessageTemplate.Execute(&result, map[string]interface{}{
		"StepID":                    templateVars["StepID"],
		"StepTitle":                 templateVars["StepTitle"],
		"StepDescription":           templateVars["StepDescription"],
		"RunFolder":                 templateVars["RunFolder"],
		"StepOutputPath":            templateVars["StepOutputPath"],
		"StepContextOutput":         templateVars["StepContextOutput"],
		"StepOutputFilesListing":    templateVars["StepOutputFilesListing"],
		"ExecutionLogsPath":         templateVars["ExecutionLogsPath"],
		"ExecutionLogsFilesListing": templateVars["ExecutionLogsFilesListing"],
		"ContributionInstruction":   templateVars["ContributionInstruction"],
		"NotesFolderPath":           templateVars["NotesFolderPath"],
		"NotesIndexPath":            templateVars["NotesIndexPath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb update user message template execution failed: %v", err))
	}
	return result.String()
}

// Consolidate: a global pass that reads all step contributions + step outputs from a
// selected run folder and produces cross-step consolidation work that a single step's
// KB update agent can't see — cross-step patterns, narrative dedupe across topics,
// topic consolidation. Runs OUT-OF-BAND from any single step, invoked by the builder
// via the `/improve-knowledge` cross-step checklist. Serialized through kbUpdateQueue so it
// can't race with per-step KB updates.

var kbConsolidateSystemPromptTemplate = MustRegisterTemplate("kbConsolidateSystemPrompt", `# Knowledgebase Consolidate Agent

## READ-ONLY REVIEW OVERRIDE
This maintenance agent reviews cross-step KB consolidation opportunities. Do not
create, edit, move, merge, canonicalize, or delete files. Do not update
_index.json. Use shell only for read-only inspection and do not call
diff_patch_workspace_file. Treat every later write instruction as an audit
criterion. Return: verdict, ordered findings, exact evidence, bounded
recommended edits for the Pulse Fixer, contradictions requiring user judgment,
and deferred items.

## Role
You are a cross-step consolidation pass over the workspace knowledgebase. You have a privileged, holistic view that the per-step KB update agent does not: you see every step's `+"`"+`knowledgebase_contribution`+"`"+` instruction AND every step's output folder for the selected run. Use this view to do work that is IMPOSSIBLE to do one step at a time — and only that work.

You own reads and writes to the per-topic narrative files under `+"`"+`{{.NotesFolderPath}}/`+"`"+` and to `+"`"+`{{.NotesIndexPath}}`+"`"+` for this one operation. Per-step KB updates are paused while you run.

## What you do (and don't)

**Do — cross-step work only, with holistic view as justification:**
- **Narrative dedupe across topics.** If two topic files were created in different runs covering near-identical subject matter (e.g. `+"`"+`notes/company-acme.md`+"`"+` and `+"`"+`notes/company-acme-inc.md`+"`"+`), merge them: concatenate sections, dedupe, canonicalize the entity slug, update `+"`"+`covers`+"`"+`, drop the obsolete file from `+"`"+`_index.json`+"`"+`.
- **Cross-step pattern narratives.** Write or update `+"`"+`notes/pattern-<slug>.md`+"`"+` when a pattern is only visible with multiple step outputs side-by-side (e.g. *"pattern-balance-anomaly: three accounts show the same dip-then-recover shape across quarter-end weeks"*). Populate `+"`"+`covers`+"`"+` with every entity slug the pattern touches.
- **Contradiction surfacing.** If two steps produced contradictory observations about the same entity (e.g. different claimed headcount, different ownership), surface the contradiction in a dated section of the entity's topic file with step provenance, rather than letting one silently clobber the other. Do NOT resolve the contradiction — that's for the user to decide; the reorganize agent can apply the resolution later.
- **Entity-slug canonicalization.** If multiple topics reference the same real-world entity under different slugs, pick one canonical slug and rewrite cross-references inside other topics' markdown bodies.

**Don't — out of scope:**
- Do NOT extract new observations from step outputs that a step's own KB update agent should have extracted. If a step has a `+"`"+`knowledgebase_contribution`+"`"+` but nothing from it landed in notes, report that as a diagnostic — do not silently re-run the extraction.
- Do NOT do per-file cleanup that isn't cross-step in nature (compaction, renaming). Those belong to the `+"`"+`/improve-knowledge`+"`"+` targeted checklist and parent fixer.
- Do NOT touch `+"`"+`learnings/`+"`"+` or `+"`"+`db/`+"`"+`.
- **Do NOT touch `+"`"+`knowledgebase/context/`+"`"+`** — that folder holds user-supplied runtime business context. It is excluded from consolidation. Read and write only `+"`"+`knowledgebase/notes/`+"`"+`.

## Inputs available to you

**Context files (read-only):**
- `+"`"+`{{.NotesIndexPath}}`+"`"+` — notes topic registry. Read this FIRST to know which topics exist.
- Step contributions block below (in the user message) — every step's `+"`"+`knowledgebase_contribution`+"`"+` string concatenated, with step ids. This is the declared schema across the workflow.
- Step output folders — enumerated in the user message. You MAY `+"`"+`cat`+"`"+` specific files to verify a pattern, but NEVER glob-read everything. Pick targeted files after the contributions block tells you what to look for.

**Objective (from user):** the consolidation goal for this invocation. Scope your work to it — do not opportunistically do other consolidation.

## Tools
- `+"`"+`execute_shell_command`+"`"+` — read/jq files and calculate values. Do not write KB content with shell redirection/heredocs/tee/Python.
- `+"`"+`diff_patch_workspace_file`+"`"+` — every content write for topic files and `+"`"+`_index.json`+"`"+`, including new files, section appends, merges, and compaction rewrites. Pass workspace-relative file paths or absolute paths under the workspace docs root.

## Safety rails
- Apply changes incrementally. If the objective calls for three consolidation actions, do them as three reads + three writes, not one megabatch.
- When canonicalizing an entity slug, scan other topics' bodies for literal mentions of the old slug and rewrite them too — otherwise cross-references drift.
- If you cannot confidently resolve a consolidation action (ambiguous slug mapping, unclear canonical label), SKIP it and include it in the summary as "deferred: <reason>" — do not guess.

## Final action
Print ONE summary line at the end:
`+"`"+`KB consolidated: <short description>; topics merged: [<old>→<new>, ...]; pattern notes written: [<topic-id>, ...]; contradictions surfaced: <count>; deferred: [<reason>, ...]`+"`"+`
Omit any clause whose count is zero.
`)

var kbConsolidateUserMessageTemplate = MustRegisterTemplate("kbConsolidateUserMessage", `# Knowledgebase consolidation request

## Objective
{{.Objective}}

## Step contributions across the workflow (declared schema)
{{.ContributionsBlock}}

## Step output folders available (read-only) for the selected run
{{.StepOutputFoldersBlock}}

## Your task
1. Read `+"`"+`{{.NotesIndexPath}}`+"`"+`. Form a picture of the current KB state.
2. Cross-reference against the step contributions block above. Look for: topic duplicates under different slugs, missing pattern narratives implied by overlapping contributions, contradictions between steps on the same entity.
3. Scope the review to the stated objective. State which consolidations you recommend and why.
4. Apply the consolidations. For each merge or pattern-write, `+"`"+`cat`+"`"+` only the specific topic files or step output files that substantiate the change.
5. Resync `+"`"+`{{.NotesIndexPath}}`+"`"+` for every touched topic.
6. Print the structured review result. Do not apply any consolidation.
`)

type KBConsolidateAgent struct {
	*agents.BaseOrchestratorAgent
}

func NewKBConsolidateAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *KBConsolidateAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config, logger, tracer, agents.TodoPlannerSuccessLearningAgentType, eventBridge,
	)
	return &KBConsolidateAgent{BaseOrchestratorAgent: baseAgent}
}

// Execute runs one consolidation pass. templateVars must include: Objective,
// ContributionsBlock, StepOutputFoldersBlock, NotesFolderPath, NotesIndexPath.
func (agent *KBConsolidateAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	systemPrompt := renderKBConsolidateSystemPrompt(templateVars)
	userMessage := renderKBConsolidateUserMessage(templateVars)
	inputProcessor := func(map[string]string) string {
		return userMessage
	}
	type empty struct{}
	return agent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, empty{}, systemPrompt, true)
}

func renderKBConsolidateSystemPrompt(templateVars map[string]string) string {
	var result strings.Builder
	err := kbConsolidateSystemPromptTemplate.Execute(&result, map[string]interface{}{
		"NotesFolderPath": templateVars["NotesFolderPath"],
		"NotesIndexPath":  templateVars["NotesIndexPath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb consolidate system prompt template execution failed: %v", err))
	}
	return result.String()
}

func renderKBConsolidateUserMessage(templateVars map[string]string) string {
	var result strings.Builder
	err := kbConsolidateUserMessageTemplate.Execute(&result, map[string]interface{}{
		"Objective":              templateVars["Objective"],
		"ContributionsBlock":     templateVars["ContributionsBlock"],
		"StepOutputFoldersBlock": templateVars["StepOutputFoldersBlock"],
		"NotesFolderPath":        templateVars["NotesFolderPath"],
		"NotesIndexPath":         templateVars["NotesIndexPath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb consolidate user message template execution failed: %v", err))
	}
	return result.String()
}
