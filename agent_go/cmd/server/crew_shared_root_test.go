package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// sharedCrewStore is a Crew/ root holding two owners' crews and one legacy
// crew still in an owner's tree.
func sharedCrewStore() productProjectStore {
	files := map[string]string{
		"Crew/sde-aaaa1111/product.json":                             `{"schema_version":1,"product":"work","id":"crew-a","owner_id":"alice","title":"SDE","session_id":"work:project:crew-a"}`,
		"Crew/ops-bbbb2222/product.json":                             `{"schema_version":1,"product":"work","id":"crew-b","owner_id":"bob","title":"Ops","session_id":"work:project:crew-b"}`,
		"Crew/nobody-cccc3333/product.json":                          `{"schema_version":1,"product":"work","id":"crew-c","title":"Orphan","session_id":"work:project:crew-c"}`,
		"_users/carol/Chats/Work/projects/old-dddd4444/product.json": `{"schema_version":1,"product":"work","id":"crew-d","title":"Old","session_id":"work:project:crew-d"}`,
	}
	return productProjectStore{
		listPaths: func(_ context.Context, root string) ([]string, bool, error) {
			var paths []string
			for path := range files {
				if strings.HasPrefix(path, root+"/") {
					paths = append(paths, path)
				}
			}
			return paths, len(paths) > 0, nil
		},
		read: func(_ context.Context, path string) (string, bool, error) {
			content, ok := files[path]
			return content, ok, nil
		},
	}
}

func sharedCrewProfile() agentprofiles.Profile {
	profile := routeTestProfile("work", true, "")
	profile.Runtime.Workspace = agentprofiles.WorkspacePolicy{Mode: agentprofiles.WorkspaceModeProject, ProjectsRoot: crewSharedRootName}
	profile.Runtime.Conversation = agentprofiles.ConversationPolicy{Mode: agentprofiles.ConversationModeKeyed, KeyType: agentprofiles.ConversationKeyTypeProject}
	return profile
}

// At the shared root a binding is the resolving user's: their own crews only.
func TestSharedRootBindingFiltersByManifestOwner(t *testing.T) {
	store, profile := sharedCrewStore(), sharedCrewProfile()
	binding, err := resolveProductProjectBindingWithStore(context.Background(), "alice", profile, "crew-a", store)
	if err != nil || binding.WorkspacePath != "Crew/sde-aaaa1111" {
		t.Fatalf("owner binding: %+v %v", binding, err)
	}
	if _, err := resolveProductProjectBindingWithStore(context.Background(), "alice", profile, "crew-b", store); err == nil {
		t.Fatal("alice bound bob's crew as her own")
	}
	// Resolving as the owner is how a reader's binding is verified.
	if binding, err := resolveProductProjectBindingWithStore(context.Background(), "bob", profile, "crew-b", store); err != nil || binding.WorkspacePath != "Crew/ops-bbbb2222" {
		t.Fatalf("bob's binding: %+v %v", binding, err)
	}
	if _, err := resolveProductProjectBindingWithStore(context.Background(), "alice", profile, "crew-c", store); err == nil {
		t.Fatal("an ownerless crew bound")
	}
}

// The catalog lists shared crews by manifest owner and legacy crews by path;
// a crew without an owner is nobody's and is not listed.
func TestCrewCatalogListsSharedAndLegacyCrews(t *testing.T) {
	withMemoryUserDirectory(t, `{"users":[{"id":"alice","username":"alice"},{"id":"bob","username":"bob"},{"id":"carol","username":"carol"}]}`)
	catalog, err := listCrewCatalog(context.Background(), sharedCrewStore())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, entry := range catalog {
		got[entry.Root] = entry.OwnerID
	}
	want := map[string]string{
		"Crew/sde-aaaa1111":                             "alice",
		"Crew/ops-bbbb2222":                             "bob",
		"_users/carol/Chats/Work/projects/old-dddd4444": "carol",
	}
	if len(got) != len(want) {
		t.Fatalf("catalog: %v", got)
	}
	for root, owner := range want {
		if got[root] != owner {
			t.Fatalf("catalog %s owner %q, want %q (all: %v)", root, got[root], owner, got)
		}
	}
}

// A browser key is shared by a crew's owner and readers, and a migrated crew
// keeps the key of its old root so its saved browser profile survives.
func TestBrowserProjectKeyForSharedCrews(t *testing.T) {
	stubCrewLookups(t, map[string]string{"Crew/sde-aaaa1111": "alice", "Crew/new-eeee5555": "alice"},
		map[string]string{"_users/alice/Chats/Work/projects/sde-aaaa1111": "Crew/sde-aaaa1111"})
	for _, user := range []string{"alice", "bob"} {
		if got := browserProjectKey(user, "Crew/sde-aaaa1111"); got != "_users/alice/Chats/Work/projects/sde-aaaa1111" {
			t.Fatalf("%s: migrated crew key %q", user, got)
		}
		if got := browserProjectKey(user, "Crew/new-eeee5555"); got != "Crew/new-eeee5555" {
			t.Fatalf("%s: new crew key %q", user, got)
		}
	}
	// An old spelling of the migrated crew lands on the same key.
	if got := browserProjectKey("alice", "Chats/Work/projects/sde-aaaa1111"); got != "_users/alice/Chats/Work/projects/sde-aaaa1111" {
		t.Fatalf("legacy spelling key %q", got)
	}
}

// Only a crew's owner may scope a Slack bot to it; being under the Crew/
// projects root proves nothing.
func TestSlackScopeOnSharedCrewNeedsOwnership(t *testing.T) {
	stubCrewLookups(t, map[string]string{"Crew/sde-aaaa1111": "alice"}, nil)
	profile := sharedCrewProfile()
	if !productWorkspaceUnderCallerRoot(profile, "alice", "Crew/sde-aaaa1111") {
		t.Fatal("owner refused")
	}
	if productWorkspaceUnderCallerRoot(profile, "bob", "Crew/sde-aaaa1111") {
		t.Fatal("another user may manage the crew's Slack bot")
	}
}

// The owner's transcripts live in the crew; a reader's never do.
func TestOwnedCrewRootForTranscripts(t *testing.T) {
	stubCrewLookups(t, map[string]string{"Crew/sde-aaaa1111": "alice"}, nil)
	if root, ok := ownedCrewRoot("alice", "Crew/sde-aaaa1111"); !ok || root != "Crew/sde-aaaa1111" {
		t.Fatalf("owner: %q %v", root, ok)
	}
	if _, ok := ownedCrewRoot("bob", "Crew/sde-aaaa1111"); ok {
		t.Fatal("reader got the owner's transcript folder")
	}
	path, ok := workProjectChatHistoryConversationPath("alice", "Crew/sde-aaaa1111", "sid", time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC))
	if !ok || !strings.HasPrefix(path, "Crew/sde-aaaa1111/builder/") {
		t.Fatalf("owner transcript path %q %v", path, ok)
	}
}
