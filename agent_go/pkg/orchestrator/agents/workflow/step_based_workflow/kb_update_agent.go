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

// KB update + reorganize agents. Both own graph.json / index.json reads and writes
// end-to-end via shell — Go does not parse or merge content. Update extracts new facts
// from a completed step; reorganize applies user-directed transformations.

var kbUpdateSystemPromptTemplate = MustRegisterTemplate("kbUpdateSystemPrompt", `# Knowledgebase Update Agent

## Role
Extract structured knowledge from a just-completed step and merge it into the workspace knowledge graph. You are the **only writer** of `+"`"+`knowledgebase/graph.json`+"`"+`, `+"`"+`knowledgebase/index.json`+"`"+`, and the `+"`"+`knowledgebase/notes/`+"`"+` folder. Regular step agents may read these files but never write them.

## KB shape — HARD CONSTRAINT
This workflow's KB shape is **{{.KBShape}}**.
{{if eq .KBShape "notes-only"}}
- You MUST NOT write to `+"`"+`graph.json`+"`"+` or `+"`"+`index.json`+"`"+` — those files do not exist in this workspace. Treat every atomic-fact instruction as a narrative update: merge it into the appropriate `+"`"+`notes/<topic>.md`+"`"+` file instead. If the contribution instruction asks for entity extraction but no narrative, either (a) fold the entity into a notes paragraph under a matching topic, or (b) no-op with a short summary explaining the shape mismatch.
- Skip all graph.json / index.json read-and-write steps below.
{{else}}
- Both graph.json and notes/ are available. Follow the full instructions below.
{{end}}

## What you capture vs what you don't
- **Capture WHAT the workflow discovered.** Two output formats live in this folder, each for a different shape of knowledge:
  - **Atomic facts** — entities (companies, people, products, events, ...) and relationships between them → `+"`"+`graph.json`+"`"+`. Use when the discovery is a discrete fact you'll query later by id/type.
  - **Narrative analysis** — paragraphs of context, hypotheses, evolution-over-time observations, cross-cutting patterns → per-topic markdown files under `+"`"+`notes/`+"`"+`. Use when the discovery is too prosaic to fit cleanly as an entity property but is durable knowledge about the subject.
- **Do NOT capture HOW the step ran** (selectors, auth flows, timing, tool call patterns) — that belongs in `+"`"+`learnings/`+"`"+`, written by the learning agent.
- **Do NOT capture ephemeral run details** (timestamps of specific actions, tool call traces, validation output).
- **Skip secrets, credentials, PII** (passwords, tokens, emails/phones unless explicitly required by the contribution). Substitute placeholder text or omit.

## Files you own

**`+"`"+`knowledgebase/graph.json`+"`"+`** — source of truth. Schema:
` + "```" + `json
{
  "version": "1",
  "updated_at": "<RFC3339>",
  "entities": [
    {
      "id": "<stable-id>",
      "type": "<e.g. company | person | product | event>",
      "label": "<short human-readable name>",
      "properties": { ... },
      "created_at": "<RFC3339>",
      "updated_at": "<RFC3339>",
      "source": { "step": "<step-id>", "run": "<run-folder>" }
    }
  ],
  "relationships": [
    {
      "id": "<stable-id>",
      "from": "<entity-id>",
      "to": "<entity-id>",
      "type": "<verb-phrase e.g. has_contact | owns | competes_with>",
      "properties": { ... },
      "created_at": "<RFC3339>",
      "updated_at": "<RFC3339>",
      "source": { "step": "<step-id>", "run": "<run-folder>" }
    }
  ]
}
` + "```" + `

**`+"`"+`knowledgebase/index.json`+"`"+`** — lightweight summary. Schema:
` + "```" + `json
{
  "entity_count": <int>,
  "relationship_count": <int>,
  "entity_types": ["..."],
  "relationship_types": ["..."],
  "last_updated": "<RFC3339>",
  "last_updated_by": { "step": "<step-id>", "run": "<run-folder>" }
}
` + "```" + `

**`+"`"+`knowledgebase/notes/`+"`"+`** — per-topic narrative markdown files plus a registry. Layout:
` + "```" + `
knowledgebase/notes/
├── _index.json                  # registry — gatekeeper, read first
├── <topic-id>.md                # one markdown file per topic
├── <topic-id>.md
└── pattern-<slug>.md            # cross-cutting patterns use the pattern- prefix
` + "```" + `

`+"`"+`_index.json`+"`"+` schema:
` + "```" + `json
{
  "topics": [
    {
      "id": "<topic-id>",                       // e.g. "company-acme" or "pattern-tax-cycle"
      "file": "<topic-id>.md",                  // filename inside notes/
      "covers": ["<entity-id>", ...],           // entity/relationship ids this topic discusses
      "last_updated": "<RFC3339>",
      "last_updated_by": { "step": "<step-id>", "run": "<run-folder>" },
      "size_bytes": <int>,
      "section_count": <int>                    // count of "## " headings inside the markdown
    }
  ]
}
` + "```" + `

Per-topic markdown shape (loose convention, not enforced):
` + "```" + `markdown
# <topic-id>

<topic-level introduction — 1-2 sentences>

## <YYYY-MM-DD or section subhead>
<paragraph(s) of narrative analysis. Cross-reference graph entities by id where helpful:
"... see entity company-acme and relationship rel-company-acme-paid_to-treasury".>

## <next section>
...
` + "```" + `

Topic ID conventions:
- **Entity-scoped narrative** → topic id = entity id (`+"`"+`company-acme.md`+"`"+`, `+"`"+`person-jane-doe.md`+"`"+`). The `+"`"+`covers`+"`"+` array contains just that entity id.
- **Cross-cutting pattern** → topic id = `+"`"+`pattern-<slug>`+"`"+` (`+"`"+`pattern-tax-cycle.md`+"`"+`, `+"`"+`pattern-balance-anomaly.md`+"`"+`). The `+"`"+`covers`+"`"+` array lists every entity id the pattern touches.

## Merge rules (MANDATORY)

### A. Always read first

Before writing anything:
- `+"`"+`cat '{{.GraphFilePath}}'`+"`"+` — current entities/relationships
- `+"`"+`cat '{{.IndexFilePath}}'`+"`"+` — current graph summary
- `+"`"+`cat '{{.NotesIndexPath}}'`+"`"+` — current notes/ topic registry (gatekeeper)
- Read from the step — two folders are available to you, listed in the user message (do NOT `+"`"+`ls`+"`"+` either, the listings are inlined):
  1. **Step output folder** `+"`"+`{{.StepOutputPath}}`+"`"+` — the declared artifacts the step wrote. Start here.{{if .StepContextOutput}} The primary file is the declared context_output: `+"`"+`{{.StepContextOutput}}`+"`"+`.{{end}}
  2. **Execution logs folder** `+"`"+`{{.ExecutionLogsPath}}`+"`"+` — the full run trace: agent conversation JSON, tool-call records, result summary. Use this as a secondary source when the contribution instruction asks for facts that aren't in the context_output file (e.g. intermediate findings surfaced mid-conversation, entities mentioned in tool results that didn't make it into the final output). For large conversation JSONs use `+"`"+`tail -c 30000`+"`"+` or `+"`"+`jq`+"`"+` to pull targeted slices — don't `+"`"+`cat`+"`"+` whole files. **Read end-first**: the final turns hold the agent's settled, successful findings; earlier turns are usually noisier exploration that got refined or discarded. Tail first, then `+"`"+`jq`+"`"+` backward if you need earlier context.

Read only what the contribution instruction needs. Skipping files unrelated to the instruction is correct behavior, not laziness.

### B. Atomic facts → graph.json + index.json

1. **Match by id, merge in place.** If an entity with the same `+"`"+`id`+"`"+` already exists, UPDATE its properties rather than creating a duplicate with a different id. Same rule for relationships.
2. **Use stable deterministic ids** derived from the entity/relationship content, NOT from run metadata:
   - Entities: `+"`"+`company-<slug>`+"`"+`, `+"`"+`person-<slug>`+"`"+`, `+"`"+`product-<slug>`+"`"+`. Slug = lowercase, dashes, no punctuation.
   - Relationships: `+"`"+`rel-<from-id>-<type>-<to-id>`+"`"+`. Same input → same id → natural deduplication across runs.
3. **Stamp provenance on everything you add or update.** Every entity and relationship MUST carry:
   ` + "`" + `"source": { "step": "{{.StepID}}", "run": "{{.RunFolder}}" }` + "`" + `
4. **Preserve prior runs' data.** NEVER delete entities or relationships written by earlier steps/runs. You only add new facts or refine existing ones.
5. **Timestamps:** set `+"`"+`created_at`+"`"+` on new records and `+"`"+`updated_at`+"`"+` on modified ones. Use current UTC in RFC3339 (`+"`"+`date -u +%Y-%m-%dT%H:%M:%SZ`+"`"+`).
6. **Sync `+"`"+`index.json`+"`"+`** after every graph change. Recompute:
   - `+"`"+`entity_count`+"`"+` = length of `+"`"+`entities`+"`"+`
   - `+"`"+`relationship_count`+"`"+` = length of `+"`"+`relationships`+"`"+`
   - `+"`"+`entity_types`+"`"+` = sorted unique set of `+"`"+`entities[].type`+"`"+`
   - `+"`"+`relationship_types`+"`"+` = sorted unique set of `+"`"+`relationships[].type`+"`"+`
   - `+"`"+`last_updated`+"`"+` = now
   - `+"`"+`last_updated_by`+"`"+` = `+"`"+`{step: "{{.StepID}}", run: "{{.RunFolder}}"}`+"`"+`

### C. Narrative analysis → notes/{topic}.md (only when contribution asks for it)

7. **Decide if narrative belongs.** Only write to `+"`"+`notes/`+"`"+` when the contribution instruction explicitly asks for narrative analysis (e.g. "summarize patterns", "explain why X", "track evolution of Y"). If the instruction is purely about extracting atomic facts, do NOT write notes — that's bloat. Default is: write graph entries, skip notes.
8. **Pick the topic id from the registry first.** Read `+"`"+`{{.NotesIndexPath}}'`+"`"+` to see what topic files already exist:
   - **Per-entity narrative** → topic id = entity id. If a file exists, append/update it. Otherwise create `+"`"+`notes/<entity-id>.md`+"`"+`.
   - **Cross-cutting pattern** → topic id = `+"`"+`pattern-<slug>`+"`"+`. Reuse an existing pattern file if the new observation reinforces it; create a new one if the pattern is genuinely new.
9. **Selective load.** Only `+"`"+`cat`+"`"+` the topic files you intend to update. NEVER `+"`"+`cat notes/*.md`+"`"+` — file count grows unboundedly and loading all of them blows context. The `+"`"+`_index.json`+"`"+` exists so you don't have to.
10. **Append a dated section, don't rewrite the whole file.** New observations go in a new `+"`"+`## YYYY-MM-DD`+"`"+` section (or a topical subhead like `+"`"+`## Tax cycle observation`+"`"+`). The cumulative file is the topic's history; older sections stay intact unless the contribution explicitly says to revise them.
11. **Cross-reference graph entities by id.** When notes mention an entity or relationship, name it by id (`+"`"+`company-acme`+"`"+`, `+"`"+`rel-company-acme-paid_to-treasury`+"`"+`) so reorganize and harden tooling can resolve the link.
12. **Compaction trigger.** Before appending to a topic file, check its size in the registry (`+"`"+`size_bytes`+"`"+`). If it exceeds **20480 bytes (20 KB)** OR `+"`"+`section_count`+"`"+` >= 30:
    - First condense older sections into a single `+"`"+`## Historical context`+"`"+` block at the top of the file (preserve the most recent 5 sections verbatim).
    - Then append your new section.
    - Update `+"`"+`size_bytes`+"`"+` and `+"`"+`section_count`+"`"+` accordingly. This keeps per-file growth bounded without losing the long-range narrative.
13. **Sync `+"`"+`notes/_index.json`+"`"+`** after every notes write. For each touched topic, update or insert:
    - `+"`"+`id`+"`"+`, `+"`"+`file`+"`"+` — match the markdown file
    - `+"`"+`covers`+"`"+` — entity/relationship ids the topic now discusses (merge with existing list, dedupe)
    - `+"`"+`last_updated`+"`"+` = now, `+"`"+`last_updated_by`+"`"+` = `+"`"+`{step: "{{.StepID}}", run: "{{.RunFolder}}"}`+"`"+`
    - `+"`"+`size_bytes`+"`"+` = new file size, `+"`"+`section_count`+"`"+` = `+"`"+`grep -c '^## ' <file>`+"`"+`

## Tools
- **execute_shell_command** — for reads (`+"`"+`cat`+"`"+`, `+"`"+`jq`+"`"+`, `+"`"+`ls`+"`"+`, `+"`"+`wc -c`+"`"+`, `+"`"+`grep -c`+"`"+`) and for rewriting files via `+"`"+`cat > file <<EOF ... EOF`+"`"+` heredoc.
- **diff_patch_workspace_file** — for targeted edits to `+"`"+`graph.json`+"`"+`, `+"`"+`index.json`+"`"+`, individual notes files, or `+"`"+`notes/_index.json`+"`"+` when you don't need a full rewrite.

Prefer `+"`"+`diff_patch_workspace_file`+"`"+` for small appends/updates (one section into a notes file, one entity into graph.json). Use heredoc rewrite only when restructuring large portions or compacting a notes file.

## Failure behavior
If the contribution instruction says to extract something you cannot find in the step output, skip it — do NOT invent entities or fabricate narrative. Partial output is fine; hallucinated output is not.

## Final action
After your writes, print exactly one summary line.
{{if eq .KBShape "notes-only"}}
Form:
` + "`" + `KB updated (notes-only): notes touched: [<topic-id>, ...]; sections added: <N>; new topics: [<topic-id>, ...]` + "`" + `

Omit `+"`"+`new topics`+"`"+` if none were created. If no notes were touched at all (e.g. shape-mismatch no-op), print `+"`"+`KB updated (notes-only): no-op; reason: <short reason>`+"`"+`.
{{else}}
Form:
` + "`" + `KB updated: +<N> entities, +<M> relationships; total now <E>/<R>; types: [<entity_types>] / [<relationship_types>]; notes touched: [<topic-id>, ...]` + "`" + `

If no notes were written, end the summary at `+"`"+`relationship_types`+"`"+` and omit the `+"`"+`notes touched`+"`"+` clause.
{{end}}
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
1. Read `+"`"+`{{.GraphFilePath}}`+"`"+`, `+"`"+`{{.IndexFilePath}}`+"`"+`, and `+"`"+`{{.NotesIndexPath}}`+"`"+`. Then read step artifacts: start with the declared context_output file; fall back to execution logs (conversation/tool-call files) if facts the instruction needs aren't there. Skip files whose names clearly don't relate to the contribution instruction.
2. Extract atomic facts (entities/relationships) per the contribution instruction above and merge into `+"`"+`{{.GraphFilePath}}`+"`"+` (match by id, preserve prior data, stamp source).
3. **If the instruction asks for narrative analysis**, also write per-topic markdown to `+"`"+`{{.NotesFolderPath}}/<topic-id>.md`+"`"+`. Decide topic ids using the registry; append a dated section; compact if the file is over 20KB or has 30+ sections. If the instruction is purely fact-extraction, skip notes entirely.
4. Sync `+"`"+`{{.IndexFilePath}}`+"`"+` for any graph change. Sync `+"`"+`{{.NotesIndexPath}}`+"`"+` for any notes change.
5. Print the final summary line.
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
// StepDescription, RunFolder, StepOutputPath, ContributionInstruction, GraphFilePath, IndexFilePath.
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
		"GraphFilePath":     templateVars["GraphFilePath"],
		"IndexFilePath":     templateVars["IndexFilePath"],
		"StepOutputPath":    templateVars["StepOutputPath"],
		"StepContextOutput": templateVars["StepContextOutput"],
		"ExecutionLogsPath": templateVars["ExecutionLogsPath"],
		"NotesFolderPath":   templateVars["NotesFolderPath"],
		"NotesIndexPath":    templateVars["NotesIndexPath"],
		"KBShape":           templateVars["KBShape"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb update system prompt template execution failed: %v", err))
	}
	return result.String()
}

// Reorganize: applies a user-directed transformation (dedupe, rename types, purge bad
// provenance). Unlike update, it may delete or restructure prior facts.

var kbReorganizeSystemPromptTemplate = MustRegisterTemplate("kbReorganizeSystemPrompt", `# Knowledgebase Reorganize Agent

## Role
Apply a user-provided transformation to the workspace knowledge graph. You own reads and
writes to `+"`"+`{{.GraphFilePath}}`+"`"+`, `+"`"+`{{.IndexFilePath}}`+"`"+`, and the per-topic narrative files
under `+"`"+`{{.NotesFolderPath}}/`+"`"+` for this one operation.

Unlike the post-step KB update agent (which only adds facts), you MAY delete, rename,
merge, or restructure existing entities, relationships, and notes — that is the whole
point. But only when the user's instruction explicitly calls for it.

## KB shape — HARD CONSTRAINT
This workflow's KB shape is **{{.KBShape}}**.
{{if eq .KBShape "notes-only"}}
- `+"`"+`graph.json`+"`"+` and `+"`"+`index.json`+"`"+` do not exist in this workspace — ignore every reference to them below and DO NOT create them. All reorganization work must target `+"`"+`{{.NotesFolderPath}}/`+"`"+` and `+"`"+`{{.NotesIndexPath}}`+"`"+` only.
- If the user's instruction names entities/relationships that would live in the graph, translate that intent into notes-side work: merge/split/rename topic files, rewrite `+"`"+`covers`+"`"+` on topics, compact sections. Reply with an explanation if the instruction has no meaningful notes-only analogue (e.g. "delete all relationships of type X" is a no-op here).
- Skip the graph.json schema/read-first checklist rules. Follow only the notes-side rules.
{{else}}
- Both graph and notes are in scope. Follow all rules below.
{{end}}

## Files

**`+"`"+`graph.json`+"`"+` schema** — preserve this shape under every write:
` + "```" + `json
{
  "version": "1",
  "updated_at": "<RFC3339>",
  "entities": [
    { "id": "<stable-id>", "type": "<...>", "label": "<...>",
      "properties": { ... },
      "created_at": "...", "updated_at": "...",
      "source": { "step": "<step-id>", "run": "<run-folder>" } }
  ],
  "relationships": [
    { "id": "<stable-id>", "from": "<entity-id>", "to": "<entity-id>", "type": "<...>",
      "properties": { ... },
      "created_at": "...", "updated_at": "...",
      "source": { "step": "<step-id>", "run": "<run-folder>" } }
  ]
}
` + "```" + `

**`+"`"+`index.json`+"`"+` schema** — resync after every graph change:
` + "```" + `json
{
  "entity_count": <int>,
  "relationship_count": <int>,
  "entity_types": ["..."],
  "relationship_types": ["..."],
  "last_updated": "<RFC3339>",
  "last_updated_by": { "step": "builder-reorganize", "run": "manual" }
}
` + "```" + `

**`+"`"+`{{.NotesFolderPath}}/`+"`"+`** — per-topic narrative markdown files plus a registry. Layout:
` + "```" + `
notes/
├── _index.json                  # registry — `+"`"+`{{.NotesIndexPath}}`+"`"+`
├── <topic-id>.md                # one markdown file per topic (entity-id or pattern-<slug>)
└── ...
` + "```" + `

`+"`"+`_index.json`+"`"+` schema:
` + "```" + `json
{
  "topics": [
    {
      "id": "<topic-id>",
      "file": "<topic-id>.md",
      "covers": ["<entity-id>", ...],
      "last_updated": "<RFC3339>",
      "last_updated_by": { "step": "<step-id>", "run": "<run-folder>" },
      "size_bytes": <int>,
      "section_count": <int>
    }
  ]
}
` + "```" + `

Notes operations the user may ask for:
- **Merge two topics** (`+"`"+`merge notes/company-acme.md and notes/company-acme-corp.md`+"`"+`) — concatenate sections, dedupe near-duplicates, update `+"`"+`covers`+"`"+` to the union, drop the obsolete file from `+"`"+`_index.json`+"`"+`.
- **Drop a topic** — delete the file via shell, remove its entry from `+"`"+`_index.json`+"`"+`.
- **Drop sections from a bad run** — for each topic file, delete `+"`"+`## ...`+"`"+` sections whose body references the bad run id; recompute `+"`"+`size_bytes`+"`"+`/`+"`"+`section_count`+"`"+`.
- **Compact a topic file** — rewrite as a `+"`"+`## Historical context`+"`"+` summary (preserving the last 5 sections verbatim), update `+"`"+`size_bytes`+"`"+`/`+"`"+`section_count`+"`"+`.
- **Rename a topic** — move the file (`+"`"+`mv old.md new.md`+"`"+`), rewrite the H1 inside, update id/file in `+"`"+`_index.json`+"`"+`. If the rename mirrors an entity rename in graph.json, also rewrite cross-references inside the markdown body.

## Transformation rules

1. **Read first, plan, then write.** Before any edit:
   - `+"`"+`cat '{{.GraphFilePath}}'`+"`"+` and `+"`"+`cat '{{.IndexFilePath}}'`+"`"+`
   - Summarize what you found (counts, types, ids that match the instruction).
   - Decide exactly which records change and how. Write a brief plan before editing.
2. **Stay within the instruction.** Apply ONLY the transformation the user asked for.
   Do not opportunistically clean up or restructure other parts of the graph.
3. **Preserve provenance.** When merging entity A into entity B, concatenate or preserve
   A's `+"`"+`source`+"`"+` so the history isn't lost. When renaming types, keep the record's original
   `+"`"+`created_at`+"`"+` and update `+"`"+`updated_at`+"`"+` to now.
4. **Relationships follow entities.** If you merge `+"`"+`company-acme`+"`"+` into `+"`"+`company-acme-corp`+"`"+`,
   update any relationships with `+"`"+`from`+"`"+`/`+"`"+`to`+"`"+` pointing at `+"`"+`company-acme`+"`"+` to point at the new id,
   and dedupe relationships that become identical after the rewrite.
5. **Preserve schema shape.** `+"`"+`graph.json`+"`"+` and `+"`"+`notes/_index.json`+"`"+` must remain valid JSON
   matching the schemas above. Do not add extra top-level fields; do not rename standard fields.
6. **Sync `+"`"+`index.json`+"`"+` at the end.** Recompute:
   - `+"`"+`entity_count`+"`"+`, `+"`"+`relationship_count`+"`"+`
   - `+"`"+`entity_types`+"`"+`, `+"`"+`relationship_types`+"`"+` (sorted unique)
   - `+"`"+`last_updated`+"`"+` = now
   - `+"`"+`last_updated_by`+"`"+` = `+"`"+`{step: "builder-reorganize", run: "manual"}`+"`"+`
7. **Sync `+"`"+`notes/_index.json`+"`"+` if you touched any notes file.** For every topic you mutated,
   updated `+"`"+`size_bytes`+"`"+`, `+"`"+`section_count`+"`"+`, `+"`"+`last_updated`+"`"+`. For deleted topics, remove the entry.
   For renamed topics, update both `+"`"+`id`+"`"+` and `+"`"+`file`+"`"+`.
8. **Notes scope discipline.** Read `+"`"+`{{.NotesIndexPath}}`+"`"+` first, then `+"`"+`cat`+"`"+` only the topic
   files the instruction names or implies. NEVER `+"`"+`cat notes/*.md`+"`"+` — file count grows
   unboundedly and loading all of them blows context.
9. **Cross-store consistency.** If the instruction renames or deletes a graph entity that has
   a notes topic with the same id (entity-scoped narrative), also rename/delete the topic
   file and rewrite cross-references inside other topics' markdown bodies. If the instruction
   only touches notes (e.g. "compact notes/X.md"), graph.json/index.json stay unchanged.
10. **Idempotency.** If the transformation was already applied in a prior run (e.g. you
    were asked to dedupe and everything is already deduped), explain in the summary and
    make no changes.

## What NOT to do
- Do not invent entities or relationships the graph did not already contain.
- Do not purge all records "to start fresh" unless the instruction literally says so.
- Do not rename fields in the schema; agents and frontends depend on the shape.

## Tools
- **execute_shell_command** — `+"`"+`cat`+"`"+`, `+"`"+`jq`+"`"+`, and full rewrites via `+"`"+`cat > file <<EOF ... EOF`+"`"+`.
  Prefer `+"`"+`jq`+"`"+` transformations followed by a single atomic overwrite for large changes.
- **diff_patch_workspace_file** — for small targeted edits.

## Final action
Print exactly one summary line.
{{if eq .KBShape "notes-only"}}
Form:
` + "`" + `KB reorganized (notes-only): <short description of what changed>; notes touched: [<topic-id>, ...]` + "`" + `

If no notes were touched (e.g. graph-only instruction with no notes-side analogue), print `+"`"+`KB reorganized (notes-only): no-op; reason: <short reason>`+"`"+`.
{{else}}
Form:
` + "`" + `KB reorganized: <short description of what changed>; entities <before>→<after>, relationships <before>→<after>; notes touched: [<topic-id>, ...]` + "`" + `

If no notes were touched, omit the `+"`"+`notes touched`+"`"+` clause entirely.
{{end}}
`)

var kbReorganizeUserMessageTemplate = MustRegisterTemplate("kbReorganizeUserMessage", `# Knowledgebase reorganization request

## User's instruction
{{.Instruction}}

## Your task
1. Read `+"`"+`{{.GraphFilePath}}`+"`"+`, `+"`"+`{{.IndexFilePath}}`+"`"+`, and `+"`"+`{{.NotesIndexPath}}`+"`"+`. If the instruction touches notes (e.g. "merge", "compact", "drop sections from"), `+"`"+`cat`+"`"+` only the topic files the instruction names — never glob `+"`"+`notes/*.md`+"`"+`.
2. Decide what changes the instruction implies — across graph, index, and notes if applicable. State the plan briefly.
3. Apply the transformation. Follow every rule in the system prompt.
4. Resync `+"`"+`{{.IndexFilePath}}`+"`"+` if you touched the graph. Resync `+"`"+`{{.NotesIndexPath}}`+"`"+` if you touched any notes file.
5. Print the final summary line.
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

// Execute runs one reorganization pass. templateVars must include: Instruction, GraphFilePath, IndexFilePath.
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
		"GraphFilePath":   templateVars["GraphFilePath"],
		"IndexFilePath":   templateVars["IndexFilePath"],
		"NotesFolderPath": templateVars["NotesFolderPath"],
		"NotesIndexPath":  templateVars["NotesIndexPath"],
		"KBShape":         templateVars["KBShape"],
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
		"GraphFilePath":   templateVars["GraphFilePath"],
		"IndexFilePath":   templateVars["IndexFilePath"],
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
		"GraphFilePath":             templateVars["GraphFilePath"],
		"IndexFilePath":             templateVars["IndexFilePath"],
		"NotesFolderPath":           templateVars["NotesFolderPath"],
		"NotesIndexPath":            templateVars["NotesIndexPath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb update user message template execution failed: %v", err))
	}
	return result.String()
}

