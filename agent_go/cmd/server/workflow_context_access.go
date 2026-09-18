package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Context paths are untrusted request input. Resolve each existing workflow
// against this request's live user permissions before reading its context or
// granting tool/shell access. An unreadable manifest must never grant access.
func authorizeWorkflowContextPaths(ctx context.Context, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	denied := errors.New("One or more attached workflows are unavailable or you no longer have access. Remove the attachment and select an accessible workflow.")
	claims := GetUserFromContext(ctx)
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, raw := range paths {
		folder := strings.TrimSuffix(strings.TrimSpace(raw), "/")
		parts := strings.Split(folder, "/")
		if len(parts) != 2 || parts[0] != "Workflow" || parts[1] == "" || parts[1] == "." || parts[1] == ".." || strings.ContainsAny(folder, "\\\x00") {
			return nil, denied
		}
		if seen[folder] {
			continue
		}
		manifest, exists, err := ReadWorkflowManifest(ctx, folder)
		if err != nil || !exists || manifest == nil || !userAllowedWorkflowID(claims, manifest.ID) || workflowAccessForManifest(claims, manifest) == WorkflowAccessNone {
			return nil, denied
		}
		seen[folder] = true
		result = append(result, folder)
	}
	return result, nil
}

func contextReferenceReadRoot(userID, folder string) (string, bool) {
	folder = strings.TrimSuffix(strings.TrimSpace(folder), "/")
	if strings.ContainsAny(folder, "\\\x00") {
		return "", false
	}
	parts := strings.Split(folder, "/")
	switch {
	case len(parts) == 2 && parts[0] == "Workflow" && parts[1] != "" && parts[1] != "." && parts[1] != "..":
		return folder, true
	case len(parts) == 4 && parts[0] == "Chats" && parts[1] == "Work" && parts[2] == "projects" && parts[3] != "" && parts[3] != "." && parts[3] != ".." && strings.TrimSpace(userID) != "":
		return agentProfileRuntimeWorkspace(userID, folder), true
	default:
		return "", false
	}
}

// authorizeWorkflowContextPathsWithReadRoots keeps the durable/user-visible
// reference canonical while separately resolving the folder-guard root. Crew
// projects live below _users/<id>/..., but their public project identity is the
// stable Chats/Work/projects/<project> path used by the picker and workflow.json.
func authorizeWorkflowContextPathsWithReadRoots(ctx context.Context, paths []string) ([]string, []string, error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	denied := errors.New("One or more attached workflows or Crew projects are unavailable or you no longer have access. Remove the attachment and select an accessible project.")
	claims := GetUserFromContext(ctx)
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	readRoots := make([]string, 0, len(paths))
	for _, raw := range paths {
		folder := strings.TrimSuffix(strings.TrimSpace(raw), "/")
		parts := strings.Split(folder, "/")
		if strings.ContainsAny(folder, "\\\x00") {
			return nil, nil, denied
		}
		if seen[folder] {
			continue
		}
		userID := ""
		if claims != nil {
			userID = claims.UserID
		}
		readRoot, validShape := contextReferenceReadRoot(userID, folder)
		if !validShape {
			return nil, nil, denied
		}
		switch {
		case len(parts) == 2 && parts[0] == "Workflow" && parts[1] != "" && parts[1] != "." && parts[1] != "..":
			if _, err := authorizeWorkflowContextPaths(ctx, []string{folder}); err != nil {
				return nil, nil, denied
			}
		case len(parts) == 4 && parts[0] == "Chats" && parts[1] == "Work" && parts[2] == "projects" && parts[3] != "" && parts[3] != "." && parts[3] != "..":
			if claims == nil || strings.TrimSpace(claims.UserID) == "" {
				return nil, nil, denied
			}
			rawManifest, exists, err := readFileFromWorkspace(ctx, readRoot+"/product.json")
			if err != nil || !exists {
				return nil, nil, denied
			}
			var manifest productProjectManifest
			if json.Unmarshal([]byte(rawManifest), &manifest) != nil || !strings.EqualFold(strings.TrimSpace(manifest.Product), "work") || strings.TrimSpace(manifest.ID) == "" {
				return nil, nil, denied
			}
		default:
			return nil, nil, denied
		}
		seen[folder] = true
		result = append(result, folder)
		readRoots = append(readRoots, readRoot)
	}
	return result, readRoots, nil
}

// mergeDurableWorkflowContextPaths adds workflow.json links to the transient
// # references supplied by a client. Authorization intentionally remains in
// authorizeWorkflowContextPaths so saved links are rechecked on every turn.
func mergeDurableWorkflowContextPaths(ctx context.Context, selectedFolder string, transient []string) []string {
	selected := strings.TrimSuffix(strings.TrimSpace(selectedFolder), "/")
	if !strings.HasPrefix(selected, "Workflow/") {
		return appendUniqueStrings(nil, transient...)
	}
	manifest, exists, err := ReadWorkflowManifest(ctx, selected)
	if err != nil || !exists || manifest == nil {
		return appendUniqueStrings(nil, transient...)
	}
	return appendUniqueStrings(manifest.WorkflowContextPaths, transient...)
}
