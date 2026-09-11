// Package cliupdate maintains private coding-CLI releases independently of
// backend restarts. It never upgrades an executable from the user's PATH in place.
package cliupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const Interval = 24 * time.Hour
const RetryInterval = time.Hour

type Provider struct{ Name, Package string }

var providers = []Provider{
	{"codex", "@openai/codex"},
	{"claude", "@anthropic-ai/claude-code"},
	{"pi", "@earendil-works/pi-coding-agent"},
	{"cursor-agent", ""},
	{"muse", ""},
}

// selfUpdatingProviders never get a managed install: their own launcher
// already checks for and applies updates in the background on its own
// cadence (muse's ~/.local/bin/muse wrapper polls MUSE_CHANNEL_URL every
// MUSE_UPDATE_INTERVAL_SECONDS, default 1h, with no invocation needed).
// codex/claude/cursor-agent/pi all expose an `update` subcommand too, but
// none of them ever call it themselves -- that manual-only shape is exactly
// what this package's scheduled Check exists to drive, so they stay fully
// managed. Muse is the only one where "our own update" would just race an
// update already happening on its own.
var selfUpdatingProviders = map[string]bool{"muse": true}

type Result struct {
	Status      string    `json:"status"`
	Version     string    `json:"version,omitempty"`
	Executable  string    `json:"executable,omitempty"`
	LastAttempt time.Time `json:"last_attempt,omitempty"`
	LastSuccess time.Time `json:"last_success,omitempty"`
	NextCheck   time.Time `json:"next_check_at"`
	Error       string    `json:"error,omitempty"`
}
type State struct {
	SchemaVersion int               `json:"schema_version"`
	LastAttempt   time.Time         `json:"last_attempt,omitempty"`
	LastSuccess   time.Time         `json:"last_success,omitempty"`
	NextCheck     time.Time         `json:"next_check_at"`
	CLIs          map[string]Result `json:"clis"`
}

type Installer func(context.Context, Provider, string, Result) (executable, version string, err error)
type Manager struct {
	Root    string
	Install Installer
	Now     func() time.Time
	Log     func(string, ...any)
}

func DefaultRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("AGENTWORKS_STATE_ROOT")); root != "" {
		if !filepath.IsAbs(root) {
			return "", errors.New("AGENTWORKS_STATE_ROOT must be absolute")
		}
		return filepath.Join(root, "cli-updates"), nil
	}
	root, err := os.UserConfigDir()
	return filepath.Join(root, "agentworks", "cli-updates"), err
}

func New(root string, log func(string, ...any)) (*Manager, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("CLI update root must be absolute")
	}
	m := &Manager{Root: root, Now: time.Now, Log: log, Install: installRelease}
	for _, dir := range []string{"", "bin", "current", "releases", "sessions"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			return nil, err
		}
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	m.Root = canonical
	return m, nil
}

// Prepare runs once, before any agents start. Existing installations remain the
// fallback until the first successful managed install. No network access here.
func (m *Manager) Prepare() error {
	for _, p := range providers {
		// Explicit PI_BIN is an operator pin, not an installation we own.
		if p.Name == "pi" && os.Getenv("PI_BIN") != "" {
			continue
		}
		// Self-updating providers keep resolving straight to their own PATH
		// entry; a managed shim/symlink here would just shadow whatever
		// version their own updater just installed.
		if selfUpdatingProviders[p.Name] {
			continue
		}
		current := filepath.Join(m.Root, "current", p.Name)
		if _, err := os.Readlink(current); errors.Is(err, os.ErrNotExist) {
			path, err := exec.LookPath(p.Name)
			if err != nil && p.Name == "cursor-agent" {
				path, err = exec.LookPath("agent")
			}
			if err != nil {
				continue
			}
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if strings.HasPrefix(path, m.Root+string(os.PathSeparator)) {
				continue
			}
			if err = os.Symlink(path, current); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
		}
		if _, err := os.Readlink(current); err != nil {
			return fmt.Errorf("read %s: %w", current, err)
		}
		if err := atomicWrite(filepath.Join(m.Root, "bin", p.Name), []byte(m.shim(p.Name)), 0700); err != nil {
			return err
		}
		if p.Name == "cursor-agent" {
			if err := atomicWrite(filepath.Join(m.Root, "bin", "agent"), []byte(m.shim(p.Name)), 0700); err != nil {
				return err
			}
		}
	}
	return nil
}

