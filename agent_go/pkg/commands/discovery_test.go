package commands

import "testing"

func TestValidateFolderNameRejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../other", "folder/name", "folder name"} {
		if err := validateFolderName(name); err == nil {
			t.Fatalf("validateFolderName(%q) accepted an unsafe command folder", name)
		}
	}
	for _, name := range []string{"quick-review", "review_code", "status2"} {
		if err := validateFolderName(name); err != nil {
			t.Fatalf("validateFolderName(%q): %v", name, err)
		}
	}
}
