package oauthflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/oauth"
)

var oauthFlowTestCmd = &cobra.Command{
	Use:   "oauth-flow",
	Short: "Test OAuth flow with Notion MCP server",
	Long: `Test OAuth authentication flow end-to-end with Notion MCP.

This test:
1. Configures Notion MCP with OAuth auto-discovery
2. Starts OAuth login flow (opens browser)
3. Waits for user to authenticate
4. Verifies token is saved correctly
5. Tests connection with cached token
6. Tests logout

Note: This test doesn't use traditional asserts. Logs are analyzed (manually or by LLM) to verify success.
See criteria.md in the oauth-flow folder for detailed log analysis criteria.

Examples:
  mcpagent-test test oauth-flow --log-file logs/oauth-test.log
  mcpagent-test test oauth-flow --verbose --log-file logs/oauth-test.log`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== OAuth Flow E2E Test ===")
		logger.Info("This test requires manual browser interaction")

		if err := testOAuthFlow(logger); err != nil {
			return fmt.Errorf("OAuth flow test failed: %w", err)
		}

		logger.Info("✅ OAuth flow test passed!")
		logger.Info("")
		logger.Info("📋 For detailed verification, see criteria.md in cmd/testing/oauth-flow/")
		return nil
	},
}

// GetOAuthFlowTestCmd returns the OAuth flow test command
func GetOAuthFlowTestCmd() *cobra.Command {
	return oauthFlowTestCmd
}

// testOAuthFlow runs the complete OAuth flow test
func testOAuthFlow(log loggerv2.Logger) error {
	ctx := context.Background()

	// Step 1: Create temporary OAuth config
	log.Info("--- Step 1: Create Notion MCP Config with OAuth ---")
	configPath, tokenFile, cleanup, err := createNotionOAuthConfig(log)
	if err != nil {
		return err
	}
	defer cleanup()

	// Step 2: Test OAuth Login
	log.Info("--- Step 2: Test OAuth Login (Browser Flow) ---")
	if err := testOAuthLogin(ctx, configPath, tokenFile, log); err != nil {
		return err
	}

	// Step 3: Verify Token File
	log.Info("--- Step 3: Verify Token File Created ---")
	if err := verifyTokenFile(tokenFile, log); err != nil {
		return err
	}

	// Step 4: Test Connection with Cached Token
	log.Info("--- Step 4: Test Connection with Cached Token ---")
	if err := testConnectionWithToken(ctx, configPath, log); err != nil {
		return err
	}

	// Step 5: Test Token Refresh (optional - requires waiting)
	log.Info("--- Step 5: Test Token Refresh (Skipped - requires token expiry) ---")
	log.Info("ℹ️  To test token refresh manually, edit token file and set expiry to past time")

	// Step 6: Test Logout
	log.Info("--- Step 6: Test OAuth Logout ---")
	if err := testOAuthLogout(tokenFile, log); err != nil {
		return err
	}

	return nil
}

func createNotionOAuthConfig(log loggerv2.Logger) (string, string, func(), error) {
	tmpDir := os.TempDir()
	configPath := tmpDir + "/notion-oauth-test.json"
	tokenFile := tmpDir + "/notion-token.json"

	config := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"Notion": map[string]interface{}{
				"url":      "https://mcp.notion.com/mcp",
				"protocol": "http",
				"oauth": map[string]interface{}{
					"auto_discover": true,
					"use_pkce":      true,
					"token_file":    tokenFile,
				},
			},
		},
	}

	// Write config file
	configData, _ := json.MarshalIndent(config, "", "  ")
	err := os.WriteFile(configPath, configData, 0600)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to write config: %w", err)
	}

	log.Info("✅ Created OAuth config",
		loggerv2.String("path", configPath),
		loggerv2.String("token_file", tokenFile))

	cleanup := func() {
		_ = os.Remove(configPath)
		_ = os.Remove(tokenFile)
		log.Info("🧹 Cleaned up temporary files")
	}

	return configPath, tokenFile, cleanup, nil
}

