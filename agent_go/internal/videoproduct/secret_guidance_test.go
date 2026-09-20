package videoproduct

import (
	"io/fs"
	"strings"
	"testing"
)

// The per-user ("yours") secret store is gone: only project-box secrets and
// globals exist. Guidance and the manifest must never name the removed tools,
// or the studio agent will call tools that are no longer registered.
func TestSecretGuidanceNamesNoRemovedTools(t *testing.T) {
	removed := []string{"set_user_secret", "delete_user_secret"}
	manifest, err := fs.ReadFile(productConfigFiles, "product.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range removed {
		if strings.Contains(string(manifest), tool) {
			t.Errorf("product.yaml grants removed tool %q", tool)
		}
	}
	for _, root := range []string{"prompts", "commands"} {
		err := fs.WalkDir(productConfigFiles, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			content, err := fs.ReadFile(productConfigFiles, path)
			if err != nil {
				return err
			}
			for _, tool := range removed {
				if strings.Contains(string(content), tool) {
					t.Errorf("%s references removed tool %q", path, tool)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
