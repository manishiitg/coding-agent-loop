package step_based_workflow

import (
	"strings"
)

// BuildStepKBGuidance returns the KB contribution guidance to splice into a step
// agent's system prompt when it's responsible for writing KB itself.
//
// Notes-only KB model: the knowledgebase is a set of per-topic markdown files
// under knowledgebase/notes/ plus notes/_index.json as a registry. There is no
// graph.json / index.json surface. Writes go through shell + diff_patch_workspace_file.
//
// Returns an empty string unless writeMethod is "direct" AND kbAccess allows writes —
// in every other case (read-only, agent-mode, disabled) the step is not the writer
// and this block must not appear. When returned, the block covers: topic-ID conventions,
// read-first discipline, append-don't-rewrite rules, _index.json sync requirement,
// and (if provided) the per-step knowledgebase_contribution as a contract.
func BuildStepKBGuidance(kbAccess, writeMethod, kbContribution string) string {
	if writeMethod != KBWriteMethodDirect {
		return ""
	}
	if !kbAccessAllowsWrite(kbAccess) {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n### Knowledgebase contribution (DIRECT write)\n")
	b.WriteString("You are the sole writer of KB for this step — the post-step KB update agent does NOT run. Contribute inline, then finish the step.\n\n")
	b.WriteString("**Surface:** per-topic markdown files under `knowledgebase/notes/` plus `notes/_index.json` as the registry. Write with shell heredoc (for new files) or `diff_patch_workspace_file` (for existing files).\n\n")

	b.WriteString("**Topic ID conventions:**\n")
	b.WriteString("- **Entity-scoped narrative** → topic id = entity slug (e.g. `company-acme.md`, `person-jane-doe.md`).\n")
	b.WriteString("- **Cross-cutting pattern** → topic id = `pattern-<slug>` (e.g. `pattern-tax-cycle.md`, `pattern-balance-anomaly.md`).\n")
	b.WriteString("- Prefer reusing an existing topic over creating a new one. `cat notes/_index.json` first to see what exists.\n\n")

	b.WriteString("**Discipline:**\n")
	b.WriteString("1. **Read first.** `cat knowledgebase/notes/_index.json` to see which topics exist and what they cover. Read only the specific topic files relevant to your work — never glob `notes/*.md` (unbounded).\n")
	b.WriteString("2. **Append, don't rewrite.** For an existing note, add a dated `## YYYY-MM-DD` section (or topical subhead) via `diff_patch_workspace_file`. Never rewrite the whole file wholesale — that destroys prior-run contributions.\n")
	b.WriteString("3. **Keep it useful.** Cross-reference entities by slug inside notes (e.g. \"see company-acme\") so future reorganize/consolidation passes can resolve links. Concise, concrete observations beat verbose restatements.\n")
	b.WriteString("4. **Update `notes/_index.json` after every note write.** Bump `size_bytes`, `section_count`, `last_updated`, `last_updated_by`; merge new entity ids into `covers[]`. Use `diff_patch_workspace_file` with `jq`-built diffs — never rewrite the index wholesale.\n")
	b.WriteString("5. **No deletes.** Never remove sections from earlier steps/runs. Refinement only.\n")
	b.WriteString("6. **No fabrication.** Capture only observations from this execution. If a pattern is unverified, say so explicitly.\n")
	b.WriteString("7. **Do not edit `knowledgebase/context/`.** That folder is user-supplied runtime context captured by the builder, not step-discovered KB notes.\n")

	if trimmed := strings.TrimSpace(kbContribution); trimmed != "" {
		b.WriteString("\n**For this step specifically, contribute:**\n")
		b.WriteString(trimmed)
		b.WriteString("\n\nIf the contribution above names something you cannot verify from the step's work, skip it — do not invent narrative. Partial output is fine; hallucinated output is not.\n")
	} else {
		b.WriteString("\n(No `knowledgebase_contribution` was declared for this step, so contribute whatever durable narrative the step surfaced that a future step would want to read.)\n")
	}

	return b.String()
}

// BuildKBContributionReviewMessage returns the one-shot user message injected
// after a step's first successful completion in direct-write mode, asking the
// step agent to verify its KB contributions against the author's contract.
//
// Returns an empty string when a review is not warranted (method != direct,
// kbAccess doesn't permit writes, or contribution is empty). This is the final
// turn for KB work on the step — no self-nudging loop.
func BuildKBContributionReviewMessage(kbAccess, writeMethod, contribution string) string {
	if writeMethod != KBWriteMethodDirect {
		return ""
	}
	if !kbAccessAllowsWrite(kbAccess) {
		return ""
	}
	trimmed := strings.TrimSpace(contribution)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Knowledgebase Contribution Self-Review (final turn)\n\n")
	b.WriteString("The step's output validation passed — before it's accepted, verify you fulfilled your `knowledgebase_contribution` contract.\n\n")

	b.WriteString("**Enumerate what you contributed:**\n")
	b.WriteString("- Topics you wrote or updated under `notes/` (list the markdown filenames and which sections you added).\n\n")

	b.WriteString("**Compare against the contract.** If anything required is missing, use shell + `diff_patch_workspace_file` under `notes/` to add it now, and update `notes/_index.json` accordingly. If every requirement is already covered, reply with a short summary of what you contributed — no further tool calls needed.\n\n")

	b.WriteString("**Contract:**\n")
	b.WriteString(trimmed)
	b.WriteString("\n\n")

	b.WriteString("**Important:** this is your final turn for KB work on this step. After this response, the step will be accepted regardless of any further gaps — there is no second review. Do not invent facts the step did not actually establish; partial coverage is better than fabricated coverage.\n")

	return b.String()
}
