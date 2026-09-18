package server

import (
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/oauth"
	"os"
	"path/filepath"
	"testing"
)

func TestBaseMCPAccountConnectionRequiresExistingExplicitCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.json")
	cfg := mcpclient.MCPServerConfig{OAuth: &oauth.OAuthConfig{TokenFile: path}}
	if connectionState("Linear-base", cfg, nil, "") != connectionAvailable {
		t.Fatal("missing credential marked connected")
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if connectionState("Linear-base", cfg, nil, "") != connectionConnected {
		t.Fatal("explicit base account hidden")
	}
	cfg.OAuth.TokenFile = ""
	if connectionState("Linear", cfg, nil, "") != connectionAvailable {
		t.Fatal("catalog entry promoted to connected")
	}
}
