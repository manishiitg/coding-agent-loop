package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	pathpkg "path"
	"strings"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

func listWorkspaceChildFolders(ctx context.Context, folderPath string) ([]string, error) {
	listing, exists, err := listWorkspaceFolder(ctx, folderPath, 1)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []string{}, nil
	}

	children := extractWorkspaceChildFolders(listing, folderPath)
	out := make([]string, 0, len(children))
	for _, child := range children {
		out = append(out, child.FilePath)
	}
	return out, nil
}

func listWorkspaceChildFolderNames(ctx context.Context, folderPath string) ([]string, error) {
	paths, err := listWorkspaceChildFolders(ctx, folderPath)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, pathpkg.Base(p))
	}
	return names, nil
}

func extractWorkspaceChildFolders(listing virtualtools.WorkspaceFolderListing, folderPath string) []virtualtools.WorkspaceFolderItem {
	// The workspace API can return the listing in a hybrid shape: the root
	// folder with its children nested AND the same child folders repeated as
	// flat top-level entries. Dedupe by FilePath so each folder is emitted
	// exactly once.
	out := make([]virtualtools.WorkspaceFolderItem, 0)
	seen := make(map[string]struct{})
	for _, item := range listing {
		switch {
		case item.FilePath == folderPath:
			for _, child := range item.Children {
				if child.Type != "folder" {
					continue
				}
				if _, ok := seen[child.FilePath]; ok {
					continue
				}
				seen[child.FilePath] = struct{}{}
				out = append(out, child)
			}
		case item.Type == "folder":
			if _, ok := seen[item.FilePath]; ok {
				continue
			}
			seen[item.FilePath] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

func createWorkspaceFolder(ctx context.Context, folderPath string) error {
	requestBody, err := json.Marshal(map[string]string{"folder_path": folderPath})
	if err != nil {
		return fmt.Errorf("failed to marshal create folder request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, getWorkspaceAPIURL()+"/api/folders", strings.NewReader(string(requestBody)))
	if err != nil {
		return fmt.Errorf("failed to create folder request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API to create folder: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read create folder response: %w", err)
	}
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
}
