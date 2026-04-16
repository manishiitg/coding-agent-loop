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
Extract structured knowledge from a just-completed step and merge it into the workspace knowledge graph. You are the **only writer** of `+"`"+`knowledgebase/graph.json`+"`"+` and `+"`"+`knowledgebase/index.json`+"`"+`. Regular step agents may read these files but never write them.

## What you capture vs what you don't
- **Capture WHAT the workflow discovered** — entities (companies, people, products, events, ...) and relationships between them. Durable facts that survive across runs.
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

## Merge rules (MANDATORY)

1. **Read first.** Always start with:
   - `+"`"+`cat '{{.GraphFilePath}}'`+"`"+`
   - `+"`"+`cat '{{.IndexFilePath}}'`+"`"+`
   - `+"`"+`ls '{{.StepOutputPath}}'`+"`"+` then read relevant output files
2. **Match by id, merge in place.** If an entity with the same `+"`"+`id`+"`"+` already exists, UPDATE its properties rather than creating a duplicate with a different id. Same rule for relationships.
3. **Use stable deterministic ids** derived from the entity/relationship content, NOT from run metadata:
   - Entities: `+"`"+`company-<slug>`+"`"+`, `+"`"+`person-<slug>`+"`"+`, `+"`"+`product-<slug>`+"`"+`. Slug = lowercase, dashes, no punctuation.
   - Relationships: `+"`"+`rel-<from-id>-<type>-<to-id>`+"`"+`. Same input → same id → natural deduplication across runs.
4. **Stamp provenance on everything you add or update.** Every entity and relationship MUST carry:
   ` + "`" + `"source": { "step": "{{.StepID}}", "run": "{{.RunFolder}}" }` + "`" + `
5. **Preserve prior runs' data.** NEVER delete entities or relationships written by earlier steps/runs. You only add new facts or refine existing ones.
6. **Timestamps:** set `+"`"+`created_at`+"`"+` on new records and `+"`"+`updated_at`+"`"+` on modified ones. Use current UTC in RFC3339 (`+"`"+`date -u +%Y-%m-%dT%H:%M:%SZ`+"`"+`).
7. **Sync `+"`"+`index.json`+"`"+`** after every graph change. Recompute:
   - `+"`"+`entity_count`+"`"+` = length of `+"`"+`entities`+"`"+`
   - `+"`"+`relationship_count`+"`"+` = length of `+"`"+`relationships`+"`"+`
   - `+"`"+`entity_types`+"`"+` = sorted unique set of `+"`"+`entities[].type`+"`"+`
   - `+"`"+`relationship_types`+"`"+` = sorted unique set of `+"`"+`relationships[].type`+"`"+`
   - `+"`"+`last_updated`+"`"+` = now
   - `+"`"+`last_updated_by`+"`"+` = `+"`"+`{step: "{{.StepID}}", run: "{{.RunFolder}}"}`+"`"+`

## Tools
- **execute_shell_command** — for reads (`+"`"+`cat`+"`"+`, `+"`"+`jq`+"`"+`, `+"`"+`ls`+"`"+`) and for rewriting files via `+"`"+`cat > file <<EOF ... EOF`+"`"+` heredoc.
- **diff_patch_workspace_file** — for targeted edits to `+"`"+`graph.json`+"`"+` or `+"`"+`index.json`+"`"+` when you don't need a full rewrite.

Prefer `+"`"+`diff_patch_workspace_file`+"`"+` for small appends/updates. Use heredoc rewrite only when restructuring large portions.

## Failure behavior
If the contribution instruction says to extract something you cannot find in the step output, skip it — do NOT invent entities. Partial output is fine; hallucinated output is not.

## Final action
After your writes, print exactly one summary line in this form:
` + "`" + `KB updated: +<N> entities, +<M> relationships; total now <E>/<R>; types: [<entity_types>] / [<relationship_types>]` + "`" + `
`)

var kbUpdateUserMessageTemplate = MustRegisterTemplate("kbUpdateUserMessage", `# Knowledgebase update request

## Step just completed
- **Step ID**: {{.StepID}}
- **Step title**: {{.StepTitle}}
- **Step description**: {{.StepDescription}}
- **Run folder**: {{.RunFolder}}
- **Step output path**: {{.StepOutputPath}}

