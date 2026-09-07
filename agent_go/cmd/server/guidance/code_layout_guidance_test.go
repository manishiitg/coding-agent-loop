package guidance

import (
	"io/fs"
	"strings"
	"testing"
)

// These references are loaded independently by builder, repair and review
// agents. Each source-path reference must explain its own layout selection.
func TestSavedCodeReferencesSelectManifestLayout(t *testing.T) {
	err := fs.WalkDir(templatesFS, "templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := templatesFS.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		referencesSource := strings.Contains(text, "<script-dir>") || strings.Contains(text, "learnings/<step-id>/main.py") || strings.Contains(text, "learnings/{step-id}/main.py")
		if referencesSource && (!strings.Contains(text, "code_layout_version") || !strings.Contains(text, "legacy")) {
			t.Errorf("%s has saved-code guidance without a layout selector", path)
		}
		if strings.Contains(text, "<script-dir>") && !strings.Contains(text, "Resolve the placeholder") {
			t.Errorf("%s does not explain how to resolve source placeholders", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
