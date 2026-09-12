package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMuseCLILocalAuthState pins a real bug found live 2026-09-11: the
// previous implementation shelled out to `muse auth status`, which is not a
// real subcommand (`muse auth`'s only verb is `set`) -- it always failed
// regardless of login state, so every muse-cli install showed as logged out
// (the workflow LLM picker's permanent "Needs setup" for Muse despite a
// real, working stored login). The fix checks the actual credential file
// `muse login` writes instead.
func TestMuseCLILocalAuthState(t *testing.T) {
	if _, err := exec.LookPath("muse"); err != nil {
		t.Skip("muse CLI not in PATH: without it the function short-circuits to (false, false) regardless of this test's fixture")
	}

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	authenticated, conclusive := museCLILocalAuthState()
	if authenticated || !conclusive {
		t.Fatalf("no credential file: authenticated=%v conclusive=%v, want false,true", authenticated, conclusive)
	}

	authPath := filepath.Join(dir, "muse", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"access_token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	authenticated, conclusive = museCLILocalAuthState()
	if !authenticated || !conclusive {
		t.Fatalf("real credential file: authenticated=%v conclusive=%v, want true,true", authenticated, conclusive)
	}

	if err := os.WriteFile(authPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	authenticated, _ = museCLILocalAuthState()
	if authenticated {
		t.Fatal("an empty credential file must not count as authenticated")
	}
}

func TestMuseAuthJSONPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test-home")
	if got, want := museAuthJSONPath(), "/tmp/xdg-test-home/muse/auth.json"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
