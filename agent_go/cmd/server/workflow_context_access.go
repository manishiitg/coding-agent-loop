package server

import (
	"context"
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
