package browser

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func findTestChrome() string {
	for _, candidate := range []string{
		os.Getenv("AGENTWORKS_TEST_CHROME"),
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
	} {
		if candidate != "" {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

// startTestChrome launches headless Chrome on url with DevTools port 0, as the
// managed browser does, and returns its PID.
func startTestChrome(t *testing.T, url string) int {
	t.Helper()
	chrome := findTestChrome()
	if chrome == "" {
		t.Skip("no Chrome binary available")
	}
	profile := t.TempDir()
	cmd := exec.Command(chrome, "--headless=new", "--remote-debugging-port=0", "--user-data-dir="+profile, "--no-first-run", "--disable-gpu", url)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start Chrome: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(profile, "DevToolsActivePort")); err == nil {
			time.Sleep(1500 * time.Millisecond) // let the page start its script
			return cmd.Process.Pid
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skip("Chrome never wrote DevToolsActivePort")
	return 0
}

func TestHangDiagnosticsTellsBusyPageFromHealthyPage(t *testing.T) {
	hangDiagRoot = t.TempDir()

	busy := startTestChrome(t, "data:text/html,<title>spin</title><script>while(true){}</script>")
	report := collectHangReport(context.Background(), "test-busy", "eval", busy)
	if len(report.Pages) == 0 || report.Pages[0].Responsive {
		t.Fatalf("a page spinning in a loop must be reported as not answering: %+v", report)
	}
	if !strings.Contains(report.Summary, "not answering") {
		t.Fatalf("busy summary = %q", report.Summary)
	}

	healthy := startTestChrome(t, "data:text/html,<title>ok</title><p>hello</p>")
	report = collectHangReport(context.Background(), "test-healthy", "eval", healthy)
	if len(report.Pages) == 0 || !report.Pages[0].Responsive || report.Pages[0].Screenshot == "" {
		t.Fatalf("an idle page must answer and give a screenshot: %+v", report)
	}
	if !strings.Contains(report.Summary, "answers directly") {
		t.Fatalf("healthy summary = %q", report.Summary)
	}
	if _, err := os.Stat(filepath.Join(report.Dir, "trace.json")); err != nil {
		t.Fatalf("no trace saved: %v (problems %v)", err, report.Problems)
	}
}

func TestHangDiagnosticsSummaries(t *testing.T) {
	blocked := HangReport{Pages: []hangPageProbe{{URL: "https://x", Responsive: false}}, Processes: []hangProcessCPU{{Type: "renderer", CPUPercent: 3}}}
	if got := summarizeHang(blocked); !strings.Contains(got, "blocked, not busy") {
		t.Fatalf("idle renderer summary = %q", got)
	}
	busy := HangReport{Pages: []hangPageProbe{{URL: "https://x"}}, Processes: []hangProcessCPU{{Type: "renderer", CPUPercent: 99}}}
	if got := summarizeHang(busy); !strings.Contains(got, "saturated") {
		t.Fatalf("busy renderer summary = %q", got)
	}
}