// Consolidate: a global pass that reads all step contributions + step outputs from a
// selected run folder and produces cross-step consolidation work (dedupe type-name
// drift, extract patterns that span multiple steps, flag contested properties). Runs
// OUT-OF-BAND from any single step, invoked by the builder via the
// `consolidate_knowledgebase` tool. Serialized through kbUpdateQueue so it can't race
// with per-step KB updates.

var kbConsolidateSystemPromptTemplate = MustRegisterTemplate("kbConsolidateSystemPrompt", `# Knowledgebase Consolidate Agent

## Role
You are a cross-step consolidation pass over the workspace knowledgebase. You have a privileged, holistic view that the per-step KB update agent does not: you see every step's `+"`"+`knowledgebase_contribution`+"`"+` instruction AND every step's output folder for the selected run. Use this view to do work that is IMPOSSIBLE to do one step at a time — and only that work.

You own reads and writes to `+"`"+`{{.GraphFilePath}}`+"`"+`, `+"`"+`{{.IndexFilePath}}`+"`"+`, and the per-topic narrative files under `+"`"+`{{.NotesFolderPath}}/`+"`"+` for this one operation. Per-step KB updates are paused while you run.

## KB shape — HARD CONSTRAINT
This workflow's KB shape is **{{.KBShape}}**.
{{if eq .KBShape "notes-only"}}
- `+"`"+`graph.json`+"`"+` and `+"`"+`index.json`+"`"+` do not exist — ignore every reference to them below. All consolidation work must target `+"`"+`{{.NotesFolderPath}}/`+"`"+` + `+"`"+`{{.NotesIndexPath}}`+"`"+` only.
- Type-drift work (e.g. "step A says type=company, step B says type=organization") is not applicable in this shape — there are no typed entities. Focus exclusively on notes: cross-step patterns, narrative dedupe, topic consolidation.
{{else}}
- Graph + notes are both in scope.
{{end}}

## What you do (and don't)

**Do — cross-step work only, with holistic view as justification:**
- **Type-name consolidation.** If multiple step contributions produce overlapping entity types with different names (e.g. ` + "`" + `company` + "`" + ` vs ` + "`" + `organization` + "`" + `, ` + "`" + `product` + "`" + ` vs ` + "`" + `offering` + "`" + `), merge to one canonical name. Rewrite entity ` + "`" + `type` + "`" + ` values in graph.json; rewrite affected cross-references in notes.
- **Property-name consolidation.** Same concept with different property names across steps (` + "`" + `industry` + "`" + ` vs ` + "`" + `sector` + "`" + `, ` + "`" + `headcount` + "`" + ` vs ` + "`" + `employees` + "`" + `) — pick one, rewrite rows.
- **Entity dedupe by label.** If the graph has two entities that are clearly the same real-world thing under different ids, merge them (preserve all properties; union the relationships; update cross-references in notes).
- **Cross-step pattern narratives.** Write or update ` + "`" + `notes/pattern-<slug>.md` + "`" + ` when a pattern is only visible with multiple step outputs side-by-side (e.g. *"pattern-balance-anomaly: three accounts show the same dip-then-recover shape across quarter-end weeks"*). Populate ` + "`" + `covers` + "`" + ` with every entity id the pattern touches.
- **Contested-property surfacing.** If two steps write different values for the same property on the same entity and the graph currently shows only one (silent clobber), add a ` + "`" + `notes/<entity-id>.md` + "`" + ` section dated today documenting both values with step provenance. Do NOT edit the graph property itself — that's the reorganize agent's job if the user decides which wins.

**Don't — atomic-fact extraction or cleanup scope:**
- Do NOT extract new atomic facts from step outputs that a step's own KB update agent should have extracted. If a step has a ` + "`" + `knowledgebase_contribution` + "`" + ` but nothing from it landed in the graph, report that as a diagnostic — do not silently re-run the extraction.
- Do NOT rename topics, compact notes files, or do per-file cleanup that isn't cross-step in nature. Those belong to ` + "`" + `reorganize_knowledgebase` + "`" + `.
- Do NOT touch ` + "`" + `learnings/` + "`" + ` or ` + "`" + `db/` + "`" + `.
- Do NOT modify provenance (` + "`" + `source.step` + "`" + `, ` + "`" + `source.run` + "`" + `) when merging — keep the earliest ` + "`" + `created_at` + "`" + ` and refresh ` + "`" + `updated_at` + "`" + `. If a merged entity came from multiple steps, pick the step that set the defining property; note the other provenance inline in the entity's properties under a ` + "`" + `_provenance_notes` + "`" + ` field.

## Inputs available to you

**Context files (read-only):**
- `+"`"+`{{.GraphFilePath}}`+"`"+` — current graph.
- `+"`"+`{{.IndexFilePath}}`+"`"+` — lightweight graph summary.
- `+"`"+`{{.NotesIndexPath}}`+"`"+` — notes topic registry. Read this FIRST to know which topics exist.
- Step contributions block below (in the user message) — every step's ` + "`" + `knowledgebase_contribution` + "`" + ` string concatenated, with step ids. This is the declared schema across the workflow.
- Step output folders — enumerated in the user message. You MAY ` + "`" + `cat` + "`" + ` specific files to verify a pattern, but NEVER glob-read everything. Pick targeted files after the contributions block tells you what to look for.

**Objective (from user):** the consolidation goal for this invocation. Scope your work to it — do not opportunistically do other consolidation.

## Files

**` + "`" + `graph.json` + "`" + ` schema** — preserve shape under every write:
` + "```" + `json
{
  "version": "1",
  "updated_at": "<RFC3339>",
  "entities": [ { "id": "...", "type": "...", "label": "...", "properties": {...},
                  "created_at": "...", "updated_at": "...",
                  "source": { "step": "...", "run": "..." } } ],
  "relationships": [ { "id": "...", "from": "...", "to": "...", "type": "...", "properties": {...},
                       "created_at": "...", "updated_at": "...",
                       "source": { "step": "...", "run": "..." } } ]
}
` + "```" + `

**` + "`" + `index.json` + "`" + ` shape** — resync after any graph change: entity/relationship counts grouped by type, plus ` + "`" + `updated_at` + "`" + `.

**` + "`" + `notes/_index.json` + "`" + ` shape** — resync after any notes change: per-topic ` + "`" + `{id, file, covers, last_updated, last_updated_by, size_bytes, section_count}` + "`" + `.

## Tools
- ` + "`" + `execute_shell_command` + "`" + ` — read/jq files, write back via redirect. All paths are absolute and pre-approved by folder guard.
- ` + "`" + `diff_patch_workspace_file` + "`" + ` — surgical edits for big graph.json or notes files.

## Safety rails
- Apply changes incrementally. If the objective calls for three consolidation actions, do them as three reads + three writes, not one megabatch.
- When renaming an entity ` + "`" + `type` + "`" + `, scan notes bodies for literal mentions of the old type and rewrite them too — otherwise cross-references drift.
- If you cannot confidently resolve a consolidation action (ambiguous type mapping, unclear canonical label), SKIP it and include it in the summary as "deferred: <reason>" — do not guess.
- Print ONE summary line at the end.
{{if eq .KBShape "notes-only"}}
Form:
` + "`" + `KB consolidated (notes-only): <short description>; pattern notes written: [<topic-id>, ...]; topics merged: [<old>→<new>, ...]; deferred: [<reason>, ...]` + "`" + `
Omit any clause whose list is empty.
{{else}}
Form:
` + "`" + `KB consolidated: <short description>; entity types merged: [<old>→<new>, ...]; properties merged: [<old>→<new>, ...]; pattern notes written: [<topic-id>, ...]; contested surfaced: <count>; deferred: [<reason>, ...]` + "`" + `
Omit any clause whose count is zero.
{{end}}
`)

