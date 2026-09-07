package step_based_workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptedBundlePreflight(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	for _, tc := range []struct{ name, main, helper, want string }{
		{"adjacent helper", "import helper\n", "raise RuntimeError('must not execute during preflight')\n", "configuration OK"},
		{"broken helper", "import helper\n", "def broken(:\n", "SyntaxError"},
		{"missing module", "import nonexistent_scripted_test_module_731\n", "", "missing module"},
		{"missing config", "import os\nurl = os.environ['SCRIPTED_TEST_MISSING_731']\n", "", "missing environment variable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "main.py")
			if err := os.WriteFile(main, []byte(tc.main), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.helper != "" {
				if err := os.WriteFile(filepath.Join(dir, "helper.py"), []byte(tc.helper), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(python, "-B", "-c", scriptedBundlePreflight, main)
			out, err := cmd.CombinedOutput()
			if !strings.Contains(string(out), tc.want) {
				t.Fatalf("output %s, error %v", out, err)
			}
			if (err == nil) != (tc.name == "adjacent helper") {
				t.Fatalf("unexpected exit: %v; %s", err, out)
			}
		})
	}
}
