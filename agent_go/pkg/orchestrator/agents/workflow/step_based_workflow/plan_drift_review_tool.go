package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpexecutor "github.com/manishiitg/mcpagent/executor"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// PLAT-258 phase 3. record_plan_drift_review is the write half of the
// plan-drift review: the reviewer turn (automatic, Pulse-triggered, or a
// manual review-artifact-drift invocation) calls this once per step to
// persist what it found. planning/step_config.json is FolderGuard-blocked-
// write for a normal session, so this must be a purpose-built tool
// registered with the same privileged writeFile callback
// registerPlanModificationTools already wraps for every other plan-mod tool
// (withPlanMutationWriteAccess) -- not a raw file write, which would be
// denied the same way a step-execution session's shell tools already are.

const (
	stepDriftCheckStatusPass  = "pass"
	stepDriftCheckStatusFail  = "fail"
	stepDriftCheckStatusFixed = "fixed"
)

// minPlanDriftCheckEvidenceLength is a low, deliberately non-strict floor —
// not a quality bar, just enough to reject the single-word "ok"/"fine"/
// "n/a" that a rubber-stamped review would produce. This is the concrete
// enforcement of the "evidence-required, not a boolean" design: the schema
// itself refuses a check with no real description of what was compared.
const minPlanDriftCheckEvidenceLength = 15

func validateStepDriftChecks(checks []StepDriftCheck) error {
	if len(checks) == 0 {
		return fmt.Errorf("checks must contain at least one entry — record every check that ran, including ones that passed, not just failures")
	}
	for i, c := range checks {
		if strings.TrimSpace(c.CheckID) == "" {
			return fmt.Errorf("checks[%d].check_id is required", i)
		}
		switch c.Status {
		case stepDriftCheckStatusPass, stepDriftCheckStatusFail, stepDriftCheckStatusFixed:
		default:
			return fmt.Errorf("checks[%d].status must be one of pass, fail, fixed (got %q)", i, c.Status)
		}
		evidence := strings.TrimSpace(c.Evidence)
		if len(evidence) < minPlanDriftCheckEvidenceLength {
			return fmt.Errorf(
				"checks[%d].evidence must describe what was actually compared and what was found, not a placeholder (got %q — need at least %d characters)",
				i, c.Evidence, minPlanDriftCheckEvidenceLength,
			)
		}
		if c.Status == stepDriftCheckStatusFail && strings.TrimSpace(c.FindingID) == "" {
			return fmt.Errorf(
				"checks[%d].finding_id is required when status is \"fail\" — call record_pulse_finding first and pass its issue_id here, so a failed check can never be recorded as reviewed without a corresponding repair item already existing",
				i,
			)
		}
	}
	return nil
}

func validateStepTypeDriftChecks(stepType string, checks []StepDriftCheck) error {
	requiredCheckID := requiredStepTypeBestPracticesCheckID(stepType)
	if requiredCheckID == "" {
		return nil
	}
	if requiredCheckID == orchestratorBestPracticesDriftCheckID {
		// Records reviewed before contract v1.0.35 carry the legacy ID.
		for _, check := range checks {
			if strings.TrimSpace(check.CheckID) == legacyOrchestratorBestPracticesDriftCheckID {
				return nil
			}
		}
	}
	for _, check := range checks {
		if strings.TrimSpace(check.CheckID) == requiredCheckID {
			return nil
		}
	}
	return fmt.Errorf(
		"%s step reviews must include check_id %q after reading the matching builder-reference step-type guidance",
		strings.TrimSpace(stepType), requiredCheckID,
	)
}

func planDriftStepType(ctx context.Context, workspacePath, stepID string, readFile func(context.Context, string) (string, error)) string {
	if stepID == WorkflowDriftReviewStepID {
		return "workflow"
	}
	planPath := normalizePathForWorkspaceAPI("planning/plan.json", workspacePath)
	content, err := readFile(ctx, planPath)
	if err != nil || strings.TrimSpace(content) == "" {
		return ""
	}
	var plan PlanningResponse
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return ""
	}
	stepTypes := map[string]string{}
	collectStepTypesByID(plan.Steps, stepTypes)
	return stepTypes[stepID]
}

// pulseFindingInactiveStatuses mirrors the "active" boundary used elsewhere
// in this package (e.g. run_concerns.go's active-issue filter): a finding
// that has been closed out no longer represents unresolved repair work, so
// referencing one is exactly as fraudulent as referencing an unrelated
// finding — it does not prove anything is currently being tracked.
var pulseFindingInactiveStatuses = map[string]bool{
	"resolved": true,
	"rejected": true,
}

