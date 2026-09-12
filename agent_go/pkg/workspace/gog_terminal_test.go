package workspace

import (
	"testing"
)

func TestGogHomePreflightGrantIsNarrow(t *testing.T) {
	t.Setenv("GOG_HOME", "/Users/test/.config/agentworks/gog")
	t.Setenv("AGENTWORKS_GOG_TERMINAL_ACCESS", "")
	guard := &FolderGuardConfig{Enabled: true, ReadPaths: []string{"Workflow/demo"}}
	for _, cmd := range []string{
		`gog --home /Users/test/.config/agentworks/gog auth list`,
		`cat /Users/test/.config/agentworks/gog/config/config.json`,
	} {
		if err := blockAbsoluteHostPaths(cmd, guard); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range []string{`cat /Users/test/.config/agentworks/other`, `cat /Users/test/.config/agentworks/gog-other/token`} {
		if err := blockAbsoluteHostPaths(cmd, guard); err == nil {
			t.Fatalf("allowed unrelated host path: %s", cmd)
		}
	}
	guard.StrictAllowlist = true
	if err := blockAbsoluteHostPaths(`gog --home /Users/test/.config/agentworks/gog auth list`, guard); err == nil {
		t.Fatal("restricted profile inherited gog access")
	}
	guard.StrictAllowlist = false
	t.Setenv("AGENTWORKS_GOG_TERMINAL_ACCESS", "false")
	if err := blockAbsoluteHostPaths(`gog --home /Users/test/.config/agentworks/gog auth list`, guard); err == nil {
		t.Fatal("disabled terminal integration still grants access")
	}
}