func testOAuthLogin(ctx context.Context, configPath, tokenFile string, log loggerv2.Logger) error {
	log.Info("Starting OAuth login flow...")
	log.Info("⚠️  Your browser will open - please authenticate with Notion")

	// Load config
	cfg, err := mcpclient.LoadConfig(configPath, log)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	notionConfig := cfg.MCPServers["Notion"]
	if notionConfig.OAuth == nil {
		return fmt.Errorf("OAuth config not found")
	}

	// Create OAuth manager
	manager := oauth.NewManager(notionConfig.OAuth, log)

	// Auto-discover endpoints from 401 response
	log.Info("Auto-discovering OAuth endpoints from Notion...")
	//nolint:gosec // G107: Test URL
	resp, err := http.Get(notionConfig.URL)
	if err != nil {
		return fmt.Errorf("failed to reach Notion MCP: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode == 401 {
		endpoints, err := oauth.DiscoverFromResponse(resp)
		if err != nil {
			return fmt.Errorf("failed to discover endpoints: %w", err)
		}

		log.Info("✅ Discovered OAuth endpoints",
			loggerv2.String("auth_url", endpoints.AuthURL),
			loggerv2.String("token_url", endpoints.TokenURL))

		manager.UpdateEndpoints(endpoints.AuthURL, endpoints.TokenURL)
	}

	// Start OAuth flow
	log.Info("🔐 Opening browser for authentication...")
	log.Info("Please complete the authentication in your browser")

	token, err := manager.StartAuthFlow(ctx)
	if err != nil {
		return fmt.Errorf("OAuth flow failed: %w", err)
	}

	log.Info("✅ Successfully authenticated",
		loggerv2.String("expires", token.Expiry.Format(time.RFC3339)))

	return nil
}

func verifyTokenFile(tokenFile string, log loggerv2.Logger) error {
	log.Info("Checking token file...", loggerv2.String("path", tokenFile))

	// Check file exists
	info, err := os.Stat(tokenFile)
	if err != nil {
		return fmt.Errorf("token file not found: %w", err)
	}

	// Check permissions
	mode := info.Mode()
	if mode.Perm() != 0600 {
		log.Warn("⚠️  Token file permissions incorrect",
			loggerv2.String("got", fmt.Sprintf("%o", mode.Perm())),
			loggerv2.String("want", "0600"))
	} else {
		log.Info("✅ Token file permissions correct", loggerv2.String("mode", "0600"))
	}

	// Check file contents
	//nolint:gosec // G304: Test file inclusion
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return fmt.Errorf("failed to read token file: %w", err)
	}

	var token map[string]interface{}
	if err := json.Unmarshal(data, &token); err != nil {
		return fmt.Errorf("token file is not valid JSON: %w", err)
	}

	// Verify token structure
	requiredFields := []string{"access_token", "token_type", "expiry"}
	for _, field := range requiredFields {
		if _, ok := token[field]; !ok {
			return fmt.Errorf("token missing required field: %s", field)
		}
	}

	log.Info("✅ Token file structure valid",
		loggerv2.Int("size_bytes", len(data)),
		loggerv2.Int("field_count", len(token)))

	return nil
}

func testConnectionWithToken(ctx context.Context, configPath string, log loggerv2.Logger) error {
	log.Info("Attempting to connect using cached token...")

	// Load config
	cfg, err := mcpclient.LoadConfig(configPath, log)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	notionConfig := cfg.MCPServers["Notion"]

	// Create MCP client
	client := mcpclient.New(notionConfig, log)

	// Connect (should use cached token)
	log.Info("Connecting to Notion MCP...")
	err = client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	log.Info("✅ Connected successfully using cached token")

	// Try to list tools
	tools, err := client.ListTools(ctx)
	if err != nil {
		log.Warn("⚠️  Failed to list tools", loggerv2.Error(err))
	} else {
		log.Info("✅ Listed tools",
			loggerv2.Int("tool_count", len(tools)))
	}

	return nil
}

func testOAuthLogout(tokenFile string, log loggerv2.Logger) error {
	log.Info("Removing token file...", loggerv2.String("path", tokenFile))

	err := os.Remove(tokenFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove token: %w", err)
	}

	log.Info("✅ Token removed successfully")

	// Verify file is gone
	_, err = os.Stat(tokenFile)
	if err == nil {
		return fmt.Errorf("token file still exists after logout")
	}

	log.Info("✅ Logout complete - token file deleted")
	return nil
}
