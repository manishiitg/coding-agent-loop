// Package gogconfig defines the Google CLI store shared by the application
// and trusted agent terminals. Resolve it before a sandbox changes HOME.
package gogconfig

import (
	"os"
	"path/filepath"
	"strings"
)

const HomeEnv = "GOG_HOME"

func Home() string {
	if v := strings.TrimSpace(os.Getenv(HomeEnv)); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
		return filepath.Clean(v)
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "agentworks", "gog")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "agentworks-gog")
	}
	return filepath.Join(home, ".config", "agentworks", "gog")
}

// TerminalHome grants the real CLI's store to ordinary trusted terminals.
// Restricted agent profiles never inherit this host credential grant. An
// operator may also disable it for an entire deployment.
func TerminalHome(strict bool) string {
	if strict || strings.EqualFold(strings.TrimSpace(os.Getenv("AGENTWORKS_GOG_TERMINAL_ACCESS")), "false") {
		return ""
	}
	return Home()
}

// Environment uses the execution host's store, independently of the shell's
// HOME/XDG settings. Headless gog installations may use a password-protected
// file keyring; pass its configured backend/password without logging values.
func Environment(env []string, strict bool) []string {
	out := make([]string, 0, len(env)+3)
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key == HomeEnv || key == "GOG_KEYRING_BACKEND" || key == "GOG_KEYRING_PASSWORD" || key == "GOG_ACCESS_TOKEN" {
			continue
		}
		out = append(out, entry)
	}
	if home := TerminalHome(strict); home != "" {
		out = append(out, HomeEnv+"="+home)
		for _, key := range []string{"GOG_KEYRING_BACKEND", "GOG_KEYRING_PASSWORD"} {
			if value, ok := os.LookupEnv(key); ok {
				out = append(out, key+"="+value)
			}
		}
	}
	return out
}
