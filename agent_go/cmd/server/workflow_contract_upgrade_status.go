package server

import (
	"context"
	"fmt"
	"strings"
)

// describeWorkflowContractUpgrades renders the workflow's pending contract
// migrations for its owner.
//
// The upgrade instructions are Go constants that only the scheduler delivers,
// so none of this was reachable by a human. A blocked workflow surfaced one
// line in its run history —
//
//	workflow upgrade preflight upgrade-current-artifact-contract did not stamp
//	required version "1.0.21" (found "1.0.20", failure 2/3 consecutive)
//
// — which names a version and nothing else. Not what the migration asks for,
// not which rungs remain behind it, not why the last attempt stopped.
// confida-login sat blocked for days and diagnosing it meant reading server
// logs and session transcripts by hand.
//
// The instruction text is included in full and deliberately. It is what the
// scheduler sends; an owner deciding whether a stalled migration is safe needs
// to read the actual words, not a summary of them.
func describeWorkflowContractUpgrades(ctx context.Context, workspacePath string) (string, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return "No workspace path associated with this workflow session.", nil
	}

	manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil {
		return fmt.Sprintf("Could not read workflow.json: %v", err), nil
	}
	if !found {
		return "No workflow manifest found.", nil
	}

	current := workflowContractVersionForUpgrade(manifest)
	var sb strings.Builder
	sb.WriteString("## Workflow contract version\n\n")
	sb.WriteString(fmt.Sprintf("- Current: `%s`\n- Platform current: `%s`\n\n", current, WorkflowContractCurrentVersion))

	if _, known := workflowContractVersionRank(current); !known && current != WorkflowContractCurrentVersion {
		sb.WriteString(fmt.Sprintf("**This version is not one this server knows.** Version ranks are a closed set so a workflow written by a newer server is never silently downgraded by an older one. There is no upgrade path from %q, and every scheduled run will refuse to start until that is resolved.\n", current))
		return sb.String(), nil
	}

	pending := workflowVersionUpgradePlan(manifest)
	if len(pending) == 0 {
		sb.WriteString("No pending migrations — this workflow is execution-compatible with the current contract. Retired no-op checkpoints do not block it.\n")
		return sb.String(), nil
	}

	sb.WriteString(fmt.Sprintf("## Pending migrations (%d)\n\n", len(pending)))
	sb.WriteString("They run one per turn, in this order, as a blocking preflight before the workflow's own schedule messages. A rung that does not complete stops the ones behind it.\n\n")
	for i, upgrade := range pending {
		sb.WriteString(fmt.Sprintf("### %d. %s → `%s`\n\n", i+1, upgrade.label, upgrade.to))
		sb.WriteString("```text\n")
		sb.WriteString(strings.TrimSpace(bindWorkflowUpgradeWorkspacePath(upgrade.query, workspacePath)))
		sb.WriteString("\n```\n\n")
	}

	sb.WriteString(describeContractUpgradeFailures(ctx, workspacePath, manifest))
	return sb.String(), nil
}

// nextWorkflowContractUpgrade returns the single migration this workflow owes
// next. An empty target means it is already current, or on a version this
// server has no path from — neither of which an operator stamp should paper
// over.
func nextWorkflowContractUpgrade(ctx context.Context, workspacePath string) (string, string, error) {
	manifest, found, err := ReadWorkflowManifest(ctx, strings.TrimSpace(workspacePath))
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", fmt.Errorf("workflow manifest not found at %s", workspacePath)
	}
	pending := workflowVersionUpgradePlan(manifest)
	if len(pending) == 0 {
		return "", "", nil
	}
	return pending[0].to, pending[0].label, nil
}

// describeContractUpgradeFailures reports what the scheduler has recorded about
// failed attempts, per schedule. The counter is per-schedule while the version
// is per-workflow, so one schedule can be at its fail-open threshold while
// another still blocks — which changes what the next run of each will do.
func describeContractUpgradeFailures(ctx context.Context, workspacePath string, manifest *WorkflowManifest) string {
	history, err := ReadWorkflowScheduleExecutionHistory(ctx, workspacePath)
	if err != nil || history == nil || len(history.Schedules) == 0 {
		return ""
	}

	names := map[string]string{}
	if manifest != nil {
		for _, sched := range manifest.Schedules {
			names[sched.ID] = sched.Name
		}
	}

	var rows []string
	for id, tracker := range history.Schedules {
		if tracker.PreflightFailureCount == 0 {
			continue
		}
		name := names[id]
		if strings.TrimSpace(name) == "" {
			name = id
		}
		outcome := "the run stops here and retries next trigger"
		if tracker.PreflightFailureCount >= workflowSchedulePreflightFailOpenThreshold {
			outcome = fmt.Sprintf("**at the fail-open threshold (%d)** — the next failure skips every remaining migration and runs the workflow on the unstamped contract", workflowSchedulePreflightFailOpenThreshold)
		}
		rows = append(rows, fmt.Sprintf("- **%s** — %d consecutive failure(s) stamping `%s`, last at %s. On the next failure, %s.",
			name, tracker.PreflightFailureCount, tracker.PreflightFailureTarget,
			tracker.PreflightFailureAt.UTC().Format("2006-01-02 15:04 MST"), outcome))
	}
	if len(rows) == 0 {
		return "No failed attempts recorded — these migrations have not been tried yet.\n"
	}

	var sb strings.Builder
	sb.WriteString("## Recorded failures\n\n")
	sb.WriteString(strings.Join(rows, "\n"))
	sb.WriteString("\n\nThe counter clears only on a successful stamp; failing open does not reset it.\n")
	sb.WriteString("\nThis records *that* an attempt failed, not why. The reason is in that run's own conversation — a migration can stop for a legitimate blocker it found, and that judgement lives in the transcript rather than here.\n")
	return sb.String()
}
