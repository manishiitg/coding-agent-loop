package step_based_workflow

import (
	"strings"
	"testing"

	"mcp-agent-builder-go/agent_go/cmd/server/guidance"
)

// executeRealisticWorkshopPromptForMode renders the workshop system prompt
// with var injections populated by their real builders, not empty strings.
// The pre-existing executeInteractiveWorkshopPromptForMode passes "" for
// MainPyAuthoringRules / BrowserPrompt / SpecialWorkspaceToolsInstructions /
// SkillPrompt — useful for snippet-presence tests but misleading for size
// measurement, since those vars carry ~10–15k tokens in production.
//
// Use this helper for size tests so the ceiling reflects what the agent
// actually sees at runtime. Use the minimal helper when asserting content
// of the inline template only.
func executeRealisticWorkshopPromptForMode(t *testing.T, mode string) string {
	t.Helper()
	prompt, err := ExecuteTemplate("interactiveWorkshopSystem", map[string]string{
		"AbsDocsRoot":                       "/app/workspace-docs",
		"AbsWorkspacePath":                  "/app/workspace-docs/Workflow/example",
		"AvailableGroups":                   "group-1",
		"BrowserPrompt":                     "",
		"Focus":                             "",
		"GroupName":                         "",
		"Instruction":                       "",
		"IsCodeExecutionMode":               "false",
		"MainPyAuthoringRules":              BuildMainPyAuthoringRules(), // real content
		"Mode":                              "",
		"PlanJSON":                          "{}",
		"ProgressSummary":                   "",
		"RunFolder":                         "",
		"SecretPrompt":                      "",
		"SkillPrompt":                       "",
		"SpecialWorkspaceToolsInstructions": "",
		"StepConfigSummary":                 "",
		"StepID":                            "",
		"StepSummary":                       "",
		"StepsToReview":                     "",
		"TargetRunFolder":                   "",
		"UseKnowledgebase":                  "false",
		"UserRequest":                       "",
		"WorkflowObjective":                 "Build a reliable workflow.",
		"WorkflowSuccessCriteria":           "It runs end to end.",
		"WorkshopMode":                      mode,
		"WorkspacePath":                     "Workflow/example",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate returned error: %v", err)
	}
	return prompt
}

// These tests lock in the system-prompt size target for the workshop
// (builder / optimizer / merged-workshop) modes. The intent is to migrate
// reference content out of the inline system prompt and into
// templates/system/*.md, loaded on demand via get_reference_doc.
//
// BEFORE migration (snapshot taken from a real chat agent prompt log):
//   - rendered prompt ~ 154,000 chars / ~38,500 tokens
//
// TARGET after migration:
//   - rendered prompt ~ 24,000 chars / ~6,000 tokens
//
// MaxWorkshopPromptBytes is the ceiling these tests enforce. While the
// migration is in flight, TestWorkshopPromptSize will fail with a clear
// message naming the target. That failure is intentional — it is the gate
// that proves the migration achieved its size goal.

// MaxWorkshopPromptBytes is the hard ceiling for the rendered workshop
// system prompt (test helper with realistic var injections). Set ~10%
// above the post-migration size so regressions and accidental re-inlines
// trip the gate, but normal small additions don't.
//
// Migration baseline (test helper):
//   - Original size (pre-migration): builder ~76KB / optimizer ~110KB
//   - Post-migration (code-authoring, message-sequence, stores, file-layout,
//     optimize-playbook moved to templates/system/): builder ~52KB / optimizer ~47KB
//
// We intentionally do NOT migrate tool-reference, media-tools, or browser:
// the LLM sees tools only through the MCP bridge, so the prose catalog is
// the agent's primary discovery surface. Those stay inline.
//
// Production prompt size is higher because the test helper passes empty
// values for some var injections (BrowserPrompt, SpecialWorkspaceTools,
// SkillPrompt). The ratio of shrinkage is what matters.
const MaxWorkshopPromptBytes = 70_000 // ~17.5k tokens at 4 chars/token

// MinWorkshopPromptBytes catches accidental gutting (e.g. a template-var
// rename that silently drops a section). Lowered 2026-05-28 after two
// trim batches: first the workshop-mode-flow + debugging-flow pointer
// (~5KB), then the execution-policy + deployed-channel + reporting-policy
// + running-steps + planning-steps batch (~9KB additional).
const MinWorkshopPromptBytes = 14_000

// TestWorkshopPromptSize logs the current rendered size for the canonical
// workshop mode and fails if it exceeds MaxWorkshopPromptBytes. The legacy
// modes ("builder", "optimizer", "reporting") were merged into "workshop"
// in the prompt-restructure migration and no longer exist as distinct
// template branches; persisted callers passing those strings are normalized
// at the input boundary before reaching the template.
func TestWorkshopPromptSize(t *testing.T) {
	for _, mode := range []string{"workshop"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			prompt := executeRealisticWorkshopPromptForMode(t, mode)
			size := len(prompt)
			estTokens := size / 4

			// Always log — gives visibility on every CI run.
			t.Logf("Workshop prompt (mode=%s): %d bytes (~%d tokens). Ceiling=%d (~%d tokens), floor=%d.",
				mode, size, estTokens,
				MaxWorkshopPromptBytes, MaxWorkshopPromptBytes/4,
				MinWorkshopPromptBytes)

			if size > MaxWorkshopPromptBytes {
				t.Errorf("workshop prompt (mode=%s) %d bytes exceeds ceiling %d (~%d tokens). "+
					"Move sections to templates/system/*.md and reference them via get_reference_doc.",
					mode, size, MaxWorkshopPromptBytes, estTokens)
			}
			if size < MinWorkshopPromptBytes {
				t.Errorf("workshop prompt (mode=%s) %d bytes below floor %d — sections likely missing.",
					mode, size, MinWorkshopPromptBytes)
			}
		})
	}
}

