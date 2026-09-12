package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

// capture is a Builder command, never an upstream CLI command. Resolve its
// workspace and permissions from the invoking session, not tool arguments.
func (e *Executor) handleCapture(ctx context.Context, session string, args []string, cfg *common.SessionShellConfig, guard *FolderGuardConfig) (string, error) {
	if len(args) != 1 || (args[0] != "start" && args[0] != "stop" && args[0] != "status") {
		return "", fmt.Errorf("usage: agent_browser(command=\"capture\", args=[\"start\"|\"stop\"|\"status\"], session=\"main\"); workspace destination is automatic")
	}
	if cfg == nil || guard == nil || !guard.Enabled {
		return "", fmt.Errorf("CAPTURE_ACCESS_DENIED: capture requires a workflow session with workspace permissions")
	}
	workspace := captureWorkspace(cfg)
	if workspace == "" || workspace != path.Clean(workspace) || !strings.HasPrefix(workspace, "Workflow/") || len(strings.Split(workspace, "/")) != 2 {
		return "", fmt.Errorf("CAPTURE_ACCESS_DENIED: no owning workflow is configured for this session")
	}
	owner, _ := ctx.Value(common.WorkflowSessionIDKey).(string)
	if owner == "" {
		owner, _ = ctx.Value(common.ChatSessionIDKey).(string)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"action": args[0], "workspace_path": workspace, "owner_session": owner,
		"working_directory": cfg.WorkingDir, "folder_guard": guard,
	})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Client.WorkspaceAPIURL+"/api/browser/live/"+url.PathEscape(session)+"/recording", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-Token", os.Getenv("WORKSPACE_API_TOKEN"))
	resp, err := e.Client.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("capture request failed; check capture status before retrying: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || !json.Valid(body) {
		return "", fmt.Errorf("invalid capture response; check capture status before retrying")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("capture returned HTTP %d: %s", resp.StatusCode, body)
	}
	var state struct {
		Recording bool   `json:"recording"`
		Owner     string `json:"owner_session"`
	}
	if json.Unmarshal(body, &state) == nil {
		GetSessionTracker().SetCapture(session, state.Recording, state.Owner, workspace)
	}
	return string(body), nil
}

func captureWorkspace(cfg *common.SessionShellConfig) string {
	if cfg == nil {
		return ""
	}
	workspace := cfg.WorkflowPath
	if workspace == "" {
		// Older sessions have only a trusted working directory. Workflow roots
		// are Workflow/<name>; never infer ownership from an agent's arguments.
		parts := strings.Split(path.Clean(cfg.WorkingDir), "/")
		if len(parts) >= 2 && parts[0] == "Workflow" && parts[1] != ".." {
			workspace = strings.Join(parts[:2], "/")
		}
	}
	return workspace
}