// verifyStepDriftCheckFindingsExist confirms every fail-status check's
// finding_id resolves to a real Pulse finding that is (a) currently active
// (not resolved/rejected — a closed-out finding proves nothing is still
// being tracked) and (b) filed against this exact step (stepID) — not merely
// any finding that happens to exist anywhere in the workspace. Both
// conditions together are what "belongs to this exact drift failure" means:
// a fabricated id, a resolved id, or a real-but-unrelated-step's id are all
// rejected the same way.
func verifyStepDriftCheckFindingsExist(ctx context.Context, workspacePath, stepID string, checks []StepDriftCheck) error {
	var wantIDs []string
	for _, c := range checks {
		if c.Status == stepDriftCheckStatusFail {
			wantIDs = append(wantIDs, strings.TrimSpace(strings.ToUpper(c.FindingID)))
		}
	}
	if len(wantIDs) == 0 {
		return nil
	}
	findings, err := LoadPulseFindingLifecycles(ctx, workspacePath, "", -1)
	if err != nil {
		return fmt.Errorf("failed to verify finding_id references against the Pulse backlog: %w", err)
	}
	activeForStep := make(map[string]bool, len(findings))
	for _, f := range findings {
		id := strings.TrimSpace(strings.ToUpper(f.IssueID))
		if id == "" || f.StepID != stepID {
			continue
		}
		if pulseFindingInactiveStatuses[strings.ToLower(strings.TrimSpace(f.Status))] {
			continue
		}
		activeForStep[id] = true
	}
	for _, id := range wantIDs {
		if !activeForStep[id] {
			return fmt.Errorf(
				"finding_id %q does not match any active Pulse finding filed against step %q — it must be a real, currently-tracked (not resolved/rejected) finding for this exact step, not a fabricated id, a closed-out finding, or one filed against a different step. Call record_pulse_finding first, then pass its real issue_id here",
				id, stepID,
			)
		}
	}
	return nil
}

func getRecordPlanDriftReviewSchema() string {
	return `{
		"type": "object",
		"properties": {
			"step_id": {
				"type": "string",
				"description": "The step's id field from plan.json."
			},
			"checks": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"check_id": {
							"type": "string",
							"description": "Stable check identifier. For the deterministic checks, use their real names: report_query_compatibility, validation_schema_db_rules, validation_schema_file_rules, scripted_code_db_queries, db_readme_contract, orphaned_tables. Step types require their matching reference-backed check: scripted_best_practices, message_sequence_best_practices, orchestrator_best_practices (legacy orchestrator_best_practices accepted), routing_best_practices, or branch_best_practices. For each due real step, also record step_prompt_quality after applying references/step-description.md to its description, item prompts, supplied schema, and accessible guidance. Reuse prior prompt evidence only when those inputs remain unchanged. Scope type-specific checks to compatibility, not unsolicited architecture redesign. Other checks need a clear stable id for an affected dependency, e.g. step_description_accuracy or learnings_content_staleness."
						},
						"status": {
							"type": "string",
							"enum": ["pass", "fail", "fixed"],
							"description": "fixed means real drift was found AND a repair was applied in this same review pass, not merely flagged."
						},
						"evidence": {
							"type": "string",
							"description": "What was actually compared and what was found — specific, not a bare verdict. E.g. 'compared description sentence 2 (\"reads leads table\") against the step's actual tool config, which only reads campaigns; description is stale' — not 'reviewed, looks fine'."
						},
						"finding_id": {
							"type": "string",
							"description": "Required when status is \"fail\": the durable issue_id (e.g. \"PUL-xxxxxxxx\") returned by record_pulse_finding for this exact failure. Call record_pulse_finding BEFORE this tool for every failing check, then pass its real issue_id here — a fabricated or missing id is rejected. This is what keeps a failed check from being marked reviewed with no corresponding repair item in the backlog."
						}
					},
					"required": ["check_id", "status", "evidence"]
				},
				"description": "One entry per check that ran against this step in this review pass, including checks that passed — not only failures. A step's full set of checks (deterministic + judgment) should normally all be recorded together in one call."
			},
			"reviewed_by": {
				"type": "string",
				"description": "Optional actor label, e.g. \"pulse:plan_drift_review\" for the automatic Pulse module, or left unset for a manual review-artifact-drift invocation (defaults to the current session)."
			},
			"reviewed_through_change_id": {
				"type": "string",
				"description": "Optional: the latest planning/changelog change_id this review actually consumed, if the step had prior evidence and you read the changelog entries since its last review. Persisted so a later review knows where to resume reading changelog history from. Leave unset if this step had no prior review or no relevant changelog entries existed."
			}
		},
		"required": ["step_id", "checks"]
	}`
}