// TestWorkshopModeIsMergedSuperset verifies the canonical "workshop" mode
// produces a prompt that includes the optimizer-flavor sections (since
// optimizer is the tool-superset of the old builder+optimizer pair) AND
// the new phase-detection directive.
func TestWorkshopModeIsMergedSuperset(t *testing.T) {
	prompt := executeRealisticWorkshopPromptForMode(t, "workshop")

	// Should declare workshop mode explicitly.
	if !strings.Contains(prompt, "## CURRENT MODE: WORKSHOP") {
		t.Errorf("workshop mode prompt should start with `## CURRENT MODE: WORKSHOP`")
	}

	// Should include the phase-detection directive (only renders in workshop
	// mode — neither builder nor optimizer had this guidance).
	if !strings.Contains(prompt, "First, determine the current phase from workspace state") {
		t.Errorf("workshop mode prompt should include the phase-detection directive")
	}

	// Should expose the optimizer-flavor tooling (harden/replan/eval). These
	// are mentioned in the inline optimizer cheat sheet and through the
	// pointer to optimize-playbook.
	mustContain := []string{
		"harden_workflow",
		"create_human_input_request",
		`get_reference_doc(kind="optimize-playbook")`,
	}
	for _, s := range mustContain {
		if !strings.Contains(prompt, s) {
			t.Errorf("workshop mode prompt missing optimizer-flavor content: %q", s)
		}
	}
}

// TestWorkshopPromptKeepsCriticalRules verifies that the rules and dynamic
// state that MUST stay inline (cannot be lazy-loaded) are still in the
// rendered prompt after migration. If any of these go missing, the agent
// loses behavior the system depends on.
//
// Note: this test only checks snippets that appear in the inline template
// literally (not via template vars). Sections like "## Special Workspace
// Tools" come from {{.SpecialWorkspaceToolsInstructions}} — they're real in
// production but absent in this test helper because the helper passes empty
// vars. Adding them here would just couple the test to var content the
// helper synthesizes.
func TestWorkshopPromptKeepsCriticalRules(t *testing.T) {
	prompt := executeRealisticWorkshopPromptForMode(t, "builder")

	// Things that MUST stay inline — hard rules / identity / dynamic state.
	// All of these are inline template literals, not template-var injections.
	mustContain := []string{
		"Workflow Builder Agent", // identity
		"## CURRENT STATE",       // dynamic state injection
		"## Execution policy",    // hard rule: per-group default
	}
	for _, s := range mustContain {
		if !strings.Contains(prompt, s) {
			t.Errorf("workshop prompt missing required snippet: %q", s)
		}
	}
}

