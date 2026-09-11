package step_based_workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

const scriptedRefusalPrefix = "AGENTWORKS_REFUSAL:"

type scriptedRefusalDetails struct {
	Reason        string `json:"reason"`
	BlockedAction string `json:"blocked_action"`
	Resolution    string `json:"resolution"`
}

// Only an explicit, complete record can describe the script's reported intent.
// Neither arbitrary log prose nor exit code 2 proves a safety guard fired.
// Invalid or ambiguous records still stop execution; they never enable repair.
func scriptedTerminalStopError(stepID, output string) error {
	var detail scriptedRefusalDetails
	records := 0
	valid := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, scriptedRefusalPrefix) {
			continue
		}
		records++
		var candidate scriptedRefusalDetails
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, scriptedRefusalPrefix))), &candidate) != nil {
			continue
		}
		candidate.Reason = strings.TrimSpace(candidate.Reason)
		candidate.BlockedAction = strings.TrimSpace(candidate.BlockedAction)
		candidate.Resolution = strings.TrimSpace(candidate.Resolution)
		if candidate.Reason != "" && candidate.BlockedAction != "" && candidate.Resolution != "" {
			detail, valid = candidate, true
		}
	}

	summary := "No valid explicit refusal record was supplied; the exit code alone does not establish why the script stopped."
	if records == 1 && valid {
		summary = fmt.Sprintf("The script reported a refusal. Reason: %s\nBlocked action: %s\nResolution: %s", detail.Reason, detail.BlockedAction, detail.Resolution)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		output = "(no output captured)"
	}
	return fmt.Errorf("scripted step %q stopped with exit code %d. %s\n"+
		"Automatic repair was withheld. Investigate the cause before retrying; do not bypass a guard or perform a refused action.\nCaptured output:\n%s",
		stepID, ScriptedTerminalRefusalExitCode, summary, output)
}
