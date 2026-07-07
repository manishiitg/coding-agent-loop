package step_based_workflow

import (
	"path"
	"strings"
)

// BuildStepKBGuidance returns the KB contribution guidance to splice into a step
// agent's system prompt when it's responsible for writing KB itself.
//
// Notes-only KB model: the knowledgebase is a set of per-topic markdown files
// under knowledgebase/notes/ plus notes/_index.json as a registry. There is no
// graph.json / index.json surface. Writes go through diff_patch_workspace_file.
//
// Returns an empty string unless writeMethod is "direct" AND kbAccess allows writes —
// in every other case (read-only, agent-mode, disabled) the step is not the writer
// and this block must not appear. When returned, the block covers: topic-ID conventions,
// read-first discipline, append-don't-rewrite rules, _index.json sync requirement,
// and (if provided) the per-step knowledgebase_contribution as a contract.
func BuildStepKBGuidance(kbAccess, writeMethod, kbContribution string) string {
	return BuildStepKBGuidanceWithTarget(kbAccess, writeMethod, kbContribution, "")
}

func BuildStepKBGuidanceWithTarget(kbAccess, writeMethod, kbContribution, notesTargetPath string) string {
	if writeMethod != KBWriteMethodDirect {
		return ""
	}
	if !kbAccessAllowsWrite(kbAccess) {
		return ""
	}

	notesTarget := strings.TrimRight(strings.TrimSpace(notesTargetPath), "/")
	if notesTarget == "" {
		notesTarget = "knowledgebase/notes"
	}
	notesIndex := path.Join(notesTarget, KBNotesIndexFileName)

	var b strings.Builder
	b.WriteString("\n### Knowledgebase contribution (DIRECT write)\n")
	b.WriteString("You are the sole writer of KB for this step — the post-step KB update agent does NOT run. Contribute inline, then finish the step.\n\n")
	b.WriteString("**Target:** `")
	b.WriteString(notesTarget)
	b.WriteString("/` plus registry `")
	b.WriteString(notesIndex)
	b.WriteString("`. Use these exact paths; do not rely on your shell working directory.\n\n")
	b.WriteString("**Surface:** per-topic markdown files under the target folder plus `")
	b.WriteString(KBNotesIndexFileName)
	b.WriteString("` as the registry. Write every KB content change with `diff_patch_workspace_file`, including new topic files and registry updates. Shell is for read-only inspection (`cat`, `jq`, `wc`, `grep`, `find`) unless a dedicated reorganize operation explicitly asks for `mv`/`rm`.\n\n")

	b.WriteString("**Topic ID conventions:**\n")
	b.WriteString("- **Entity-scoped narrative** → topic id = entity slug (e.g. `company-acme.md`, `person-jane-doe.md`).\n")
	b.WriteString("- **Cross-cutting pattern** → topic id = `pattern-<slug>` (e.g. `pattern-tax-cycle.md`, `pattern-balance-anomaly.md`).\n")
	b.WriteString("- Prefer reusing an existing topic over creating a new one. `cat '")
	b.WriteString(notesIndex)
	b.WriteString("'` first to see what exists.\n\n")

	b.WriteString("**Discipline:**\n")
	b.WriteString("1. **Read first.** `cat '")
	b.WriteString(notesIndex)
	b.WriteString("'` to see which topics exist and what they cover. Read only the specific topic files relevant to your work — never glob `notes/*.md` (unbounded).\n")
	b.WriteString("2. **Patch, don't rewrite.** Add a dated `## YYYY-MM-DD` section (or topical subhead) with `diff_patch_workspace_file`. Use the same tool for creating new `")
	b.WriteString(path.Join(notesTarget, "<topic>.md"))
	b.WriteString("` files. Never rewrite the whole file wholesale — that destroys prior-run contributions.\n")
	b.WriteString("3. **Keep it useful.** Cross-reference entities by slug inside notes (e.g. \"see company-acme\") so future reorganize/consolidation passes can resolve links. Concise, concrete observations beat verbose restatements.\n")
	b.WriteString("4. **Update `")
	b.WriteString(notesIndex)
	b.WriteString("` after every note write.** Bump `size_bytes`, `section_count`, `last_updated`, `last_updated_by`; merge new entity ids into `covers[]`. You may use shell/JQ to compute values, but apply the actual edit with `diff_patch_workspace_file`.\n")
	b.WriteString("5. **No deletes.** Never remove sections from earlier steps/runs. Refinement only.\n")
	b.WriteString("6. **No fabrication.** Capture only observations from this execution. If a pattern is unverified, say so explicitly.\n")
	b.WriteString("7. **Do not edit `knowledgebase/context/`.** That folder is user-supplied runtime context captured by the builder, not step-discovered KB notes.\n")
	b.WriteString("8. **No shell writes.** Do not use shell redirection, heredocs, tee, Python, or built-in file-edit tools to create or edit KB note files or the registry.\n")

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
	return BuildKBContributionReviewMessageWithTarget(kbAccess, writeMethod, contribution, "")
}

func BuildKBContributionReviewMessageWithTarget(kbAccess, writeMethod, contribution, notesTargetPath string) string {
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
	notesTarget := strings.TrimRight(strings.TrimSpace(notesTargetPath), "/")
	if notesTarget == "" {
		notesTarget = "knowledgebase/notes"
	}
	notesIndex := path.Join(notesTarget, KBNotesIndexFileName)

	var b strings.Builder
	b.WriteString("## Knowledgebase Contribution Self-Review (final turn)\n\n")
	b.WriteString("The step's output validation passed — before it's accepted, verify you fulfilled your `knowledgebase_contribution` contract.\n\n")
	b.WriteString("**Target:** `")
	b.WriteString(notesTarget)
	b.WriteString("/` plus registry `")
	b.WriteString(notesIndex)
	b.WriteString("`. Use these exact paths; do not rely on cwd.\n\n")

	b.WriteString("**Enumerate what you contributed:**\n")
	b.WriteString("- Topics you wrote or updated under `notes/` (list the markdown filenames and which sections you added).\n\n")

	b.WriteString("**Compare against the contract.** If anything required is missing, use `diff_patch_workspace_file` under the target folder to add it now, and update `")
	b.WriteString(notesIndex)
	b.WriteString("` accordingly. Do not use shell redirection, heredocs, tee, Python, or built-in file-edit tools for KB writes. If every requirement is already covered, reply with a short summary of what you contributed — no further tool calls needed.\n\n")

	b.WriteString("**Contract:**\n")
	b.WriteString(trimmed)
	b.WriteString("\n\n")

	b.WriteString("**Important:** this is your final turn for KB work on this step. After this response, the step will be accepted regardless of any further gaps — there is no second review. Do not invent facts the step did not actually establish; partial coverage is better than fabricated coverage.\n")

	return b.String()
}
