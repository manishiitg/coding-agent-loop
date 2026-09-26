package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
)

// End-to-end bot tests (docs/design/bot_destination_scope.md): each feeds a
// synthetic Slack mention through the real inbound path — the app's route
// and mention message, the production-wired bot manager's routing and turn
// building, then the query boundary's revalidation and target access — and
// stops before the model. They start from what the UI saves, not from
// hand-built physical paths, which is how the crew bugs of 2026-09-25/26
// slipped through step-by-step tests.

const dryRunOwnerEmail = "owner@example.com"

type botDryRunWorld struct {
	*crewRunModeFixture
	slack *services.SlackService
}

func newBotDryRunWorld(t *testing.T) botDryRunWorld {
	t.Helper()
	fx := newCrewRunModeFixture(t)
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"aman","email":"`+dryRunOwnerEmail+`","can_create":true},{"id":"reader","username":"vaibhav","email":"reader@example.com","can_create":true},{"id":"stranger","username":"stranger","can_create":true}]}`)
	resetSlackServiceForTest(t)
	store, err := chathistory.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fx.api.chatStore = store
	svc, err := ensureSlackService()
	if err != nil {
		t.Fatal(err)
	}
	manager := services.NewBotConversationManager(store, "", os.Getenv("WORKSPACE_API_URL"))
	fx.api.wireBotManager(manager)
	manager.RegisterConnector(svc)
	fx.api.botManager = manager
	fx.api.scheduler = NewSchedulerService(fx.api)
	// Match the production Work profile: crews carry their attached
	// workflows and crews into every turn.
	profile := fx.profile
	profile.Runtime.Capabilities.WorkflowReferences = agentprofiles.CapabilityOptional
	registry := agentprofiles.NewRegistry()
	if err := registry.RegisterProfile(profile); err != nil {
		t.Fatal(err)
	}
	fx.profile, fx.api.agentProfiles = profile, registry
	t.Cleanup(func() { services.SetDedicatedSlackRouteFunc(nil) })
	// The fixture pins an LLM the test profile does not offer; a crew
	// without one uses the profile's default.
	fx.mock.mu.Lock()
	var runtime map[string]any
	if err := json.Unmarshal([]byte(fx.mock.files[crewRunModeOwnerRoot+"/workflow.json"]), &runtime); err != nil {
		t.Fatal(err)
	}
	if caps, ok := runtime["capabilities"].(map[string]any); ok {
		delete(caps, "llm_config")
	}
	raw, _ := json.Marshal(runtime)
	fx.mock.files[crewRunModeOwnerRoot+"/workflow.json"] = string(raw)
	fx.mock.mu.Unlock()
	return botDryRunWorld{crewRunModeFixture: fx, slack: svc}
}

func (w botDryRunWorld) createApp(t *testing.T, name, workspacePath, profileID string) services.SlackConnection {
	t.Helper()
	conn, err := w.slack.CreateSlackConnection(context.Background(), services.SlackConnectionInput{
		DisplayName: name, BotToken: "xoxb-dryrun-" + strings.ToLower(name), AppToken: "xapp-dryrun-" + strings.ToLower(name),
		Enabled: true, WorkspacePath: workspacePath, ProfileID: profileID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func (w botDryRunWorld) mention(t *testing.T, connectionID, channelID string) services.BotDryRunOutcome {
	t.Helper()
	outcome, err := w.api.dryRunSlackMention(context.Background(), connectionID, channelID, dryRunOwnerEmail, "check again")
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func requireAdmitted(t *testing.T, outcome services.BotDryRunOutcome, destination string) {
	t.Helper()
	if !outcome.Admitted {
		t.Fatalf("mention was refused: %q replies=%q", outcome.Reason, outcome.Replies)
	}
	if !strings.Contains(outcome.Destination, destination) {
		t.Fatalf("destination = %q, want it to name %q", outcome.Destination, destination)
	}
}

// A crew's own Slack app answers for the crew in any channel.
func TestBotDryRunCrewOwnApp(t *testing.T) {
	w := newBotDryRunWorld(t)
	app := w.createApp(t, "SDE", crewRunModeOwnerRoot, "work")
	requireAdmitted(t, w.mention(t, app.ID, "C0CREWCHAN1"), "crew-aaa")
}

// RTS 2026-09-26: a crew app saved with the logical path answered "This
// Slack route is no longer configured". The startup migration repairs it.
func TestBotDryRunLegacyLogicalCrewAppAfterMigration(t *testing.T) {
	w := newBotDryRunWorld(t)
	app := w.createApp(t, "SDE", crewRunModeOwnerRoot, "work")
	w.mock.mu.Lock()
	w.mock.files["config/slack-config.json"] = strings.ReplaceAll(w.mock.files["config/slack-config.json"], crewRunModeOwnerRoot, "Chats/Work/projects/alpha")
	w.mock.mu.Unlock()
	if err := w.slack.ReloadConfig(context.Background()); err != nil {
		t.Fatal(err)
	}

	w.api.migrateCrewBotScopes(context.Background(), w.slack)

	requireAdmitted(t, w.mention(t, app.ID, "C0CREWCHAN1"), "crew-aaa")
}

// The shared bot answers for a crew in a channel its owner routed from the
// crew's Slack tab (which sends the crew's logical path).
func TestBotDryRunSharedBotCrewChannel(t *testing.T) {
	w := newBotDryRunWorld(t)
	shared := w.createApp(t, "Shared", "", "")
	if err := w.slack.SetDefaultSlackConnection(context.Background(), shared.ID); err != nil {
		t.Fatal(err)
	}
	// Save the route the way the crew's Slack tab does: read the config,
	// add the channel with the crew's logical path, post it back.
	owner := &UserClaims{UserID: "owner", Username: "aman", Email: dryRunOwnerEmail}
	get := httptest.NewRequest(http.MethodGet, "/api/human-feedback/slack/config", nil)
	get = get.WithContext(context.WithValue(get.Context(), UserContextKey, owner))
	got := httptest.NewRecorder()
	getSlackConfigHandler(w.api)(got, get)
	var config map[string]any
	if got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &config) != nil {
		t.Fatalf("reading the Slack config = %d %s", got.Code, got.Body.String())
	}
	config["channel_routing"] = map[string]any{"C0SHARED01": map[string]any{"profile_id": "work", "conversation_key": "crew-aaa", "workspace_path": "Chats/Work/projects/alpha", "bot_grant": "run"}}
	body, _ := json.Marshal(config)
	req := httptest.NewRequest(http.MethodPost, "/api/human-feedback/slack/config", strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), UserContextKey, owner))
	rec := httptest.NewRecorder()
	updateSlackConfigHandler(w.api)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("saving the crew channel route = %d %s", rec.Code, rec.Body.String())
	}
	requireAdmitted(t, w.mention(t, shared.ID, "C0SHARED01"), "crew-aaa")
}

// A workflow's own Slack app answers for its workflow.
func TestBotDryRunWorkflowOwnApp(t *testing.T) {
	w := newBotDryRunWorld(t)
	app := w.createApp(t, "Shared-WF", "Workflow/shared", "")
	requireAdmitted(t, w.mention(t, app.ID, "C0WORKFLOW1"), "workflow")
}

// The shared bot in a channel nobody routed does not start a turn.
func TestBotDryRunSharedBotUnroutedChannelIsRefused(t *testing.T) {
	w := newBotDryRunWorld(t)
	shared := w.createApp(t, "Shared", "", "")
	if err := w.slack.SetDefaultSlackConnection(context.Background(), shared.ID); err != nil {
		t.Fatal(err)
	}
	if outcome := w.mention(t, shared.ID, "C0NOROUTE01"); outcome.Admitted {
		t.Fatalf("unrouted channel started a turn: %+v", outcome)
	}
}

// RTS 2026-09-26: gptlive1 attaches two other crews by their logical path and
// a workflow; its Slack bot was refused ("One or more attached workflows or
// Crew projects are unavailable"). Attachments are the owner's context.
func TestBotDryRunCrewWithAttachedCrewsAndWorkflow(t *testing.T) {
	w := newBotDryRunWorld(t)
	w.mock.mu.Lock()
	w.mock.files["_users/owner/Chats/Work/projects/gamma/product.json"] = `{"schema_version":1,"product":"work","id":"crew-ggg","title":"Gamma","created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-01T00:00:00Z"}`
	var runtime map[string]any
	if err := json.Unmarshal([]byte(w.mock.files[crewRunModeOwnerRoot+"/workflow.json"]), &runtime); err != nil {
		w.mock.mu.Unlock()
		t.Fatal(err)
	}
	runtime["workflow_context_paths"] = []string{"Chats/Work/projects/gamma", "Workflow/shared"}
	raw, _ := json.Marshal(runtime)
	w.mock.files[crewRunModeOwnerRoot+"/workflow.json"] = string(raw)
	w.mock.mu.Unlock()
	app := w.createApp(t, "SDE", crewRunModeOwnerRoot, "work")
	outcome := w.mention(t, app.ID, "C0CREWCHAN1")
	requireAdmitted(t, outcome, "crew-aaa")
	if paths, _ := json.Marshal(outcome.Request["workflow_context_paths"]); !strings.Contains(string(paths), "gamma") {
		t.Fatalf("the crew's attachments never reached the turn: %s", paths)
	}
}

// Each Slack thread is its own chat in the crew, never the crew's main
// conversation (RTS 2026-09-26: every thread landed in the owner's web chat).
func TestBotDryRunCrewSlackThreadsGetTheirOwnChats(t *testing.T) {
	w := newBotDryRunWorld(t)
	app := w.createApp(t, "SDE", crewRunModeOwnerRoot, "work")
	first, second := w.mention(t, app.ID, "C0CREWCHAN1"), w.mention(t, app.ID, "C0CREWCHAN1")
	requireAdmitted(t, first, "crew-aaa")
	requireAdmitted(t, second, "crew-aaa")
	firstKey, _ := first.Request["agent_profile_conversation_key"].(string)
	secondKey, _ := second.Request["agent_profile_conversation_key"].(string)
	if !strings.HasPrefix(firstKey, "crew-aaa:slack-") || !strings.HasPrefix(secondKey, "crew-aaa:slack-") {
		t.Fatalf("Slack threads ran in the crew's main conversation: %q %q", firstKey, secondKey)
	}
	if firstKey == secondKey {
		t.Fatalf("two Slack threads share one chat: %q", firstKey)
	}
}