// createRecordPlanDriftReviewExecutor mirrors the shape of every other
// plan-mod tool in this file (workspacePath/logger/readFile/writeFile in,
// executor func out) so it registers through registerPlanModificationTools
// exactly like update_step_config, cleanup_orphan_step_configs, etc. — same
// privileged write path, same conventions, nothing bespoke.
func createRecordPlanDriftReviewExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID := strings.TrimSpace(asString(args["step_id"]))
		if stepID == "" {
			return "", fmt.Errorf("step_id is required")
		}

		rawChecks, err := json.Marshal(args["checks"])
		if err != nil {
			return "", fmt.Errorf("failed to encode checks: %w", err)
		}
		var checks []StepDriftCheck
		if err := json.Unmarshal(rawChecks, &checks); err != nil {
			return "", fmt.Errorf("checks must be an array of {check_id, status, evidence}: %w", err)
		}
		if err := validateStepDriftChecks(checks); err != nil {
			return "", err
		}
		stepType := planDriftStepType(ctx, workspacePath, stepID, readFile)
		if err := validateStepTypeDriftChecks(stepType, checks); err != nil {
			return "", err
		}
		if err := verifyStepDriftCheckFindingsExist(ctx, workspacePath, stepID, checks); err != nil {
			return "", err
		}

		reviewedBy := strings.TrimSpace(asString(args["reviewed_by"]))
		if reviewedBy == "" {
			if sessionID := mcpexecutor.SessionIDFromContext(ctx); sessionID != "" {
				reviewedBy = "session:" + sessionID
			} else {
				reviewedBy = "plan_drift_review"
			}
		}

		reviewedThroughChangeID := strings.TrimSpace(asString(args["reviewed_through_change_id"]))

		configs, err := readStepConfigViaFileCallback(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read step_config.json: %w", err)
		}

		now := time.Now().UTC().Format(time.RFC3339)
		// NeedsReview is explicitly false: this is what actually clears the
		// step from plan_drift_review's due list. A completed review always
		// fully replaces the prior evidence — the stale flag model never keeps
		// an old check result "pending" alongside a new one.
		record := &StepDriftReview{
			NeedsReview:             false,
			ReviewedAt:              now,
			ReviewedBy:              reviewedBy,
			ReviewedThroughChangeID: reviewedThroughChangeID,
			Checks:                  checks,
			ContractVersion:         planDriftReviewContractVersion,
		}

		found := false
		for i := range configs {
			if configs[i].ID != stepID {
				continue
			}
			found = true
			if configs[i].AgentConfigs == nil {
				configs[i].AgentConfigs = &AgentConfigs{}
			}
			configs[i].AgentConfigs.DriftReview = record
			break
		}
		if !found {
			// A step can legitimately have no prior step_config.json entry (its
			// config has always used preset defaults) — create one rather than
			// erroring, mirroring how update_step_config handles a first write.
			configs = append(configs, StepConfig{ID: stepID, AgentConfigs: &AgentConfigs{DriftReview: record}})
		}

		if err := writeStepConfigViaFileCallback(ctx, workspacePath, configs, writeFile); err != nil {
			return "", fmt.Errorf("failed to write step_config.json: %w", err)
		}

		var passed, failed, fixed int
		for _, c := range checks {
			switch c.Status {
			case stepDriftCheckStatusPass:
				passed++
			case stepDriftCheckStatusFail:
				failed++
			case stepDriftCheckStatusFixed:
				fixed++
			}
		}
		logger.Info(fmt.Sprintf("📋 record_plan_drift_review: step=%q checks=%d pass=%d fail=%d fixed=%d by=%s", stepID, len(checks), passed, failed, fixed, reviewedBy))
		return fmt.Sprintf(
			"Recorded drift_review for step %q: %d check(s) (%d pass, %d fail, %d fixed). This clears the step from plan_drift_review's due list until it is edited again.",
			stepID, len(checks), passed, failed, fixed,
		), nil
	}
}
