package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RunSlackCLI uses the saved connector token only in the CLI child's environment.
func RunSlackCLI(ctx context.Context, method string, parameters map[string]interface{}) (string, error) {
	slackFSMu.Lock()
	cfg, err := loadSlackConfigFromDisk()
	slackFSMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("could not load Slack connector credentials")
	}
	if cfg == nil || !cfg.Enabled || cfg.BotToken == "" {
		return "", fmt.Errorf("Slack bot is disabled or its token is missing; configure Setup > Connectors > Slack")
	}
	binary, err := exec.LookPath("slack")
	if err != nil {
		for _, candidate := range []string{filepath.Join(os.Getenv("HOME"), ".slack", "bin", "slack"), filepath.Join(os.Getenv("HOME"), ".local", "bin", "slack")} {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				binary = candidate
				break
			}
		}
		if binary == "" {
			return "", fmt.Errorf("Slack CLI is not installed on the backend; install the official Slack CLI")
		}
	}
	return executeSlackCLI(ctx, binary, cfg.BotToken, method, parameters)
}

type slackCLIOutput struct{ bytes.Buffer }

func (b *slackCLIOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2*1024*1024 {
		return 0, fmt.Errorf("Slack response exceeds 2MB; use a smaller page")
	}
	return b.Buffer.Write(p)
}
func executeSlackCLI(ctx context.Context, binary, token, method string, parameters map[string]interface{}) (string, error) {
	body, err := json.Marshal(parameters)
	if err != nil {
		return "", fmt.Errorf("invalid Slack API parameters")
	}
	dir, err := os.MkdirTemp("", "agentworks-slack-cli-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--skip-update", "--no-color", "--config-dir", dir, "api", method, "--json", string(body))
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + dir, "SLACK_BOT_TOKEN=" + token}
	var out, stderr slackCLIOutput
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	safe := strings.ReplaceAll(out.String(), token, "[redacted]")
	var response map[string]interface{}
	if json.Unmarshal([]byte(safe), &response) != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("Slack CLI timed out; no automatic retry was attempted")
		}
		if runErr != nil {
			return "", fmt.Errorf("Slack CLI failed before returning an API response; check backend CLI installation and connectivity")
		}
		return "", fmt.Errorf("Slack CLI returned an invalid API response")
	}
	if ok, _ := response["ok"].(bool); !ok {
		code, _ := response["error"].(string)
		needed, _ := response["needed"].(string)
		if code == "missing_scope" {
			return "", fmt.Errorf("Slack permission missing: %s. Add these scopes in OAuth & Permissions, reinstall the app, and save the updated token in Connectors", needed)
		}
		return "", fmt.Errorf("Slack API rejected %s: %s", method, code)
	}
	return safe, nil
}