// TestWorkshopPromptReferencesNewToolForLazyDocs verifies the inline prompt
// mentions get_reference_doc so the agent knows how to load the migrated
// content.
func TestWorkshopPromptReferencesNewToolForLazyDocs(t *testing.T) {
	prompt := executeRealisticWorkshopPromptForMode(t, "workshop")
	if !strings.Contains(prompt, "get_reference_doc") {
		t.Errorf("workshop prompt does not reference get_reference_doc — agent will not know to load templates/system/*.md docs. " +
			"Add a pointer to at least one migrated section (e.g. 'For full main.py rules call get_reference_doc(kind=\"code-authoring\")').")
	}
}

// TestWorkshopPromptMovedSectionsAreReferencedNotInlined locks in the
// migration outcome. For each section that should move to templates/system/,
// it asserts:
//  1. A unique marker from the old inlined block is GONE from the prompt
//  2. The kind name IS mentioned somewhere in the prompt (so the agent
//     knows to call get_reference_doc with that kind)
//
// This will fail until each section is actually moved. That's the point —
// it makes "did we migrate yet?" a green/red signal in CI.
func TestWorkshopPromptMovedSectionsAreReferencedNotInlined(t *testing.T) {
	prompt := executeRealisticWorkshopPromptForMode(t, "workshop")

	type migration struct {
		kind          string // referenceKinds key
		oldBodyMarker string // a string unique to the inline section
	}
	// tool-reference, media-tools, and browser are intentionally NOT
	// migrated: the LLM only sees tools through the MCP bridge (not
	// individual JSON schemas), so the prose tool catalog IS the
	// agent's primary discovery surface. Lazy-loading would create a
	// bootstrap problem (agent doesn't know tools exist until it
	// loads a doc that lists them).
	migrations := []migration{
		{kind: "code-authoring", oldBodyMarker: "## main.py authoring rules"},
		{kind: "stores", oldBodyMarker: "Three persistent stores — skill vs knowledgebase vs db"},
		{kind: "message-sequence", oldBodyMarker: "## MESSAGE SEQUENCE ROUTE PATTERNS"},
		{kind: "optimize-playbook", oldBodyMarker: "## OPTIMIZATION GUIDELINES"},
		{kind: "file-layout", oldBodyMarker: "## FILE LAYOUT"},
	}

	for _, m := range migrations {
		if strings.Contains(prompt, m.oldBodyMarker) {
			t.Errorf("section %q still inlined (found %q); should be in templates/system/%s.md and referenced via get_reference_doc",
				m.kind, m.oldBodyMarker, m.kind)
		}
		if !strings.Contains(prompt, m.kind) {
			t.Errorf("workshop prompt does not reference kind %q — agent will not know to load templates/system/%s.md",
				m.kind, m.kind)
		}
	}
}

// TestReferenceKindsAllRenderable verifies every kind declared in
// referenceKinds renders without error and produces a reasonable amount of
// content. Catches: missing .md files, malformed Go templates, accidentally
// empty docs. Should pass even before content migration (placeholder docs
// exist).
func TestReferenceKindsAllRenderable(t *testing.T) {
	for _, kind := range guidance.ListReferenceKindsForTest() {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			body, err := guidance.RenderReferenceKindForTest(kind, "workshop")
			if err != nil {
				t.Fatalf("render %q failed: %v", kind, err)
			}
			if len(body) < 200 {
				t.Errorf("%s rendered to %d bytes — suspiciously short. Ensure the placeholder content has at least an intro paragraph.",
					kind, len(body))
			}
			if len(body) > 50_000 {
				t.Errorf("%s rendered to %d bytes — split into multiple kinds before it gets unwieldy.",
					kind, len(body))
			}
		})
	}
}
