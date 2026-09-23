package workflowfiles

import "testing"

func TestMatchGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.py", "main.py", true},
		{"**/*.py", "run-basic-smoke/modules/auth.py", true},
		{"**/*.py", "run-basic-smoke/modules/auth.md", false},
		{"*.py", "main.py", true},
		{"*.py", "modules/auth.py", false},
		{"run-*/**/*.py", "run-basic-smoke/modules/auth.py", true},
		{"test_?.py", "test_a.py", true},
	} {
		if err := ValidateGlob(tc.pattern); err != nil {
			t.Fatalf("validating %q: %v", tc.pattern, err)
		}
		if got := MatchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
	for _, pattern := range []string{"../*.py", "/code/*.py", "foo/**bar.py", "[broken", "a//b", "."} {
		if err := ValidateGlob(pattern); err == nil {
			t.Errorf("accepted invalid glob %q", pattern)
		}
	}
}

func TestPrivateRuntimePaths(t *testing.T) {
	for _, path := range []string{".cache/pip/cache", ".antigravitycli/config", "code/.local/lib/x.py", "code/run/__pycache__/main.pyc", "code/node_modules/pkg/x.js", "code/.venv/lib/x.py", "code/.env", ".ssh/id_ed25519"} {
		if !Private(path) {
			t.Errorf("public runtime path: %q", path)
		}
	}
	for _, path := range []string{"code/run-basic-smoke/main.py", "knowledgebase/notes/test-setup.md", "code/vendor_utils.py"} {
		if Private(path) {
			t.Errorf("blocked workflow source: %q", path)
		}
	}
}
