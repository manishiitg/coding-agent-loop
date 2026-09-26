package server

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Crews moved to a shared Crew/<id> root (docs/design/crew_shared_root.md).
// Their old per-user location has three spellings, and code that parses one
// by hand broke on the others again and again. New code resolves crew paths
// through resolveCrewPath / parseCrewPath (crew_ref.go). The files below still
// read legacy paths for compatibility (unmigrated crews, stored references);
// shrink this list, never grow it.
var legacyCrewPathFiles = map[string]bool{
	"agent_profile_runtime.go":           true,
	"chat_submission_journal.go":         true,
	"cost_overview.go":                   true,
	"crew_access.go":                     true,
	"crew_bot_scope_migration.go":        true,
	"crew_ref.go":                        true,
	"crew_root_migration.go":             true,
	"services/bot_scope.go":              true,
	"services/slack_connections.go":      true,
	"services/whatsapp_service.go":       true,
	"shared_assets_crew.go":              true,
	"slack_connection_routes.go":         true,
	"ui_control_routes.go":               true,
	"ui_control.go":                      true,
	"virtual-tools/workflow_db_tools.go": true,
	"workflow_context_access.go":         true,
}

var legacyCrewPathPattern = regexp.MustCompile(`Chats/Work/projects|"Work",\s*"projects"`)

func TestNoNewHandWrittenCrewPaths(t *testing.T) {
	var offenders []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (d.Name() == "testdata" || d.Name() == "node_modules" || d.Name() == "guidance") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(path)
		if legacyCrewPathFiles[rel] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if legacyCrewPathPattern.Match(raw) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("hand-written legacy crew paths in %v: resolve crew paths with resolveCrewPath/parseCrewPath (crew_ref.go) instead", offenders)
	}
}
