package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
	case strings.TrimSpace(userID) != "" && isCrewReference(userID, folder):
		// Crews are shared server-wide. Any spelling (Crew/<id>, the owner's
		// Chats/..., another owner's physical _users/...) reads the crew's
		// current root, following a migration alias.
		ref, _ := resolveCrewPath(context.Background(), userID, folder)
		return ref.Root, true
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
			logContextDenial(claims, folder, "invalid characters")
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
			logContextDenial(claims, folder, "not a workflow or crew path")
			return nil, nil, denied
		}
		switch {
		case len(parts) == 2 && parts[0] == "Workflow" && parts[1] != "" && parts[1] != "." && parts[1] != "..":
			if _, err := authorizeWorkflowContextPaths(ctx, []string{folder}); err != nil {
				logContextDenial(claims, folder, "workflow not readable by this user")
				return nil, nil, denied
			}
		case isCrewReference(userID, folder):
			if claims == nil || strings.TrimSpace(claims.UserID) == "" {
				logContextDenial(claims, folder, "no user for a crew attachment")
				return nil, nil, denied
			}
			rawManifest, exists, err := readFileFromWorkspace(ctx, readRoot+"/product.json")
			if err != nil || !exists {
				logContextDenial(claims, folder, "crew has no product.json")
				return nil, nil, denied
			}
			var manifest productProjectManifest
			if json.Unmarshal([]byte(rawManifest), &manifest) != nil || !strings.EqualFold(strings.TrimSpace(manifest.Product), "work") || strings.TrimSpace(manifest.ID) == "" {
				logContextDenial(claims, folder, "crew product.json is not a Work crew")
				return nil, nil, denied
			}
			// The same rule as every other way into a crew: a private crew
			// is its owners' alone, and a crew without owners is nobody's.
			if ref, ok := resolveCrewPath(ctx, claims.UserID, folder); !ok || crewAccessFor(claims, ref) == crewAccessNone {
				logContextDenial(claims, folder, "crew not accessible to this user")
				return nil, nil, denied
			}
		default:
			logContextDenial(claims, folder, "unsupported attachment path")
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

// isCrewReference reports whether folder names a crew's root in any spelling.
func isCrewReference(userID, folder string) bool {
	ref, ok := parseCrewPath(userID, folder)
	return ok && ref.Rest == ""
}

// admitTurnContextPaths merges a turn's saved workflow links with its
// one-message # references and authorizes them, recording the allowed paths
// and their read roots on req. handleQuery and the bot dry run share it.
func admitTurnContextPaths(ctx context.Context, req *QueryRequest) error {
	req.WorkflowContextPaths = mergeDurableWorkflowContextPaths(ctx, req.SelectedFolder, req.WorkflowContextPaths)
	contextPaths, contextReadPaths, err := authorizeWorkflowContextPathsWithReadRoots(workflowContextAuthorizationContext(ctx), req.WorkflowContextPaths)
	if err != nil {
		return err
	}
	req.WorkflowContextPaths = contextPaths
	req.authorizedWorkflowContextReadPaths = contextReadPaths
	return nil
}

// workflowContextAuthorizationContext is the identity a turn's attachments
// are checked against. A bot turn runs on its resource owner's behalf (the
// crew or trigger owner, execution_principal.go), and the attachments saved on
// that crew are the owner's context: they are checked as the owner, so a crew
// answering in Slack has the same attached context as in the owner's web chat
// (user decision 2026-09-26). Every other turn is checked as its caller.
func workflowContextAuthorizationContext(ctx context.Context) context.Context {
	claims := GetUserFromContext(ctx)
	if claims == nil || claims.ExecutionPrincipal == nil {
		return ctx
	}
	ownerID := strings.TrimSpace(claims.ExecutionPrincipal.ResourceOwnerID)
	if ownerID == "" {
		return ctx
	}
	owner := &UserClaims{UserID: ownerID, Username: ownerID}
	if record := directoryUserFor(ownerID, "", ""); record != nil {
		owner.Username = record.Username
		owner.Email = record.Email
	}
	return context.WithValue(ctx, UserContextKey, owner)
}

// logContextDenial records which attachment refused a turn and why; the
// user-facing error is deliberately generic.
func logContextDenial(claims *UserClaims, folder, reason string) {
	userID, principal := "", ""
	if claims != nil {
		userID = claims.UserID
		if claims.ExecutionPrincipal != nil {
			principal = claims.ExecutionPrincipal.Kind
		}
	}
	log.Printf("[CONTEXT_ACCESS] attachment %q refused for user=%s principal=%s: %s", folder, userID, principal, reason)
}
