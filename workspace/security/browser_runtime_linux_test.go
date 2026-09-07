//go:build linux

package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in deployment smoke: two independent sandboxes launch the configured
// browser, record a local HTML page, and prove recordings survive cleanup.
func TestBrowserRuntimeSmoke(t *testing.T) {
	site := os.Getenv("AW_TEST_PLAYWRIGHT_SITE")
	if site == "" {
		t.Skip("set AW_TEST_PLAYWRIGHT_SITE for live browser verification")
	}
	for i := 0; i < 2; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			deep := filepath.Join(root, strings.Repeat("long-step-", 12))
			if err := os.Mkdir(deep, 0700); err != nil {
				t.Fatal(err)
			}
			iso := &Isolator{BaseDir: root, WorkDir: deep, ReadPaths: []string{root, site}, WritePaths: []string{root}}
			if encoders := os.Getenv("PLAYWRIGHT_BROWSERS_PATH"); encoders != "" {
				iso.ReadPaths = append(iso.ReadPaths, encoders)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			script := fmt.Sprintf("python3 - <<'PY'\nimport sys,os\nsys.path.insert(0,%q)\nos.environ['STEP_OUTPUT_DIR']=%q\nfrom agentworks_browser import browser_session\nwith browser_session(record_video=True) as (c,a):\n p=c.new_page()\n p.set_content('<html><title>runtime smoke</title><body>browser test</body></html>')\n assert p.title() == 'runtime smoke'\n p.wait_for_timeout(500)\nprint('BROWSER_SMOKE_OK')\nPY", site, root)
			cmd, cleanup, err := iso.ExecuteIsolated(ctx, script, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("browser failed: %v\n%s", err, out)
			}
			cleanup()
			videos, _ := filepath.Glob(filepath.Join(root, "browser", "*", "*.webm"))
			if len(videos) == 0 {
				t.Fatal("recording missing")
			}
			for _, path := range videos {
				if info, err := os.Stat(path); err != nil || info.Size() == 0 {
					t.Fatal("recording empty")
				}
			}
			t.Log(string(out))
		})
	}
}
