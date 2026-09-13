package cliupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Manager, *time.Time, string) {
	t.Helper()
	t.Setenv("PI_BIN", "")
	base := t.TempDir()
	bin := filepath.Join(base, "global-bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range providers {
		writeCLI(t, filepath.Join(bin, p.Name), "old")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	m, err := New(filepath.Join(base, "cli-updates"), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	m.Now = func() time.Time { return now }
	return m, &now, bin
}

func writeCLI(t *testing.T, path, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' '" + strings.ReplaceAll(version, "'", "'\"'\"'") + "'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
}

func fakeUpdater(version string, calls *int) Installer {
	return func(_ context.Context, p Provider, _ string, _ Result) (string, string, error) {
		(*calls)++
		path, err := findGlobalCLI(p)
		return path, version, err
	}
}

func check(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func state(t *testing.T, m *Manager) State {
	t.Helper()
	s, err := m.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestChecksGlobalCLIsDailyAcrossRestarts(t *testing.T) {
	m, now, _ := fixture(t)
	calls := 0
	m.Install = fakeUpdater("2", &calls)
	check(t, m)
	if calls != 4 {
		t.Fatalf("update calls=%d, want 4", calls)
	}
	s := state(t, m)
	if !s.LastSuccess.Equal(*now) || s.CLIs["codex"].Status != "updated" {
		t.Fatalf("state=%+v", s)
	}
	info, err := os.Stat(filepath.Join(m.Root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state mode=%v", info.Mode())
	}

	restarted, err := New(m.Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Now, restarted.Install = m.Now, m.Install
	*now = now.Add(23 * time.Hour)
	check(t, restarted)
	if calls != 4 {
		t.Fatalf("restart reset daily timer: %d", calls)
	}
	*now = now.Add(time.Hour)
	check(t, restarted)
	if calls != 8 {
		t.Fatalf("24-hour update calls=%d", calls)
	}
}

func TestFailureKeepsPreviousVersionAndRetriesOnlyFailure(t *testing.T) {
	m, now, _ := fixture(t)
	calls := 0
	m.Install = fakeUpdater("1", &calls)
	check(t, m)
	*now = now.Add(Interval)
	good := fakeUpdater("2", &calls)
	m.Install = func(ctx context.Context, p Provider, root string, old Result) (string, string, error) {
		if p.Name == "codex" {
			calls++
			return "", "", errors.New("offline")
		}
		return good(ctx, p, root, old)
	}
	check(t, m)
	s := state(t, m)
	if s.CLIs["codex"].Status != "failed" || s.CLIs["codex"].Version != "1" || s.CLIs["claude"].Version != "2" {
		t.Fatalf("state=%+v", s)
	}
	*now = now.Add(RetryInterval)
	m.Install = good
	check(t, m)
	if calls != 9 || state(t, m).CLIs["codex"].Version != "2" {
		t.Fatalf("retry calls=%d state=%+v", calls, state(t, m))
	}
}

func TestMissingCLIIsNotInstalled(t *testing.T) {
	m, _, _ := fixture(t)
	calls := 0
	m.Install = func(_ context.Context, p Provider, _ string, _ Result) (string, string, error) {
		calls++
		if p.Name == "pi" {
			return "", "", errCLINotInstalled
		}
		path, err := findGlobalCLI(p)
		return path, "2", err
	}
	check(t, m)
	if calls != 4 || state(t, m).CLIs["pi"].Status != "not_installed" {
		t.Fatalf("calls=%d state=%+v", calls, state(t, m))
	}
}

func TestOnlyOneProcessChecksSharedRoot(t *testing.T) {
	m, _, _ := fixture(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	m.Install = func(context.Context, Provider, string, Result) (string, string, error) {
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
		t.Error("concurrent updater ran")
		return "", "", errors.New("unexpected")
	}
	check(t, other)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestGlobalUpdaterUsesOfficialCommandsAndSanitizedEnvironment(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	log := filepath.Join(base, "calls")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("HOME", base)
	t.Setenv("SECRET_TEST", "must-not-leak")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
[ -z "${SECRET_TEST:-}" ] || exit 31
case "$1" in
  update) printf '%s\n' "$*" >> "` + log + `" ;;
  --version) printf '9.1.0\n' ;;
  --disable-auto-update) [ "$2" = --version ]; printf '9.1.0\n' ;;
  *) exit 32 ;;
esac
`
	for _, name := range []string{"codex", "claude", "pi", "agent"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range providers[:4] {
		executable, version, err := updateGlobalCLI(context.Background(), p, "", Result{})
		if err != nil || !filepath.IsAbs(executable) || version != "9.1.0" {
			t.Fatalf("%s: executable=%q version=%q err=%v", p.Name, executable, version, err)
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"update", "update", "update --self", "update"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commands=%q want=%q", got, want)
		}
	}
}

func TestExplicitPiPathIsTheGlobalCLIThatGetsUpdated(t *testing.T) {
	base := t.TempDir()
	pi := filepath.Join(base, "custom-pi")
	marker := filepath.Join(base, "updated")
	script := "#!/bin/sh\nif [ \"$1\" = update ]; then touch '" + marker + "'; else echo 3; fi\n"
	if err := os.WriteFile(pi, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_BIN", pi)
	got, version, err := updateGlobalCLI(context.Background(), Provider{Name: "pi"}, "", Result{})
	if err != nil || got != pi || version != "3" {
		t.Fatalf("path=%q version=%q err=%v", got, version, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("PI_BIN updater was not invoked")
	}
}

func TestActivateAndCheckRetirePrivateInstallations(t *testing.T) {
	m, _, _ := fixture(t)
	retiredBin := filepath.Join(m.Root, "bin")
	for _, name := range []string{"bin", "current", "releases", "sessions"} {
		if err := os.MkdirAll(filepath.Join(m.Root, name, "old"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENTWORKS_MANAGED_CLI_BIN", retiredBin)
	t.Setenv("PATH", retiredBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := m.Activate(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AGENTWORKS_MANAGED_CLI_BIN") != "" || strings.Contains(os.Getenv("PATH"), retiredBin) {
		t.Fatal("retired private launch routing remains active")
	}
	calls := 0
	m.Install = fakeUpdater("2", &calls)
	check(t, m)
	for _, name := range []string{"bin", "current", "releases", "sessions"} {
		if _, err := os.Stat(filepath.Join(m.Root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("retired %s remains: %v", name, err)
		}
	}
}

func TestCorruptStateIsPreserved(t *testing.T) {
	m, _, _ := fixture(t)
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
}

func TestCancellationStopsUpdaterDescendants(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "should-not-exist")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	command := "(sleep 0.5; touch '" + marker + "') & wait"
	_, err := run(ctx, dir, updateEnv(), "/bin/sh", "-c", command)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("updater descendant survived cancellation")
	}
}

func TestMuseIsObservedButNeverUpdated(t *testing.T) {
	m, now, _ := fixture(t)
	calls := 0
	m.Install = fakeUpdater("2", &calls)
	check(t, m)
	r := state(t, m).CLIs["muse"]
	if calls != 4 || r.Status != "self_updating" || r.Version != "old" || !r.LastSuccess.Equal(*now) {
		t.Fatalf("calls=%d muse=%+v", calls, r)
	}
}