// Activate only changes the backend's environment, never the user's shell.
func (m *Manager) Activate() error {
	bin := filepath.Join(m.Root, "bin")
	if err := os.Setenv("AGENTWORKS_MANAGED_CLI_BIN", bin); err != nil {
		return err
	}
	return os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(RetryInterval)
	defer ticker.Stop()
	for {
		if err := m.Check(ctx); err != nil && ctx.Err() == nil && m.Log != nil {
			m.Log("[CLI UPDATE] %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check coordinates all backend processes sharing a state root. flock is
// released by the OS on crashes, unlike a mkdir lock that can remain forever.
func (m *Manager) Check(ctx context.Context) error {
	lock, err := os.OpenFile(filepath.Join(m.Root, "update.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil
		}
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	s, err := m.ReadState()
	if err != nil {
		return err
	}
	changed := false
	for _, p := range providers {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := m.Now().UTC()
		r := s.CLIs[p.Name]
		if now.Before(r.NextCheck) {
			continue
		}
		changed = true
		s.LastAttempt, r.LastAttempt = now, now
		if p.Name == "pi" && os.Getenv("PI_BIN") != "" {
			r.Status, r.Error, r.NextCheck = "operator_pinned", "", now.Add(Interval)
			s.CLIs[p.Name] = r
			if err := m.save(&s); err != nil {
				return err
			}
			continue
		}
		if selfUpdatingProviders[p.Name] {
			version, executable, probeErr := probeSelfUpdatingVersion(ctx, p.Name)
			if probeErr != nil {
				r.Status, r.Error, r.NextCheck = "not_installed", "", now.Add(Interval)
			} else {
				r.Status, r.Error, r.Version, r.Executable, r.LastSuccess, r.NextCheck =
					"self_updating", "", version, executable, now, now.Add(RetryInterval)
			}
			s.CLIs[p.Name] = r
			if err := m.save(&s); err != nil {
				return err
			}
			if m.Log != nil {
				m.Log("[CLI UPDATE] %s: %s (version=%s)", p.Name, r.Status, r.Version)
			}
			continue
		}
		current := filepath.Join(m.Root, "current", p.Name)
		if _, err := os.Readlink(current); errors.Is(err, os.ErrNotExist) {
			// Discover CLIs installed after the server started. PATH is already
			// prepended, but absent providers have no managed shim yet.
			if err := m.Prepare(); err != nil {
				return err
			}
		}
		selected, err := os.Readlink(current)
		if errors.Is(err, os.ErrNotExist) {
			r.Status, r.Error, r.NextCheck = "not_installed", "", now.Add(Interval)
			s.CLIs[p.Name] = r
			if err := m.save(&s); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		r.Executable = selected
		r.LastAttempt, r.NextCheck, r.Status, r.Error = now, now.Add(RetryInterval), "checking", ""
		s.LastAttempt, s.CLIs[p.Name] = now, r
		// Persist before starting: a crash is a failed attempt with a bounded retry.
		if err := m.save(&s); err != nil {
			return err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		executable, version, installErr := m.Install(attemptCtx, p, filepath.Join(m.Root, "releases"), r)
		cancel()
		if installErr == nil {
			if !filepath.IsAbs(executable) || version == "" {
				installErr = errors.New("installer returned no verified release")
			} else {
				installErr = publish(current, executable)
			}
		}
		finished := m.Now().UTC()
		if installErr != nil {
			r.Status, r.Error, r.NextCheck = "failed", installErr.Error(), finished.Add(RetryInterval)
		} else {
			r.Status = "updated"
			if r.Version == version {
				r.Status = "current"
			}
			r.Version, r.Executable, r.LastSuccess, r.NextCheck = version, executable, finished, finished.Add(Interval)
		}
		s.CLIs[p.Name] = r
		if err := m.save(&s); err != nil {
			return err
		}
		if m.Log != nil {
			m.Log("[CLI UPDATE] %s: %s (version=%s)", p.Name, r.Status, r.Version)
		}
	}
	if changed {
		allSuccessful := len(s.CLIs) == len(providers)
		for _, r := range s.CLIs {
			if r.Status == "failed" || r.Status == "checking" {
				allSuccessful = false
			}
		}
		if allSuccessful {
			s.LastSuccess = m.Now().UTC()
		}
		return m.save(&s)
	}
	return nil
}

func (m *Manager) ReadState() (State, error) {
	s := State{SchemaVersion: 1, CLIs: make(map[string]Result)}
	data, err := os.ReadFile(filepath.Join(m.Root, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("invalid CLI update state (preserved): %w", err)
	}
	if s.SchemaVersion != 1 || s.CLIs == nil {
		return s, errors.New("unsupported CLI update state (preserved)")
	}
	return s, nil
}

func (m *Manager) save(s *State) error {
	s.NextCheck = time.Time{}
	for _, r := range s.CLIs {
		if s.NextCheck.IsZero() || r.NextCheck.Before(s.NextCheck) {
			s.NextCheck = r.NextCheck
		}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(m.Root, "state.json"), append(data, '\n'), 0600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func publish(path, target string) error {
	// A temporary directory gives the symlink a unique name without a gap in
	// current. os.Rename atomically replaces the old symlink on macOS/Linux.
	dir, err := os.MkdirTemp(filepath.Dir(path), ".select-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "link")
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func (m *Manager) shim(name string) string {
	return `#!/bin/sh
set -eu
root=` + quote(m.Root) + `
name=` + quote(name) + `
target=$(readlink "$root/current/$name")
key=${AGENTWORKS_CLI_SESSION_KEY:-}
if [ -n "$key" ]; then
  case "$key" in *[!a-f0-9]*) echo 'Invalid CLI session key' >&2; exit 1;; esac
  [ "${#key}" -eq 64 ] || exit 1
  umask 077
  mkdir -p "$root/sessions/$key"
  pin="$root/sessions/$key/$name"
  # First launch wins, including two concurrent launches of the same chat.
  ln -s "$target" "$pin" 2>/dev/null || [ -L "$pin" ]
  target=$(readlink "$pin")
fi
export DISABLE_AUTOUPDATER=1
export DISABLE_UPDATES=1
if [ "$name" = cursor-agent ]; then
  case "$target" in "$root"/releases/*) exec "$target" --disable-auto-update "$@";; esac
fi
exec "$target" "$@"
`
}

// probeSelfUpdatingVersion reports a self-updating provider's current
// version without touching PATH, symlinks, or the provider's own update
// cycle: this package never installs for these providers (see
// selfUpdatingProviders), it only observes what their own updater already
// produced. executable is the resolved binary path so State.CLIs carries
// the same field shape as a managed install.
func probeSelfUpdatingVersion(ctx context.Context, name string) (version, executable string, err error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", "", err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, path, "--version").Output()
	if err != nil {
		return "", "", fmt.Errorf("%s --version: %w", name, err)
	}
	firstLine := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if firstLine == "" {
		return "", "", fmt.Errorf("%s --version produced no output", name)
	}
	return firstLine, path, nil
}