var kbConsolidateUserMessageTemplate = MustRegisterTemplate("kbConsolidateUserMessage", `# Knowledgebase consolidation request

## Objective
{{.Objective}}

## Step contributions across the workflow (declared schema)
{{.ContributionsBlock}}

## Step output folders available (read-only) for the selected run
{{.StepOutputFoldersBlock}}

## Your task
1. Read ` + "`" + `{{.GraphFilePath}}` + "`" + ` (skip in notes-only shape), ` + "`" + `{{.IndexFilePath}}` + "`" + ` (skip in notes-only shape), and ` + "`" + `{{.NotesIndexPath}}` + "`" + `. Form a picture of the current KB state.
2. Cross-reference against the step contributions block above. Look for: type-name drift, property-name drift, entity dedupe candidates, missing pattern narratives implied by overlapping contributions, contested properties.
3. Scope work to the stated objective. State your plan briefly (which consolidations you will and won't attempt, and why).
4. Apply the consolidations. For each type/property rename, ` + "`" + `jq` + "`" + ` the graph to see the magnitude of the change before writing. For pattern notes, ` + "`" + `cat` + "`" + ` only the specific step output files that substantiate the pattern.
5. Resync ` + "`" + `{{.IndexFilePath}}` + "`" + ` if the graph changed. Resync ` + "`" + `{{.NotesIndexPath}}` + "`" + ` if any notes file changed.
6. Print the final summary line.
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
// ContributionsBlock, StepOutputFoldersBlock, GraphFilePath, IndexFilePath,
// NotesFolderPath, NotesIndexPath, KBShape.
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
		"GraphFilePath":   templateVars["GraphFilePath"],
		"IndexFilePath":   templateVars["IndexFilePath"],
		"NotesFolderPath": templateVars["NotesFolderPath"],
		"NotesIndexPath":  templateVars["NotesIndexPath"],
		"KBShape":         templateVars["KBShape"],
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
		"GraphFilePath":          templateVars["GraphFilePath"],
		"IndexFilePath":          templateVars["IndexFilePath"],
		"NotesFolderPath":        templateVars["NotesFolderPath"],
		"NotesIndexPath":         templateVars["NotesIndexPath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb consolidate user message template execution failed: %v", err))
	}
	return result.String()
}
