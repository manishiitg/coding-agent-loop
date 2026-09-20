package sparkquillproduct

import (
	"io/fs"
	"strings"
	"testing"
)

// The per-user ("yours") secret store is gone: only product-box secrets and
// globals exist. Guidance must never name the removed tools, or the parent
// agent will call tools that are no longer registered. The manifest must not
// grant them either.
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
	checkFS := func(fsys fs.FS, root string) {
		t.Helper()
		err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			content, err := fs.ReadFile(fsys, path)
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
	checkFS(productConfigFiles, "prompts")
	checkFS(SkillFiles, "skills")
}