## User's contribution instruction (what to extract)
{{.ContributionInstruction}}

## Your task
Apply the merge rules from the system prompt:
1. Read `+"`"+`{{.GraphFilePath}}`+"`"+` and `+"`"+`{{.IndexFilePath}}`+"`"+`, plus the step output files under `+"`"+`{{.StepOutputPath}}`+"`"+`.
2. Extract entities/relationships per the contribution instruction above.
3. Merge into `+"`"+`{{.GraphFilePath}}`+"`"+` (match by id, preserve prior data, stamp source).
4. Sync `+"`"+`{{.IndexFilePath}}`+"`"+`.
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
		"StepID":         templateVars["StepID"],
		"RunFolder":      templateVars["RunFolder"],
		"GraphFilePath":  templateVars["GraphFilePath"],
		"IndexFilePath":  templateVars["IndexFilePath"],
		"StepOutputPath": templateVars["StepOutputPath"],
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
writes to `+"`"+`{{.GraphFilePath}}`+"`"+` and `+"`"+`{{.IndexFilePath}}`+"`"+` for this one operation.

Unlike the post-step KB update agent (which only adds facts), you MAY delete, rename,
merge, or restructure existing entities and relationships — that is the whole point.
But only when the user's instruction explicitly calls for it.

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
5. **Preserve schema shape.** `+"`"+`graph.json`+"`"+` must remain valid JSON matching the schema above.
   Do not add extra top-level fields; do not rename the standard fields.
6. **Sync `+"`"+`index.json`+"`"+` at the end.** Recompute:
   - `+"`"+`entity_count`+"`"+`, `+"`"+`relationship_count`+"`"+`
   - `+"`"+`entity_types`+"`"+`, `+"`"+`relationship_types`+"`"+` (sorted unique)
   - `+"`"+`last_updated`+"`"+` = now
   - `+"`"+`last_updated_by`+"`"+` = `+"`"+`{step: "builder-reorganize", run: "manual"}`+"`"+`
7. **Idempotency.** If the transformation was already applied in a prior run (e.g. you
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
Print exactly one summary line:
` + "`" + `KB reorganized: <short description of what changed>; entities <before>→<after>, relationships <before>→<after>` + "`" + `
`)

var kbReorganizeUserMessageTemplate = MustRegisterTemplate("kbReorganizeUserMessage", `# Knowledgebase reorganization request

## User's instruction
{{.Instruction}}

## Your task
1. Read `+"`"+`{{.GraphFilePath}}`+"`"+` and `+"`"+`{{.IndexFilePath}}`+"`"+`.
2. Decide what changes the instruction implies. State the plan briefly.
3. Apply the transformation. Follow every rule in the system prompt.
4. Resync `+"`"+`{{.IndexFilePath}}`+"`"+`.
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
		"GraphFilePath": templateVars["GraphFilePath"],
		"IndexFilePath": templateVars["IndexFilePath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb reorganize system prompt template execution failed: %v", err))
	}
	return result.String()
}

func renderKBReorganizeUserMessage(templateVars map[string]string) string {
	var result strings.Builder
	err := kbReorganizeUserMessageTemplate.Execute(&result, map[string]interface{}{
		"Instruction":   templateVars["Instruction"],
		"GraphFilePath": templateVars["GraphFilePath"],
		"IndexFilePath": templateVars["IndexFilePath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb reorganize user message template execution failed: %v", err))
	}
	return result.String()
}

func renderKBUpdateUserMessage(templateVars map[string]string) string {
	var result strings.Builder
	err := kbUpdateUserMessageTemplate.Execute(&result, map[string]interface{}{
		"StepID":                  templateVars["StepID"],
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"RunFolder":               templateVars["RunFolder"],
		"StepOutputPath":          templateVars["StepOutputPath"],
		"ContributionInstruction": templateVars["ContributionInstruction"],
		"GraphFilePath":           templateVars["GraphFilePath"],
		"IndexFilePath":           templateVars["IndexFilePath"],
	})
	if err != nil {
		panic(fmt.Sprintf("kb update user message template execution failed: %v", err))
	}
	return result.String()
}
