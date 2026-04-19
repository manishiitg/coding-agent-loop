package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Paths used by the learn_code scoring fast path. Kept as constants so the prompt
// the builder writes, the runtime reader, and any docs all agree on the location
// and filenames.
const (
	// EvaluationScoringLearningsDir is the workspace-relative folder where the
	// saved main.py lives. Same "learnings/{stepID}" convention every step uses.
	EvaluationScoringLearningsDir = "learnings/" + EvaluationScoringStepID

	// EvaluationScoringMainPyName is the filename the scoring main.py must use.
	EvaluationScoringMainPyName = "main.py"

	// EvaluationScoringInputsFileName is the input file the fast path writes for
	// the script to consume (argv[1]).
	EvaluationScoringInputsFileName = "scoring_inputs.json"

	// ScoringLearnCodeMode is the declared_execution_mode value that opts scoring
	// into the learn_code path. Matches the per-step convention so builders
	// recognize it.
	ScoringLearnCodeMode = "learn_code"
)

// scoringInputsFile is the exact shape the Python main.py receives at argv[1].
// Keeping this as a typed struct means the on-disk contract is visible in code
// rather than stringly-typed everywhere.
type scoringInputsFile struct {
	GeneratedAt   string                `json:"generated_at"`
	TargetRunPath string                `json:"target_run_path,omitempty"` // absolute path to the workflow run being evaluated (resolved {{TARGET_RUN_PATH}}); main.py can read original artifacts from here
	Steps         []EvaluationStepInput `json:"steps"`
}

// tryScoringFastPath attempts to score the eval run by executing a pre-existing
// `learnings/__evaluation_scoring__/main.py` without any LLM involvement. The
// script must read the inputs JSON from argv[1] and write the report JSON to
// argv[2]; the produced file is then validated against the same fixed schema
// used by the LLM submit_report path, so a stale or broken script is rejected
// and the caller can fall back to the LLM path.
//
// Returns:
//   - (report, true, nil)   → fast path succeeded, skip the LLM
//   - (nil,    false, nil)  → no main.py present; caller should run the LLM
//   - (nil,    true, err)   → main.py existed but failed (exec error, invalid
//     output, or failed validation). Caller should log the
//     error and fall back to the LLM path.
func (hcpo *StepBasedWorkflowOrchestrator) tryScoringFastPath(
	ctx context.Context,
	stepInputs []EvaluationStepInput,
	schema *ValidationSchema,
	evalReportFolder string,
) (*EvaluationReport, bool, error) {
	mainPyRel := filepath.Join(EvaluationScoringLearningsDir, EvaluationScoringMainPyName)

	// Peek main.py via the workspace API first so we decide presence consistently
	// with the rest of the workflow code (which treats missing files as "not set").
	mainPyContent, err := hcpo.ReadWorkspaceFile(ctx, mainPyRel)
	if err != nil || strings.TrimSpace(mainPyContent) == "" {
		return nil, false, nil
	}

	docsRoot := GetPromptDocsRoot()
	workspacePath := hcpo.GetWorkspacePath()
	absMainPy := filepath.Join(docsRoot, workspacePath, mainPyRel)
	absInputs := filepath.Join(docsRoot, workspacePath, evalReportFolder, EvaluationScoringInputsFileName)
	absOutput := filepath.Join(docsRoot, workspacePath, evalReportFolder, EvaluationReportFileName)

	// Write inputs JSON via the workspace API so the path exists/creates correctly.
	inputs := scoringInputsFile{
		GeneratedAt:   time.Now().Format(time.RFC3339),
		TargetRunPath: hcpo.variableValues["TARGET_RUN_PATH"],
		Steps:         stepInputs,
	}
	inputsData, err := json.MarshalIndent(inputs, "", "  ")
	if err != nil {
		return nil, true, fmt.Errorf("failed to marshal scoring inputs: %w", err)
	}
	inputsRel := filepath.Join(evalReportFolder, EvaluationScoringInputsFileName)
	if err := hcpo.WriteWorkspaceFile(ctx, inputsRel, string(inputsData)); err != nil {
		return nil, true, fmt.Errorf("failed to write scoring inputs file: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scoring learn_code] Executing saved main.py: %s", absMainPy))

	// Run `python3 main.py <inputs> <output>`. Working directory is the learnings
	// folder so the script can import sibling modules (matching how step-level
	// learn_code scripts are run).
	cmd := exec.CommandContext(ctx, "python3", absMainPy, absInputs, absOutput)
	cmd.Dir = filepath.Join(docsRoot, workspacePath, EvaluationScoringLearningsDir)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return nil, true, fmt.Errorf("scoring main.py failed (%v):\n%s", runErr, truncateForLog(string(out), 4000))
	}
	if len(out) > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scoring learn_code] main.py output:\n%s", truncateForLog(string(out), 1000)))
	}

	// Validate the produced report against the same fixed schema the LLM path uses.
	// Reuses RunPreValidation so a stale main.py that silently drifts out of shape
	// is rejected here instead of corrupting the stored report.
	results, valErr := RunPreValidation(ctx, schema, evalReportFolder, hcpo.BaseOrchestrator)
	if valErr != nil {
		return nil, true, fmt.Errorf("scoring main.py output: validation engine error: %w", valErr)
	}
	if results == nil || !results.OverallPass {
		return nil, true, fmt.Errorf("scoring main.py output failed validation:\n%s", formatScoringValidationErrors(results))
	}

	// Parse the report. Only max_score is enriched here — step_title and
	// success_criteria are no longer part of EvaluationStepScore (UI consumers
	// look them up by step_id from evaluation_plan.json).
	reportRel := filepath.Join(evalReportFolder, EvaluationReportFileName)
	reportContent, err := hcpo.ReadWorkspaceFile(ctx, reportRel)
	if err != nil {
		return nil, true, fmt.Errorf("failed to read scoring output: %w", err)
	}
	parsed := &EvaluationReport{}
	if err := json.Unmarshal([]byte(reportContent), parsed); err != nil {
		return nil, true, fmt.Errorf("failed to parse scoring output: %w", err)
	}
	for _, s := range parsed.StepScores {
		if s == nil {
			continue
		}
		s.MaxScore = 10
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ [scoring learn_code] Fast path produced a valid report (%d step scores)", len(parsed.StepScores)))
	return parsed, true, nil
}

