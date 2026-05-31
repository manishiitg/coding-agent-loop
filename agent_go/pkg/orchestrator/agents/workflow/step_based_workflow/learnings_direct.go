package step_based_workflow

import (
	"strings"
	"sync"
)

// learningsGlobalFileMutex serializes direct-mode writes to learnings/_global/
// across parallel steps. Parallel sub-agents under a todo_task each have their
// own MCP session + folder guard, but the _global skill file is shared — without
// a mutex they'd race each other's diff_patches. Held for the duration of the
// direct-learnings continuation turn (see controller_execution.go).
//
// Uses a simple in-process mutex since the turn is inline and short.
//
// LIMITATION (intentional for v1): this is an in-process mutex. It does NOT
// serialize writes across multiple orchestrator processes sharing the same
// workspace (e.g. a multi-node deployment). If that topology becomes real,
// this needs to upgrade to a file lock (flock on learnings/_global/SKILL.md)
// or equivalent cross-process primitive. Not addressed in v1.
var learningsGlobalFileMutex sync.Mutex

// BuildLearningsContributionTurn returns the scripted user message that fires
// one-shot after pre-validation (and after any KB review turn) when the step
// is configured for direct-mode learnings writes. All SKILL.md guidance lives
// in this message — the step's system prompt deliberately says nothing about
// direct-mode learnings, so the agent can focus on the main task during
// execution and switch context cleanly when this turn arrives.
//
// Writes target learnings/_global/SKILL.md — the single global workflow skill
// shared across all steps. Multiple direct-mode steps contribute scoped sections
// to the same file; the serialization mutex prevents parallel writes from
// racing.
//
// Learn-code note: the step's main.py is copied to learnings/<stepID>/code/
// automatically by Go code (controller_execution.go:2672 via
// saveScriptedScriptToLearnings), independent of both the post-step learning
// agent and this direct-mode turn. The step agent is NOT asked to do that copy
// here — that would double-write a shared file and open needless write access
// to learnings/<stepID>/. Direct-mode learnings only targets _global/ for
// author-authored domain knowledge beyond what main.py encodes.
//
// Returns empty when the step shouldn't enter direct-learnings — callers decide
// via shouldDirectWriteLearnings before invoking this.
func BuildLearningsContributionTurn(stepID, stepDescription, learningObjective string, isScriptedMode bool) string {
	_ = isScriptedMode // retained in the signature in case future behavior diverges by mode; not currently referenced
	description := strings.TrimSpace(stepDescription)
	objective := strings.TrimSpace(learningObjective)
	if stepID == "" || objective == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Learnings Contribution (dedicated turn)\n\n")
	b.WriteString("Your main-step work is complete and pre-validation passed. Now — in this turn only — you have WRITE access to `learnings/_global/`. Your job for this turn is to capture HOW to run this task well, so future runs don't have to rediscover what you just worked out.\n\n")

	b.WriteString("**Target:** `learnings/_global/SKILL.md` plus linked files under `learnings/_global/references/` — the single global runbook shared across every step of this workflow. You are appending this step's contribution, not owning the folder.\n\n")

	if description != "" {
		b.WriteString("**Current step description (source of truth for stale-learning cleanup):**\n")
		b.WriteString(description)
		b.WriteString("\n\n")
	}

	b.WriteString("**Frontmatter (top of SKILL.md):** preserve existing frontmatter if the file exists. If you're creating it fresh, use:\n")
	b.WriteString("```\n")
	b.WriteString("---\n")
	b.WriteString("name: <workflow name from your current context>\n")
	b.WriteString("description: \"<Summary of accumulated HOW-to-run knowledge for this workflow>\"\n")
	b.WriteString("disable-model-invocation: true\n")
	b.WriteString("user-invocable: false\n")
	b.WriteString("---\n")
	b.WriteString("```\n\n")

	b.WriteString("**Write rules (critical — you are writing to a shared file):**\n")
	b.WriteString("1. **Read first.** `cat learnings/_global/SKILL.md` and `ls learnings/_global/references/` (if it exists). Understand the existing structure — what topic files already cover which areas — before you write anything.\n")
	b.WriteString("2. **Patch surgically, never rewrite.** Use `diff_patch_workspace_file` for existing files. Add your observations to the topic file they belong to (e.g. `references/auth-flow.md`, `references/selectors.md`) rather than dumping them into SKILL.md. If no existing topic fits, create a new `references/<topic>.md` file. **Never rewrite SKILL.md wholesale** — you'd destroy contributions from other steps.\n")
	b.WriteString("3. **SKILL.md stays lean (under ~80-100 lines).** It is only the index/overview: frontmatter, a brief scope note, and links to focused reference files. Detailed HOW-to-run content from this step run belongs in `references/<topic>.md`, not in SKILL.md itself.\n")
	b.WriteString("4. **Reference files hold the details.** Store selectors, auth flows, API quirks, timing/wait rules, file-format notes, retry patterns, browser gotchas, and step-specific HOW guidance in topic files under `references/`. If you create a new reference file, add or update a short link in SKILL.md so future agents can discover it.\n")
	b.WriteString("5. **Reconcile stale guidance.** Compare the existing reference content you touch against the current step description above, current step behavior, and this step's learning objective. If an old note clearly describes a previous step description, obsolete selector/API path, or behavior contradicted by this successful run, remove or replace that stale note in the same patch. Do not delete unrelated shared guidance just because this step didn't use it.\n")
	b.WriteString("6. **Merge with existing knowledge, don't duplicate.** If the lesson you'd write overlaps with a pattern another step already captured in an existing references file, extend that file (append a new section, refine an existing one) rather than creating a second place for the same knowledge.\n")
	b.WriteString("7. **No ephemeral refs.** Do not save session-local browser handles (`@e1`, `e68`, etc.) — they are useless across runs.\n")
	b.WriteString("8. **No fabrication.** Capture only patterns you actually used in this execution. If you're unsure whether a pattern is reliable, say so explicitly in the note.\n\n")

	b.WriteString("**Objective for this step's contribution (the contract):**\n")
	b.WriteString(objective)
	b.WriteString("\n\n")

	b.WriteString("**Important:**\n")
	b.WriteString("- This is your final turn for learnings on this step. After your reply, the step is accepted regardless of whether every gap is closed — there is no second learnings pass.\n")
	b.WriteString("- If there's genuinely nothing new worth capturing (e.g. the step was trivial and the existing SKILL.md already covers it), do NOT force an edit. Reply briefly that no learning changes were needed and why.\n")
	b.WriteString("- If you did update files, end with exactly one summary line: `Learnings updated: files changed: <comma-separated file list>`.\n")
	b.WriteString("- Available tools: `execute_shell_command` (for `cat`, `mkdir`, `cp`, heredoc creation), `diff_patch_workspace_file` (for updating existing files).\n")

	return b.String()
}
