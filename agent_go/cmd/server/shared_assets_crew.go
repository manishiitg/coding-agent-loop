package server

import (
	"net/http"
	"path"
	"strings"

	wf "github.com/manishiitg/coding-agent-loop/workspace/workflowfiles"
)

// Crews are shared like workflows: anyone who may open a crew (Crew Run
// mode — the owner, or any signed-in user with the Crew product) may open
// its file and folder links read-only. The crew's private areas stay
// hidden exactly as in the mediated crew reader (crew_directory.go):
// builder/ transcripts, db/ run databases, and the raw root manifests.

// crewLinkReadAllowed reports whether claims may read the crew rooted at
// crewRoot ("_users/<owner>/Chats/Work/projects/<p>"). Tests replace it.
var crewLinkReadAllowed = func(api *StreamingAPI, claims *UserClaims, crewRoot string) bool {
	return api.crewBrowserAccess(claims, crewRoot) != WorkflowAccessNone
}

// crewReaderSharedAsset resolves a link to another owner's crew for a
// signed-in reader. handled is false when the link is not a cross-user crew
// link, so the caller applies the ordinary rules. crewRootView is true when
// the link addresses the crew root itself, whose listing must be filtered.
func (api *StreamingAPI) crewReaderSharedAsset(w http.ResponseWriter, r *http.Request, full string) (root, relative string, crewRootView, handled, ok bool) {
	claims := GetUserFromContext(r.Context())
	if claims == nil {
		return "", "", false, false, false
	}
	caller := publicWorkspaceUserID(r)
	clean, err := wf.CleanRelative(full)
	if err != nil || clean != full {
		return "", "", false, false, false
	}
	// A shared crew root (Crew/<id>): one path for owner and readers. The
	// owner sees the whole crew; a reader gets the same confined view as a
	// legacy cross-user crew link.
	if ref, isCrew := resolveCrewPath(r.Context(), caller, clean); isCrew && ref.Shared {
		access := crewAccessFor(claims, ref)
		if access == crewAccessNone {
			externalError(w, 403, "forbidden", "You do not have access to this crew.")
			return "", "", false, true, false
		}
		if claims.AccessToken != nil && !claims.AccessToken.Allows("files:read") {
			externalError(w, 403, "insufficient_scope", "This token does not allow this asset.")
			return "", "", false, true, false
		}
		relative := ref.Rest
		if access == crewAccessReader && relative != "" {
			if _, confined := confineSharedProjectPath(ref.Root, relative); !confined {
				externalError(w, 403, "protected_path", "This part of the crew is private to its owner.")
				return "", "", false, true, false
			}
		}
		if relative == "" {
			relative = "."
		}
		if wf.Private(relative) {
			externalError(w, 403, "protected_path", "Private workspace files are not shareable.")
			return "", "", false, true, false
		}
		return ref.Root, relative, access == crewAccessReader && ref.Rest == "", true, true
	}
	if !IsMultiUserMode() {
		return "", "", false, false, false
	}
	owner := strings.TrimSpace(r.URL.Query().Get("uid"))
	parts := strings.Split(clean, "/")
	if parts[0] == "_users" && len(parts) >= 3 {
		owner, parts = parts[1], parts[2:]
	}
	if owner == "" || owner == caller || sanitizeUserIDForPath(owner) != owner {
		return "", "", false, false, false
	}
	if len(parts) < 4 || parts[0] != "Chats" || parts[1] != "Work" || parts[2] != "projects" || parts[3] == "" {
		return "", "", false, false, false
	}
	crewRoot := path.Join("_users", owner, "Chats/Work/projects", parts[3])
	// A link made before the crew moved to Crew/<id> follows it there.
	if moved := crewPathAliases.lookup(r.Context(), crewRoot); moved != "" {
		ref, _ := resolveCrewPath(r.Context(), caller, moved+"/"+strings.Join(parts[4:], "/"))
		ref.Rest = strings.Trim(ref.Rest, "/")
		access := crewAccessFor(claims, ref)
		if access == crewAccessNone {
			externalError(w, 403, "forbidden", "You do not have access to this crew.")
			return "", "", false, true, false
		}
		relative := ref.Rest
		if access == crewAccessReader && relative != "" {
			if _, confined := confineSharedProjectPath(ref.Root, relative); !confined {
				externalError(w, 403, "protected_path", "This part of the crew is private to its owner.")
				return "", "", false, true, false
			}
		}
		if relative == "" {
			relative = "."
		}
		return ref.Root, relative, access == crewAccessReader && ref.Rest == "", true, true
	}
	if !crewLinkReadAllowed(api, claims, crewRoot) {
		externalError(w, 403, "forbidden", "You do not have access to this crew.")
		return "", "", false, true, false
	}
	if claims.AccessToken != nil && !claims.AccessToken.Allows("files:read") {
		externalError(w, 403, "insufficient_scope", "This token does not allow this asset.")
		return "", "", false, true, false
	}
	crewRelative := strings.Join(parts[4:], "/")
	if crewRelative != "" {
		if _, confined := confineSharedProjectPath(crewRoot, crewRelative); !confined {
			externalError(w, 403, "protected_path", "This part of the crew is private to its owner.")
			return "", "", false, true, false
		}
	}
	relative = strings.Join(parts[1:], "/")
	if wf.Private(relative) {
		externalError(w, 403, "protected_path", "Private workspace files are not shareable.")
		return "", "", false, true, false
	}
	return path.Join("_users", owner, "Chats"), relative, crewRelative == "", true, true
}

// crewRootListingVisible hides the crew's private areas from a reader's
// listing of the crew root. name is relative to the crew root.
func crewRootListingVisible(name string) bool {
	name = strings.Trim(name, "/")
	if sharedProjectExcludedRootFiles[name] {
		return false
	}
	top := name
	if index := strings.IndexByte(name, '/'); index >= 0 {
		top = name[:index]
	}
	return !sharedProjectExcludedTopSegments[top]
}
