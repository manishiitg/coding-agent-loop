// Package schedulepolicy defines the shared schedule Pulse authoring contract.
package schedulepolicy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const ExplicitPulseContractVersion = "1.0.41"

func RequiresExplicitPulse(version string) bool {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) != 3 {
		return false
	}
	var values [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return false
		}
		values[i] = n
	}
	return values[0] > 1 || (values[0] == 1 && (values[1] > 0 || values[2] >= 41))
}

// LegacyFullPulseMode is no longer authorable on a normal schedule. The full
// Pulse review runs on the workflow's own self-deciding Pulse schedule
// (workflow.json pulse.schedule); a persisted legacy "full" reads as "basic".
const LegacyFullPulseMode = "full"

// NormalizePulse maps a persisted schedule pulse_mode to its effective value.
func NormalizePulse(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == LegacyFullPulseMode {
		return "basic"
	}
	return mode
}

// ValidatePulse is the authoring contract for a normal schedule's pulse_mode.
func ValidatePulse(mode, reason string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "off", "basic":
	case LegacyFullPulseMode:
		return fmt.Errorf("pulse_mode full is retired: the full Pulse review runs on the workflow's own Pulse schedule (workflow.json pulse.schedule), not after a normal run; use basic (backup, publish, notify) or off")
	default:
		return fmt.Errorf("pulse_mode is required and must be off or basic; choose explicitly for this schedule")
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("pulse_mode_reason is required; explain this schedule's purpose, frequency and review needs")
	}
	return nil
}

// ValidatePulseStamp checks the persisted schedules before acknowledging the
// migration. It intentionally includes disabled and calendar schedules.
func ValidatePulseStamp(content []byte, target string) error {
	if !RequiresExplicitPulse(target) {
		return nil
	}
	var manifest struct {
		Schedules []struct {
			ID     string `json:"id"`
			Mode   string `json:"pulse_mode"`
			Reason string `json:"pulse_mode_reason"`
		} `json:"schedules"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return err
	}
	for i, s := range manifest.Schedules {
		// Persisted legacy "full" is tolerated here; it reads as basic.
		if err := ValidatePulse(NormalizePulse(s.Mode), s.Reason); err != nil {
			return fmt.Errorf("schedules[%d] (%s): %w", i, s.ID, err)
		}
	}
	return nil
}
