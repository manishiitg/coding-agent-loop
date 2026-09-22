package step_based_workflow

import (
	"fmt"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/contractupgrade"
)

const contractUpgradeHistoryField = "contract_upgrade_history"

// appendContractUpgradeHistory records the first successful stamp time for a
// contract rung. The version remains the authoritative execution marker; this
// ledger is display/audit metadata for migrations completed after tracking was
// introduced.
func appendContractUpgradeHistory(manifest map[string]interface{}, version string, appliedAt time.Time) error {
	raw := manifest[contractUpgradeHistoryField]
	history := make([]interface{}, 0)
	if raw != nil {
		var ok bool
		history, ok = raw.([]interface{})
		if !ok {
			return fmt.Errorf("%s must be an array", contractUpgradeHistoryField)
		}
	}
	for _, rawEntry := range history {
		entry, ok := rawEntry.(map[string]interface{})
		if !ok {
			continue
		}
		if stampedVersion, _ := entry["version"].(string); stampedVersion == version {
			return nil
		}
	}
	manifest[contractUpgradeHistoryField] = append(history, map[string]interface{}{
		"version":    version,
		"applied_at": appliedAt.UTC().Format(time.RFC3339),
	})
	return nil
}

// NestedAgentArtifactsContractVersion is shared with the server's workflow
// contract ladder because the stamp executor must enforce this migration's
// source-layout prerequisite, not merely describe it in a prompt.
const NestedAgentArtifactsContractVersion = "1.0.44"

func validateContractVersionStampPrerequisites(version string, manifest map[string]interface{}) error {
	if version != NestedAgentArtifactsContractVersion {
		return nil
	}
	codeLayout, ok := manifest["code_layout_version"].(float64)
	if !ok || codeLayout != 1 {
		return fmt.Errorf("contract %s requires code_layout_version=1; migrate every scripted bundle to code/<step-id>/ and call set_code_layout_version(code_layout_version=1) before stamping", version)
	}
	return nil
}

// authorizeContractVersionStamp decides whether sessionID may stamp version
// right now, spending the authorization when it may. The second return value
// reports permission; the first is the refusal to hand back when it does not.
//
// This bound lives in the executor rather than in the upgrade prompt because
// the stamp does not only arrive as a native tool call. On confida-login
// 2026-08-12 it arrived as
//
//	curl -X POST -d '{"version":"1.0.21"}' -H "$MCP_AUTH" "$MCP_CUSTOM/set_workflow_contract_version"
//
// run through execute_shell_command from a Pulse turn, ten minutes after the
// upgrade turn that owed that stamp had been adjudicated and closed. Both paths
// land here, so both are covered; no wording in a prompt could have covered the
// shell one.
func authorizeContractVersionStamp(sessionID, version string, nextPending func() (string, string, error)) (string, bool) {
	// An operator working in the workflow builder is the authorization. They
	// asked for the migration and can see what the agent does, so there is
	// nothing to forge. Binding them too removed the only way a person could
	// unblock a stalled upgrade by hand — which is exactly what a workflow that
	// keeps declining its own migration needs.
	//
	// What still has to hold is the ladder. Without checking the next pending
	// target, nothing otherwise stops a stamp jumping straight to the
	// newest version and skipping three migrations whose work was never done.
	if !contractupgrade.IsScheduled(sessionID) {
		if nextPending == nil {
			return "", true
		}
		target, label, err := nextPending()
		if err != nil {
			// A lookup failure is not evidence of a bad stamp; do not block the
			// operator's only manual route on it.
			return "", true
		}
		switch {
		case target == "":
			return "Refused: this workflow owes no contract migration, so there is nothing to stamp. Use get_contract_upgrades to confirm what it is on.", false
		case target != version:
			return fmt.Sprintf(
				"Refused: the next migration this workflow owes is %s (%s), not %s. Migrations are stamped one at a time, in order — stamping %s now would record %s as done without its work being performed. Use get_contract_upgrades, complete %s, then stamp it.",
				target, label, version, version, target, target,
			), false
		}
		return "", true
	}
	if contractupgrade.Consume(sessionID, version) {
		return "", true
	}
	if granted := contractupgrade.Granted(sessionID); granted != "" {
		return fmt.Sprintf(
			"Refused: this turn is authorized to stamp %s, not %s. Stamp the version its own upgrade instruction named.",
			granted, version,
		), false
	}
	return "Refused: schedules may run an older workflow contract, but they never authorize or perform migrations. " +
		"Open the workflow chat and use Update workflow so an operator can supervise each migration in order.", false
}
