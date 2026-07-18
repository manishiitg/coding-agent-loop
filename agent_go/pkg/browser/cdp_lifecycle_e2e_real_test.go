package browser

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	workspacehandlers "github.com/manishiitg/coding-agent-loop/workspace/handlers"
	"github.com/spf13/viper"
)

// TestManagedCDPMultiWorkflowRealE2E exercises the production browser path:
// Executor -> Client -> workspace /api/execute -> real agent-browser -> a
// dedicated real Chrome CDP instance. It never attaches to the user's normal
// Chrome profile or default CDP port. Two workflow owners deliberately issue
// overlapping commands against the same CDP daemon to verify tab, filesystem,
// artifact, recording, and delayed-cleanup isolation.
//
// Run with:
//
//	RUN_BROWSER_REAL_E2E=1 go test ./pkg/browser -run TestManagedCDPMultiWorkflowRealE2E -count=1 -v
func TestManagedCDPMultiWorkflowRealE2E(t *testing.T) {
	if os.Getenv("RUN_BROWSER_REAL_E2E") != "1" {
		t.Skip("set RUN_BROWSER_REAL_E2E=1 to run the live agent-browser/CDP contract")
	}
	if _, err := exec.LookPath("agent-browser"); err != nil {
		t.Fatalf("live browser E2E requires agent-browser in PATH: %v", err)
	}

	chromeBinary := browserE2EChromeBinary(t)
	port := browserE2EFreePort(t)
	profileDir := t.TempDir()
	startBrowserE2EChrome(t, chromeBinary, profileDir, port)

	t.Setenv("NATIVE_WORKSPACE", "true")
	t.Setenv("CDP_HOST", "localhost")
	resetCDPRegistryForTest(t)

	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download-file" {
			owner := r.URL.Query().Get("owner")
			if owner == "" {
				owner = "unknown"
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-downloaded.txt"`, owner))
			_, _ = fmt.Fprintf(w, "browser-e2e-download-%s", owner)
			return
		}
		marker := "alpha-e2e-marker"
		if r.URL.Path == "/beta" {
			marker = "beta-e2e-marker"
		} else if r.URL.Path == "/gamma" {
			marker = "gamma-e2e-marker"
		}
		owner := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html>
<title>%s</title><h1 id=marker>%s</h1>
<input id=upload type=file>
<output id=upload-name></output><output id=upload-body></output>
<button id=animate onclick="this.textContent='clicked'; document.body.dataset.clicked='yes'">Animate</button>
<a id=download href="/download-file?owner=%s" download>Download</a>
<script>
document.querySelector('#upload').addEventListener('change', async (event) => {
  const file = event.target.files[0];
  document.querySelector('#upload-name').textContent = file ? file.name : '';
  document.querySelector('#upload-body').textContent = file ? await file.text() : '';
});
</script>`, marker, marker, owner)
	}))
	defer pageServer.Close()

	workspaceDir := t.TempDir()
	for _, dir := range []string{"Downloads", "inputs", "evidence", "workflow-b/Downloads", "workflow-b/inputs", "workflow-b/evidence"} {
		if err := os.MkdirAll(filepath.Join(workspaceDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "inputs", "e2e-upload.txt"), []byte("browser-e2e-upload-body"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "workflow-b", "inputs", "e2e-upload-b.txt"), []byte("browser-e2e-upload-b-body"), 0600); err != nil {
		t.Fatal(err)
	}
	previousDocsDir := viper.GetString("docs-dir")
	viper.Set("docs-dir", workspaceDir)
	t.Cleanup(func() { viper.Set("docs-dir", previousDocsDir) })

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.POST("/api/execute", workspacehandlers.ExecuteShellCommand)
	router.GET("/api/cdp-check", workspacehandlers.CheckCdpConnection)
	workspaceServer := httptest.NewServer(router)
	defer workspaceServer.Close()

	client := NewClient(workspaceServer.URL)
	executor := NewExecutor(client, WithCdpPort(port))
	owner := fmt.Sprintf("browser-e2e-owner-a-%d", time.Now().UnixNano())
	ownerB := fmt.Sprintf("browser-e2e-owner-b-%d", time.Now().UnixNano())
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, owner)
	ctx = context.WithValue(ctx, common.BrowserDownloadsPathKey, "Downloads")
	ctxB := context.WithValue(context.Background(), common.ChatSessionIDKey, ownerB)
	ctxB = context.WithValue(ctxB, common.BrowserDownloadsPathKey, "workflow-b/Downloads")
	common.SetSessionFolderGuard(owner, []string{"Downloads"}, []string{"Downloads"})
	common.SetSessionFolderGuard(ownerB, []string{"workflow-b/Downloads"}, []string{"workflow-b/Downloads"})
	t.Cleanup(func() {
		common.ClearSessionShellConfig(owner)
		common.ClearSessionShellConfig(ownerB)
	})
	endpoint := fmt.Sprintf("http://localhost:%d", port)
	session := sharedCDPSessionName(port)
	directOptions := &ExecuteOptions{
		Timeout:          20 * time.Second,
		WorkingDirectory: "Downloads",
		FolderGuard: &FolderGuardConfig{
			Enabled:    true,
			ReadPaths:  []string{"Downloads"},
			WritePaths: []string{"Downloads"},
		},
	}

	direct := func(args ...string) string {
		t.Helper()
		command := append([]string{"--session", session}, args...)
		command = append(command, "--cdp", endpoint, "--json")
		output, err := client.ExecuteCommand(ctx, command, directOptions)
		if err != nil {
			t.Fatalf("direct agent-browser %v: %v", args, err)
		}
		return output
	}
	managedCall := func(callCtx context.Context, command string, args ...string) (string, error) {
		withEndpoint := append([]string{"--cdp", endpoint}, args...)
		return executor.HandleAgentBrowser(callCtx, map[string]interface{}{
			"command": command,
			"args":    withEndpoint,
			"session": "caller-session-is-remapped-in-cdp",
		})
	}
	managed := func(command string, args ...string) string {
		t.Helper()
		output, err := managedCall(ctx, command, args...)
		if err != nil {
			t.Fatalf("managed agent_browser %s %v: %v", command, args, err)
		}
		return output
	}
	managedB := func(command string, args ...string) string {
		t.Helper()
		output, err := managedCall(ctxB, command, args...)
		if err != nil {
			t.Fatalf("workflow B agent_browser %s %v: %v", command, args, err)
		}
		return output
	}
	list := func() []cdpTabInfo {
		t.Helper()
		tabs, err := parseCDPTabs(direct("tab"))
		if err != nil {
			t.Fatalf("parse real tab list: %v", err)
		}
		return tabs
	}

	alphaURL := pageServer.URL + "/alpha"
	betaURL := pageServer.URL + "/beta"
	gammaURL := pageServer.URL + "/gamma"
	initialCount := len(list())

	// Create a pre-existing user tab outside the managed ownership path.
	userCreated, ok := findCreatedCDPTab(direct("tab", "new", "--label", "e2e-user-tab", alphaURL), "e2e-user-tab")
	if !ok || !isCDPTabID(userCreated.TabID) {
		t.Fatalf("real agent-browser did not return a durable user tab ID: %#v", userCreated)
	}
	if got := len(list()); got != initialCount+1 {
		t.Fatalf("pre-existing tab count = %d, want %d", got, initialCount+1)
	}

	// Asking for the exact same URL must reuse the user tab without owning it.
	reused := managed("tab", "new", "--label", "e2e-reuse-request", alphaURL)
	if !strings.Contains(reused, "Reused existing CDP tab") || !strings.Contains(reused, "id="+userCreated.TabID) {
		t.Fatalf("exact-URL reuse result = %q, want user tab %s", reused, userCreated.TabID)
	}
	if got := len(list()); got != initialCount+1 {
		t.Fatalf("exact-URL reuse created a duplicate: tab count = %d, want %d", got, initialCount+1)
	}
	if tabs := ownedCDPTabsForOwner(port, owner); len(tabs) != 0 {
		t.Fatalf("reused user tab became workflow-owned: %#v", tabs)
	}

	// Put the URL before --label to prove the adapter canonicalizes tab-new
	// arguments before the real CLI sees them.
	created := managed("tab", "new", betaURL, "--label", "e2e-workflow-tab")
	if !strings.Contains(created, "Created CDP tab") {
		t.Fatalf("managed tab creation result = %q", created)
	}
	workflowTabID := getCDPTabSelection(port, owner)
	if !isCDPTabID(workflowTabID) || workflowTabID == userCreated.TabID {
		t.Fatalf("workflow tab ID = %q, want a new real tN", workflowTabID)
	}
	if got := len(list()); got != initialCount+2 {
		t.Fatalf("canonical creation tab count = %d, want %d", got, initialCount+2)
	}
	owned := ownedCDPTabsForOwner(port, owner)
	if len(owned) != 1 || owned[0].TabID != workflowTabID {
		t.Fatalf("owned tabs = %#v, want only %s", owned, workflowTabID)
	}

	createdB := managedB("tab", "new", "--label", "e2e-workflow-b-tab", gammaURL)
	if !strings.Contains(createdB, "Created CDP tab") {
		t.Fatalf("workflow B tab creation result = %q", createdB)
	}
	workflowBTabID := getCDPTabSelection(port, ownerB)
	if !isCDPTabID(workflowBTabID) || workflowBTabID == workflowTabID || workflowBTabID == userCreated.TabID {
		t.Fatalf("workflow B tab ID = %q, want a distinct real tN", workflowBTabID)
	}
	if got := len(list()); got != initialCount+3 {
		t.Fatalf("multi-workflow tab count = %d, want %d", got, initialCount+3)
	}
	if ownedB := ownedCDPTabsForOwner(port, ownerB); len(ownedB) != 1 || ownedB[0].TabID != workflowBTabID {
		t.Fatalf("workflow B owned tabs = %#v, want only %s", ownedB, workflowBTabID)
	}

	// One workflow must not be able to reset the shared daemon while the other
	// owner is active.
	if _, err := managedCall(ctx, "reset"); err == nil || !strings.Contains(err.Error(), "other workflow") {
		t.Fatalf("shared reset was not blocked while workflow B was active: %v", err)
	}

	// Simulate three actors changing Chrome's active tab. Parallel requests enter
	// the production per-port lock in an unpredictable order; every request must
	// still select its own real tab before acting.
	direct("tab", userCreated.TabID)
	browserE2ERunConcurrent(t,
		browserE2ECall{name: "workflow A URL", run: func() error {
			output, err := managedCall(ctx, "get", "tab", workflowTabID, "url")
			if err == nil && !strings.Contains(output, betaURL) {
				err = fmt.Errorf("got %q, want %q", output, betaURL)
			}
			return err
		}},
		browserE2ECall{name: "workflow B URL", run: func() error {
			output, err := managedCall(ctxB, "get", "tab", workflowBTabID, "url")
			if err == nil && !strings.Contains(output, gammaURL) {
				err = fmt.Errorf("got %q, want %q", output, gammaURL)
			}
			return err
		}},
	)

	// A direct selection response must stay compact and must not expose the raw
	// all-tabs JSON to the agent context.
	selection := managed("tab", workflowTabID)
	if !strings.Contains(selection, "Selected CDP tab: "+workflowTabID) || strings.Contains(selection, `"tabs"`) || strings.Contains(selection, alphaURL) {
		t.Fatalf("selection response is not context-safe: %q", selection)
	}

	// The daemon was launched with access only to workflow A's Downloads. Later
	// requests grant two disjoint workflow trees. Concurrent uploads must bridge
	// that stale daemon sandbox without crossing either workflow's FolderGuard.
	common.SetSessionFolderGuard(owner, []string{"inputs", "Downloads", "evidence"}, []string{"Downloads", "evidence"})
	common.SetSessionFolderGuard(ownerB,
		[]string{"workflow-b/inputs", "workflow-b/Downloads", "workflow-b/evidence"},
		[]string{"workflow-b/Downloads", "workflow-b/evidence"})
	if _, err := managedCall(ctx, "upload", "tab", workflowTabID, "#upload", "workflow-b/inputs/e2e-upload-b.txt"); !browserE2EIsAccessDenied(err) {
		t.Fatalf("workflow A could upload workflow B's file: %v", err)
	}
	browserE2ERunConcurrent(t,
		browserE2ECall{name: "workflow A upload", run: func() error {
			_, err := managedCall(ctx, "upload", "tab", workflowTabID, "#upload", "inputs/e2e-upload.txt")
			return err
		}},
		browserE2ECall{name: "workflow B upload", run: func() error {
			_, err := managedCall(ctxB, "upload", "tab", workflowBTabID, "#upload", "workflow-b/inputs/e2e-upload-b.txt")
			return err
		}},
	)
	managed("wait", "tab", workflowTabID, "100")
	managedB("wait", "tab", workflowBTabID, "100")
	for _, check := range []struct {
		name, got, want string
	}{
		{"workflow A upload name", managed("eval", "tab", workflowTabID, `document.querySelector('#upload-name').textContent`), "e2e-upload.txt"},
		{"workflow A upload body", managed("eval", "tab", workflowTabID, `document.querySelector('#upload-body').textContent`), "browser-e2e-upload-body"},
		{"workflow B upload name", managedB("eval", "tab", workflowBTabID, `document.querySelector('#upload-name').textContent`), "e2e-upload-b.txt"},
		{"workflow B upload body", managedB("eval", "tab", workflowBTabID, `document.querySelector('#upload-body').textContent`), "browser-e2e-upload-b-body"},
	} {
		if !strings.Contains(check.got, check.want) {
			t.Fatalf("%s = %q, want %q", check.name, check.got, check.want)
		}
	}

	// Screenshots and downloads are daemon-produced outputs. Run both workflows'
	// requests in parallel and ensure the artifact broker publishes only to the
	// corresponding authorized workspace tree.
	screenshotA := "evidence/workflow-a.png"
	screenshotB := "workflow-b/evidence/workflow-b.png"
	browserE2ERunConcurrent(t,
		browserE2ECall{name: "workflow A screenshot", run: func() error {
			_, err := managedCall(ctx, "screenshot", "tab", workflowTabID, screenshotA)
			return err
		}},
		browserE2ECall{name: "workflow B screenshot", run: func() error {
			_, err := managedCall(ctxB, "screenshot", "tab", workflowBTabID, screenshotB)
			return err
		}},
	)
	browserE2EAssertFile(t, filepath.Join(workspaceDir, screenshotA), []byte("\x89PNG\r\n\x1a\n"), 100)
	browserE2EAssertFile(t, filepath.Join(workspaceDir, screenshotB), []byte("\x89PNG\r\n\x1a\n"), 100)

	downloadA := "Downloads/workflow-a.txt"
	downloadB := "workflow-b/Downloads/workflow-b.txt"
	if _, err := managedCall(ctxB, "download", "tab", workflowBTabID, "#download", downloadA); !browserE2EIsAccessDenied(err) {
		t.Fatalf("workflow B could publish a download into workflow A's tree: %v", err)
	}
	browserE2ERunConcurrent(t,
		browserE2ECall{name: "workflow A download", run: func() error {
			_, err := managedCall(ctx, "download", "tab", workflowTabID, "#download", downloadA)
			return err
		}},
		browserE2ECall{name: "workflow B download", run: func() error {
			_, err := managedCall(ctxB, "download", "tab", workflowBTabID, "#download", downloadB)
			return err
		}},
	)
	browserE2EAssertFile(t, filepath.Join(workspaceDir, downloadA), []byte("browser-e2e-download-beta"), len("browser-e2e-download-beta"))
	browserE2EAssertFile(t, filepath.Join(workspaceDir, downloadB), []byte("browser-e2e-download-gamma"), len("browser-e2e-download-gamma"))

	// Video capture is intentionally exclusive on one shared CDP port. Workflow
	// B may continue normal browser work, but it cannot start or stop workflow A's
	// recording. Once A stops, B can record and publish its own WebM.
	recordingA := "evidence/workflow-a.webm"
	managed("record", "tab", workflowTabID, "start", recordingA)
	if _, err := managedCall(ctxB, "record", "tab", workflowBTabID, "start", "workflow-b/evidence/blocked.webm"); err == nil || !strings.Contains(err.Error(), "another workflow") {
		t.Fatalf("workflow B recording was not blocked while A owned capture: %v", err)
	}
	if _, err := managedCall(ctxB, "record", "tab", workflowBTabID, "stop"); err == nil || !strings.Contains(err.Error(), "owned by another workflow") {
		t.Fatalf("workflow B could stop workflow A's recording: %v", err)
	}
	if output := managedB("get", "tab", workflowBTabID, "url"); !strings.Contains(output, gammaURL) {
		t.Fatalf("workflow B normal command failed during workflow A recording: %q", output)
	}
	managed("click", "tab", workflowTabID, "#animate")
	time.Sleep(750 * time.Millisecond)
	managed("record", "tab", workflowTabID, "stop")
	browserE2EAssertFile(t, filepath.Join(workspaceDir, recordingA), []byte("\x1a\x45\xdf\xa3"), 100)

	recordingB := "workflow-b/evidence/workflow-b.webm"
	managedB("record", "tab", workflowBTabID, "start", recordingB)
	managedB("click", "tab", workflowBTabID, "#animate")
	time.Sleep(750 * time.Millisecond)
	managedB("record", "tab", workflowBTabID, "stop")
	browserE2EAssertFile(t, filepath.Join(workspaceDir, recordingB), []byte("\x1a\x45\xdf\xa3"), 100)

	// Replace the normal one-hour timers with short E2E timers. Cleaning workflow
	// A must leave workflow B and the pre-existing user tab intact; cleaning B
	// afterwards must still preserve the user tab.
	AcquireCDPTabOwnerLease(owner, []int{port})
	ReleaseCDPTabOwnerLease(owner, []int{port}, client, 75*time.Millisecond)
	browserE2EWaitForTabState(t, list, workflowTabID, false, 10*time.Second)
	if tabs := list(); !browserE2EHasTab(tabs, workflowBTabID) || !browserE2EHasTab(tabs, userCreated.TabID) {
		t.Fatalf("workflow A cleanup disturbed workflow B or user tab: %#v", tabs)
	}
	if tabs := ownedCDPTabsForOwner(port, owner); len(tabs) != 0 {
		t.Fatalf("workflow A owned-tab registry after cleanup = %#v, want empty", tabs)
	}
	if tabs := ownedCDPTabsForOwner(port, ownerB); len(tabs) != 1 || tabs[0].TabID != workflowBTabID {
		t.Fatalf("workflow A cleanup changed workflow B registry: %#v", tabs)
	}

	AcquireCDPTabOwnerLease(ownerB, []int{port})
	ReleaseCDPTabOwnerLease(ownerB, []int{port}, client, 75*time.Millisecond)
	browserE2EWaitForTabState(t, list, workflowBTabID, false, 10*time.Second)
	if tabs := list(); !browserE2EHasTab(tabs, userCreated.TabID) {
		t.Fatalf("workflow B cleanup also closed reused user tab %s: %#v", userCreated.TabID, tabs)
	}
	if tabs := ownedCDPTabsForOwner(port, ownerB); len(tabs) != 0 {
		t.Fatalf("workflow B owned-tab registry after cleanup = %#v, want empty", tabs)
	}
}

type browserE2ECall struct {
	name string
	run  func() error
}

func browserE2ERunConcurrent(t *testing.T, calls ...browserE2ECall) {
	t.Helper()
	start := make(chan struct{})
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = calls[i].run()
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent %s failed: %v", calls[i].name, err)
		}
	}
}

func browserE2EAssertFile(t *testing.T, path string, prefix []byte, minimumSize int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read browser artifact %s: %v", path, err)
	}
	if len(data) < minimumSize {
		t.Fatalf("browser artifact %s has %d bytes, want at least %d", path, len(data), minimumSize)
	}
	if !bytes.HasPrefix(data, prefix) {
		t.Fatalf("browser artifact %s prefix = %x, want %x", path, data[:min(len(data), len(prefix))], prefix)
	}
}

func browserE2EIsAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "access denied") ||
		strings.Contains(message, "not covered by this session's read paths") ||
		strings.Contains(message, "not covered by allowed write paths") ||
		strings.Contains(message, "not covered by this session's write paths")
}

func browserE2EWaitForTabState(t *testing.T, list func() []cdpTabInfo, tabID string, present bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if browserE2EHasTab(list(), tabID) == present {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tab %s present=%v did not become %v", tabID, !present, present)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func browserE2EChromeBinary(t *testing.T) string {
	t.Helper()
	if configured := strings.TrimSpace(os.Getenv("BROWSER_E2E_CHROME_BINARY")); configured != "" {
		if _, err := os.Stat(configured); err != nil {
			t.Fatalf("BROWSER_E2E_CHROME_BINARY: %v", err)
		}
		return configured
	}
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
	}
	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			continue
		}
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved
		}
	}
	t.Fatalf("live browser E2E requires Chrome/Chromium; set BROWSER_E2E_CHROME_BINARY")
	return ""
}

func browserE2EFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func startBrowserE2EChrome(t *testing.T, binary, profileDir string, port int) {
	t.Helper()
	args := []string{
		"--headless=new",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-extensions",
		"--disable-sync",
		"--remote-debugging-address=127.0.0.1",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir=" + profileDir,
		"about:blank",
	}
	if runtime.GOOS == "linux" {
		args = append([]string{"--no-sandbox", "--disable-dev-shm-usage"}, args...)
	}
	cmd := exec.Command(binary, args...)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dedicated Chrome: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		resetCDPSessionRuntime(sharedCDPSessionName(port))
	})

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := (&http.Client{Timeout: time.Second}).Get(endpoint)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	t.Fatalf("dedicated Chrome did not expose CDP on port %d: %s", port, logs.String())
}

func browserE2EHasTab(tabs []cdpTabInfo, tabID string) bool {
	for _, tab := range tabs {
		if tab.TabID == tabID {
			return true
		}
	}
	return false
}
