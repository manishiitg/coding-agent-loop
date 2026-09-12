package step_based_workflow

import (
	"path/filepath"
	"regexp"
	"strings"
)

// workshopInternalRunFolderForTarget derives an internal "iteration-0/<group>" folder
// for workshop-style phase executions that run inside a per-target sandbox (e.g.
// evaluation) rather than the target run itself.
// Example: "iteration-3/acme" → "iteration-0/acme".
func workshopInternalRunFolderForTarget(targetRunFolder string) string {
	targetRunFolder = filepath.ToSlash(strings.TrimSpace(targetRunFolder))
	if targetRunFolder == "" {
		return "iteration-0"
	}
	parts := strings.Split(targetRunFolder, "/")
	if regexp.MustCompile(`^iteration-[0-9]+-hook$`).MatchString(parts[0]) && len(parts) <= 2 && filepath.Clean(targetRunFolder) == targetRunFolder {
		return targetRunFolder
	}
	if len(parts) >= 2 && strings.TrimSpace(parts[len(parts)-1]) != "" {
		return filepath.ToSlash(filepath.Join("iteration-0", parts[len(parts)-1]))
	}
	return "iteration-0"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