// truncateForLog caps a string at n runes for log safety.
func truncateForLog(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n…(truncated %d bytes)", len(s)-n)
}

// scoringLearnCodePromptSection is appended to the scoring agent's system prompt
// when declared_execution_mode is "learn_code". It tells the LLM that authoring
// a deterministic main.py at the conventional path will eliminate LLM involvement
// on future runs — same fast-path story step-level learn_code tells.
//
// mainPyAbsPath is the absolute filesystem path the LLM should write to —
// computed by the caller as docsRoot/workspacePath/learnings/__evaluation_scoring__/main.py.
// Relative paths are unsafe in shell here because the agent's cwd doesn't
// necessarily match the workspace root.
func scoringLearnCodePromptSection(mainPyAbsPath string) string {
	return `## Learn Code Mode — Author main.py for Future Runs

This scoring agent is in learn_code mode. The user explicitly chose this mode because they want repeated scoring to be deterministic, free, and reproducible — handled by a saved Python script instead of an LLM call on every eval run.

### What you MUST do this run
You have TWO outputs this run, not one:

1. **Author the deterministic scoring script** at this ABSOLUTE path:
   ` + "`" + mainPyAbsPath + "`" + `

2. **Write this run's evaluation_report.json** as instructed in your system prompt (also at the absolute path the user prompt gives you).

You can do these in either order. A common pattern: write main.py first, then run it against the inputs JSON locally to verify it produces a valid report, then use that output as your evaluation_report.json.

### When to SKIP main.py (rare)
Only skip authoring main.py if the eval steps genuinely cannot be scored deterministically — i.e. they require fuzzy LLM judgment that no regex / JSONPath / numeric comparison could replicate. Otherwise, default to writing the script — it's why the user enabled this mode.

### main.py contract
- Path: ` + "`" + mainPyAbsPath + "`" + ` (folder already exists; create the file there using shell)
- argv[1] = path to scoring_inputs.json:
  ` + "```json" + `
  {
    "generated_at":    "RFC3339 timestamp",
    "target_run_path": "absolute filesystem path — read this field; do not hardcode it",
    "steps": [
      { "id": "...", "title": "...", "description": "...", "execution_output": "..." }
    ]
  }
  ` + "```" + `
  ` + "`target_run_path`" + ` is the resolved absolute value of ` + "`{{TARGET_RUN_PATH}}`" + ` (the original execution folder for the run being scored). Read it from the inputs JSON at runtime — never hardcode a path in main.py, since the workspace root, group name, and host all vary. The eval step's ` + "`description`" + ` encodes what passing/failing looks like (the rubric used to live in a separate ` + "`success_criteria`" + ` field; that's been folded into description).
- argv[2] = absolute path where the script MUST write ` + "`evaluation_report.json`" + ` matching the same schema described in your system prompt: just a ` + "`step_scores`" + ` array of ` + "`{step_id, score, reasoning, evidence}`" + `. No ` + "`summary`" + ` field. Same min-length and score-range constraints apply.
- Standard library only. NO LLM API calls. NO non-stdlib network calls.
- Exit non-zero on failure so the controller falls back to this LLM path on the next run.

### How future runs use it
On every subsequent eval run, the controller writes scoring_inputs.json and execs ` + "`python3 " + mainPyAbsPath + " <inputs> <output>`" + ` directly. If the script succeeds and its output validates, the LLM is never invoked — the eval is fully deterministic. If the script fails or its output fails validation, the controller falls back to running this LLM scoring agent again, which gets to refine main.py.
`
}
