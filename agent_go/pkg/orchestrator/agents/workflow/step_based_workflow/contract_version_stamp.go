package step_based_workflow

import (
	"fmt"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/contractupgrade"
)

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
	// What still has to hold is the ladder. Without a scheduler grant pinning
	// the target, nothing otherwise stops a stamp jumping straight to the
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
	return "Refused: no contract upgrade turn is open for this session, so there is no version to stamp. " +
		"This tool is accepted only inside the scheduler's upgrade turn, for the version that turn asked for. " +
		"If an earlier upgrade turn reported a blocker, resolve the blocker and let the next preflight re-run that turn — " +
		"stamping from a later turn does not complete the migration, it makes the scheduler skip it.", false
}
