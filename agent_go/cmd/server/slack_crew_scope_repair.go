package server

import (
	"context"
	"log"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

// repairLogicalCrewSlackConnections rewrites crew Slack apps saved with a
// crew's logical path ("Chats/Work/projects/<id>") to the owner's physical
// folder. A logical scope has no owner, so the app's own crew could not be
// resolved and every mention got "This Slack route is no longer configured"
// (RTS 2026-09-26). The owner is the one user whose tree holds that project;
// zero or several candidates leave the connection untouched. When the crew
// has no Slack app selected yet, the repaired app is selected: that is the
// step the original save failed to complete. Channel routes are kept.
func repairLogicalCrewSlackConnections(ctx context.Context, svc *services.SlackService) {
	if svc == nil {
		return
	}
	for _, conn := range svc.ListConnections() {
		profileID := strings.TrimSpace(conn.ProfileID)
		logical := strings.Trim(filepath.ToSlash(strings.TrimSpace(conn.WorkspacePath)), "/")
		if profileID == "" || !strings.HasPrefix(logical, "Chats/") {
			continue
		}
		owners := crewProjectOwners(ctx, profileID, logical)
		if len(owners) != 1 {
			log.Printf("[SLACK] crew app %s keeps logical scope %s: %d candidate owners", conn.ID, logical, len(owners))
			continue
		}
		physical := agentProfileRuntimeWorkspace(owners[0], logical)
		if _, err := svc.UpdateSlackConnection(ctx, conn.ID, services.SlackConnectionInput{
			Enabled:       conn.Enabled,
			WorkspacePath: physical,
			ProfileID:     profileID,
		}); err != nil {
			log.Printf("[SLACK] crew app %s scope repair failed: %v", conn.ID, err)
			continue
		}
		log.Printf("[SLACK] crew app %s scope repaired: %s -> %s", conn.ID, logical, physical)
		if selected, err := productSlackConnectionID(ctx, profileID, physical); err == nil && selected == "" {
			if err := updateProductSlackConnectionID(ctx, profileID, physical, conn.ID); err != nil {
				log.Printf("[SLACK] crew app %s could not be selected for %s: %v", conn.ID, physical, err)
			} else {
				log.Printf("[SLACK] crew app %s selected for %s", conn.ID, physical)
			}
		}
	}
}

// crewProjectOwners lists the users whose tree holds the project manifest at
// the logical crew path.
func crewProjectOwners(ctx context.Context, profileID, logical string) []string {
	candidates := map[string]struct{}{}
	if dir, err := loadUserDirectory(); err == nil && dir != nil {
		for _, user := range dir.Users {
			if id := strings.TrimSpace(user.ID); id != "" {
				candidates[id] = struct{}{}
			}
		}
	}
	if id := strings.TrimSpace(GetDefaultUserID()); id != "" {
		candidates[id] = struct{}{}
	}
	var owners []string
	for userID := range candidates {
		physical := agentProfileRuntimeWorkspace(userID, logical)
		if _, found, err := readProjectRuntimeManifest(ctx, profileID, physical); err == nil && found {
			owners = append(owners, userID)
		}
	}
	return owners
}
