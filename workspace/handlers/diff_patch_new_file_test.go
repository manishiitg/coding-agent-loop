package handlers

import "testing"

func TestApplyDiffPatchDirectCreatesContentFromEmptyFile(t *testing.T) {
	diff := "--- a/references/unfollow-cleanup.md\n" +
		"+++ b/references/unfollow-cleanup.md\n" +
		"@@ -0,0 +1,3 @@\n" +
		"+# X Unfollow Cleanup\n" +
		"+\n" +
		"+Use the shared browser and confirm each unfollow dialog.\n"

	got, err := ApplyDiffPatchDirect("", diff)
	if err != nil {
		t.Fatalf("ApplyDiffPatchDirect returned error: %v", err)
	}
	want := "# X Unfollow Cleanup\n\nUse the shared browser and confirm each unfollow dialog.\n"
	if got != want {
		t.Fatalf("patched content = %q, want %q", got, want)
	}
}
