package cliupdate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Manager, *time.Time) {
	t.Helper()
	t.Setenv("PI_BIN", "")
	root := t.TempDir()
	bin := filepath.Join(root, "original")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range providers {
		writeCLI(t, filepath.Join(bin, p.Name), "old")
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	m, err := New(filepath.Join(root, "managed releases"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	m.Now = func() time.Time { return now }
	return m, &now
}
func writeCLI(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' "+quote(value)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}
func fakeInstall(t *testing.T, version string, calls *int) Installer {
	return func(_ context.Context, p Provider, root string, _ Result) (string, string, error) {
		*calls++
		binary := filepath.Join(root, p.Name+"-"+version, "bin", p.Name)
		writeCLI(t, binary, version)
		return binary, version, nil
	}
}
func readState(t *testing.T, m *Manager) State {
	t.Helper()
	s, err := m.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func check(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDailyChecksPersistAcrossRestarts(t *testing.T) {
	m, now := fixture(t)
	calls := 0
	m.Install = fakeInstall(t, "2", &calls)
	check(t, m)
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
	s := readState(t, m)
	// muse's self-updating status re-probes hourly (RetryInterval), not
	// daily like the 4 managed providers, so it's the earliest NextCheck.
	if !s.NextCheck.Equal(now.Add(RetryInterval)) || !s.LastSuccess.Equal(*now) {
		t.Fatalf("state=%+v", s)
	}
	info, _ := os.Stat(filepath.Join(m.Root, "state.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	restarted, err := New(m.Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Now = m.Now
	restarted.Install = m.Install
	*now = now.Add(23 * time.Hour)
	check(t, restarted)
	if calls != 4 {
		t.Fatalf("restart reset daily timer: %d", calls)
	}
	*now = now.Add(time.Hour)
	check(t, restarted)
	if calls != 8 {
		t.Fatalf("server did not update after 24h: %d", calls)
	}
}

func TestFailureKeepsReleaseAndRetriesOnlyFailedCLI(t *testing.T) {
	m, now := fixture(t)
	calls := 0
	m.Install = fakeInstall(t, "1", &calls)
	check(t, m)
	*now = now.Add(Interval)
	good := fakeInstall(t, "2", &calls)
	m.Install = func(ctx context.Context, p Provider, root string, old Result) (string, string, error) {
		if p.Name == "codex" {
			calls++
			return "", "", errors.New("offline")
		}
		return good(ctx, p, root, old)
	}
	check(t, m)
	s := readState(t, m)
	if s.CLIs["codex"].Status != "failed" || s.CLIs["codex"].Version != "1" || s.CLIs["claude"].Version != "2" {
		t.Fatalf("%+v", s)
	}
	selected, _ := os.Readlink(filepath.Join(m.Root, "current", "codex"))
	if selected != s.CLIs["codex"].Executable {
		t.Fatal("failed release selected")
	}
	if !s.NextCheck.Equal(now.Add(RetryInterval)) {
		t.Fatal(s.NextCheck)
	}
	*now = now.Add(RetryInterval)
	m.Install = good
	check(t, m)
	if calls != 9 {
		t.Fatalf("expected only failed CLI retried: %d", calls)
	}
	if readState(t, m).CLIs["codex"].Version != "2" {
		t.Fatal("retry failed")
	}
}

func TestOnlyOneProcessChecksSharedRoot(t *testing.T) {
	m, _ := fixture(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	m.Install = func(_ context.Context, p Provider, root string, old Result) (string, string, error) {
		once.Do(func() { close(entered); <-release })
		return "", "", errors.New("fake failure")
	}
	go func() { done <- m.Check(context.Background()) }()
	<-entered
	other, err := New(m.Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	other.Now = m.Now
	other.Install = func(context.Context, Provider, string, Result) (string, string, error) {
		t.Error("concurrent installer ran")
		return "", "", errors.New("unexpected")
	}
	check(t, other)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLastSuccessRequiresWholeCheckToSucceed(t *testing.T) {
	m, now := fixture(t)
	calls := 0
	m.Install = fakeInstall(t, "1", &calls)
	check(t, m)
	lastSuccess := readState(t, m).LastSuccess
	*now = now.Add(Interval)
	good := fakeInstall(t, "2", &calls)
	m.Install = func(ctx context.Context, p Provider, root string, old Result) (string, string, error) {
		if p.Name == "cursor-agent" {
			return "", "", errors.New("last provider failed")
		}
		return good(ctx, p, root, old)
	}
	check(t, m)
	if !readState(t, m).LastSuccess.Equal(lastSuccess) {
		t.Fatal("partial check marked globally successful")
	}
}

func TestInstallerCancellationStopsChildProcesses(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "should-not-exist")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := run(ctx, dir, installEnv(dir), "/bin/sh", "-c", "(sleep 0.5; touch "+quote(marker)+") & wait")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("installer descendant survived cancellation")
	}
}

func TestChatPinsSurviveUpdatesAndConcurrentFirstLaunch(t *testing.T) {
	m, _ := fixture(t)
	calls := 0
	m.Install = fakeInstall(t, "1", &calls)
	check(t, m)
	runChat := func(key string) (string, error) {
		cmd := exec.Command(filepath.Join(m.Root, "bin", "codex"))
		cmd.Env = append(os.Environ(), "AGENTWORKS_CLI_SESSION_KEY="+key)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	key := strings.Repeat("a", 64)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := runChat(key)
			if err != nil || out != "1" {
				t.Errorf("%s: %v", out, err)
			}
		}()
	}
	wg.Wait()
	newBinary := filepath.Join(m.Root, "releases", "codex-2", "codex")
	writeCLI(t, newBinary, "2")
	if err := publish(filepath.Join(m.Root, "current", "codex"), newBinary); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{key: "1", strings.Repeat("b", 64): "2", "": "2"} {
		got, err := runChat(key)
		if err != nil || got != want {
			t.Fatalf("got %q, want %q: %v", got, want, err)
		}
	}
	if _, err := runChat("../../escape"); err == nil {
		t.Fatal("invalid session path accepted")
	}
}

func TestCorruptStatePreservedAndInterruptedAttemptWaits(t *testing.T) {
	m, now := fixture(t)
	path := filepath.Join(m.Root, "state.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Check(context.Background()); err == nil {
		t.Fatal("corrupt state accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "broken" {
		t.Fatal("corrupt state overwritten")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s := State{SchemaVersion: 1, CLIs: map[string]Result{}}
	for _, p := range providers {
		s.CLIs[p.Name] = Result{Status: "checking", LastAttempt: *now, NextCheck: now.Add(RetryInterval)}
	}
	if err := m.save(&s); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.Install = fakeInstall(t, "2", &calls)
	check(t, m)
	if calls != 0 {
		t.Fatal("immediate retry after interrupted attempt")
	}
	*now = now.Add(RetryInterval)
	check(t, m)
	if calls != 4 {
		t.Fatal(calls)
	}
}

func TestExplicitPiPinAndAbsentCLIsAreNotInstalled(t *testing.T) {
	m, _ := fixture(t)
	t.Setenv("PI_BIN", "/operator/pinned/pi")
	if err := os.Remove(filepath.Join(m.Root, "current", "claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(m.Root, "bin", "claude")); err != nil {
		t.Fatal(err)
	}
	// Avoid rediscovering the fake original claude.
	if err := os.Remove(filepath.Join(filepath.Dir(m.Root), "original", "claude")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.Install = fakeInstall(t, "2", &calls)
	check(t, m)
	s := readState(t, m)
	if calls != 2 || s.CLIs["pi"].Status != "operator_pinned" || s.CLIs["claude"].Status != "not_installed" {
		t.Fatalf("calls=%d state=%+v", calls, s)
	}
}

// TestSelfUpdatingProviderIsProbedNotInstalled pins the policy behind
// selfUpdatingProviders: muse's own launcher already self-updates in the
// background (unlike codex/claude/cursor-agent/pi, which only expose a
// manual `update` subcommand nothing else ever calls), so this package must
// never install for it -- only observe its version via `muse --version` and
// leave PATH/current/bin untouched.
func TestSelfUpdatingProviderIsProbedNotInstalled(t *testing.T) {
	m, now := fixture(t)
	calls := 0
	m.Install = fakeInstall(t, "2", &calls)
	check(t, m)
	s := readState(t, m)

	if s.CLIs["muse"].Status != "self_updating" {
		t.Fatalf("muse status = %q, want self_updating: %+v", s.CLIs["muse"].Status, s.CLIs["muse"])
	}
	if s.CLIs["muse"].Version != "old" {
		t.Fatalf("muse version = %q, want the fake CLI's real --version output", s.CLIs["muse"].Version)
	}
	if !s.CLIs["muse"].LastSuccess.Equal(*now) {
		t.Fatalf("muse LastSuccess = %v, want %v", s.CLIs["muse"].LastSuccess, *now)
	}
	if !s.CLIs["muse"].NextCheck.Equal(now.Add(RetryInterval)) {
		t.Fatalf("muse NextCheck = %v, want now+RetryInterval (hourly, not daily)", s.CLIs["muse"].NextCheck)
	}
	if calls != 4 {
		t.Fatalf("calls=%d, want 4 (muse must never reach the installer)", calls)
	}
	if _, err := os.Lstat(filepath.Join(m.Root, "current", "muse")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("muse must never get a managed symlink under current/, got err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(m.Root, "bin", "muse")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("muse must never get a managed shim under bin/, got err=%v", err)
	}
}

func TestNpmInstallerUsesPrivatePrefixAndSmokeChecks(t *testing.T) {
	t.Setenv("SECRET_TEST", "must-not-leak")
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	// A fake package manager verifies the actual subprocess environment and
	// creates an executable in the prefix passed by the real installer.
	script := `#!/bin/sh
set -eu
[ -z "${SECRET_TEST:-}" ] || exit 31
[ "$npm_config_userconfig" = /dev/null ] || exit 32
case "$1" in
view) printf '"9.1.0"';;
install)
  [ "$2" = --global ] && [ "$3" = --prefix ] || exit 33
  prefix="$4"
  [ "$HOME" = "$prefix/home" ] || exit 34
  mkdir -p "$prefix/bin"
  printf '#!/bin/sh\necho codex-cli 9.1.0\n' > "$prefix/bin/codex"
  chmod 700 "$prefix/bin/codex";;
*) exit 35;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	releases := filepath.Join(root, "releases")
	if err := os.MkdirAll(releases, 0700); err != nil {
		t.Fatal(err)
	}
	releases, _ = filepath.EvalSymlinks(releases)
	binary, version, err := installRelease(context.Background(), providers[0], releases, Result{})
	if err != nil {
		t.Fatal(err)
	}
	if version != "9.1.0" || !strings.HasPrefix(binary, releases+"/") {
		t.Fatalf("%s %s", binary, version)
	}
	// Up-to-date check reuses the old immutable release; no candidate debris.
	got, _, err := installRelease(context.Background(), providers[0], releases, Result{Version: version, Executable: binary})
	if err != nil || got != binary {
		t.Fatalf("%s %v", got, err)
	}
	entries, _ := os.ReadDir(releases)
	if len(entries) != 1 {
		t.Fatalf("candidate leak: %v", entries)
	}
	// A bad installed binary never becomes a candidate for publication.
	script = strings.ReplaceAll(script, "echo codex-cli 9.1.0", "exit 42")
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := installRelease(context.Background(), providers[0], releases, Result{}); err == nil {
		t.Fatal("bad CLI accepted")
	}
	entries, _ = os.ReadDir(releases)
	if len(entries) != 1 {
		t.Fatal("failed release not cleaned")
	}
}
