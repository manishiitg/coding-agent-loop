package gogconfig

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeAndTerminalEnvironment(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "host"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv(HomeEnv, "")
	t.Setenv("AGENTWORKS_GOG_TERMINAL_ACCESS", "")
	if got := Home(); got != filepath.Join(base, "config", "agentworks", "gog") {
		t.Fatalf("XDG home = %s", got)
	}
	home := filepath.Join(base, "shared gog")
	t.Setenv(HomeEnv, home)
	t.Setenv("GOG_KEYRING_PASSWORD", "test-password")
	input := []string{"HOME=/tmp", "GOG_HOME=/stale", "GOG_ACCESS_TOKEN=wrong-account", "PATH=/bin"}
	got := strings.Join(Environment(input, false), "\n")
	if !strings.Contains(got, "GOG_HOME="+home) || !strings.Contains(got, "GOG_KEYRING_PASSWORD=test-password") || strings.Contains(got, "wrong-account") || strings.Contains(got, "/stale") {
		t.Fatal("shared store environment not resolved independently of shell HOME")
	}
	for _, strict := range []bool{true, false} {
		if !strict {
			t.Setenv("AGENTWORKS_GOG_TERMINAL_ACCESS", "false")
		}
		for _, entry := range Environment(input, strict) {
			if strings.HasPrefix(entry, "GOG_") {
				t.Fatal("restricted environment inherited Google credentials")
			}
		}
		if TerminalHome(strict) != "" {
			t.Fatal("restricted terminal received a credential grant")
		}
	}
}
