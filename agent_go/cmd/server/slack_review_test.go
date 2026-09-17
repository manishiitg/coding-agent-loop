package server

import (
	"context"
	"encoding/json"
	"fmt"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

func TestSlackRouteOwnerCannotBeRewrittenByPayload(t *testing.T) {
	route := ChannelRoute{ProfileID: "work", ConversationKey: "acme", WorkspacePath: "Chats/Work/projects/acme", WorkspaceUserID: "alice", BotGrant: "run"}
	current := map[string]ChannelRoute{"C123": route}
	route.WorkspaceUserID = "mallory"
	next := map[string]ChannelRoute{"C123": route}
	if err := validateSlackRouteMutationPermissions(context.Background(), nil, next, current); err != nil {
		t.Fatal(err)
	}
	if next["C123"].WorkspaceUserID != "alice" {
		t.Fatal("client replaced the resource owner")
	}
}
func TestSlackRouteRejectsAmbiguousAndMalformedDestinations(t *testing.T) {
	for _, route := range []ChannelRoute{
		{WorkflowID: "w"},
		{WorkflowID: "w", ProfileID: "work", ConversationKey: "acme", WorkspacePath: "Workflow/w"},
		{ProfileID: "work", WorkspacePath: "Chats/Work"},
		{WorkflowID: "w", WorkspacePath: "Workflow/w", BotGrant: "administrator"},
	} {
		if _, err := normalizeSlackChannelRouting(map[string]ChannelRoute{"C123": route}); err == nil {
			t.Fatalf("accepted %+v", route)
		}
	}
}
func TestSlackBotPrincipalCannotManageItsOwnGrantOrBecomeAccountAdmin(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "false")
	claims := botRouteUserClaims("bot", services.ChannelRoute{WorkflowID: "demo", WorkspacePath: "Workflow/demo", BotGrant: "owner"})
	if access := userAccessForClaims(claims); access.Admin || access.CanCreate || access.CanEdit {
		t.Fatalf("bot inherited account authority: %+v", access)
	}
	ctx := context.WithValue(context.Background(), UserContextKey, claims)
	if _, err := requireSlackRouteDestinationOwner(ctx, nil, ChannelRoute{WorkflowID: "demo"}); err == nil {
		t.Fatal("bot can manage grants")
	}
}
func TestSlackToolsFollowSessionOriginAndReadOnlyPolicy(t *testing.T) {
	for _, tc := range []struct {
		name               string
		req                QueryRequest
		readOnly, mutating bool
	}{
		{name: "owner", mutating: true},
		{name: "reader", readOnly: true},
		{name: "slack owner", req: QueryRequest{BotPlatform: "slack"}},
		{name: "schedule", req: QueryRequest{TriggeredBy: "cron"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := &recordingRegistrar{}
			api := &StreamingAPI{}
			if err := api.registerWorkflowUIForCaller(reg, "workflow-builder", "chat", "Workflow/demo", tc.req, tc.readOnly); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"create_slack_bot_route", "update_slack_bot_route_permission", "remove_slack_bot_route"} {
				_, present := reg.tools[name]
				if present != tc.mutating {
					t.Fatalf("%s present=%v", name, present)
				}
			}
			if _, present := reg.tools["get_slack_bot_settings"]; !present {
				t.Fatal("missing inspection")
			}
		})
	}
}
func TestSlackImmutableAllocationIsConcurrentAndIdempotent(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	manual := filepath.Join(docs, "Workflow/demo/runs/iteration-0")
	if err := os.MkdirAll(manual, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manual, "artifact"), []byte("builder"), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	folders := make([]string, 12)
	for i := range folders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var err error
			folders[i], err = allocateSlackRunFolder("Workflow/demo", string(rune('a'+i/2)))
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for i := 0; i < len(folders); i += 2 {
		if folders[i] != folders[i+1] || !strings.Contains(folders[i], "-slack-") || seen[folders[i]] {
			t.Fatalf("invalid allocation: %v", folders)
		}
		seen[folders[i]] = true
	}
	if raw, err := os.ReadFile(filepath.Join(manual, "artifact")); err != nil || string(raw) != "builder" {
		t.Fatal("Builder artifact modified")
	}
	scheduled, err := allocateScheduledRunFolder("Workflow/demo", "scheduled")
	if err != nil || scheduled != "iteration-7-sched" {
		t.Fatalf("namespace drift: %s %v", scheduled, err)
	}
}

func TestSlackSendCrossStepReferencesIdempotencyAndRevocation(t *testing.T) {
	server, _ := newFakeWorkspaceServer(t)
	t.Setenv("WORKSPACE_API_URL", server.URL)
	store, err := chathistory.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	route := ChannelRoute{WorkflowID: "demo", WorkspacePath: "Workflow/demo", BotGrant: "run"}
	cfg := &chathistory.CreateBotConnectorConfigRequest{ID: "slack", Enabled: true, BotMode: true, AllowedChannels: `{"C123":{"workflow_id":"demo","workspace_path":"Workflow/demo","bot_grant":"run"}}`}
	if _, err := store.UpsertBotConnectorConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	claims := botRouteUserClaims("bot", route)
	claims.ExecutionPrincipal = &ExecutionPrincipal{Kind: "bot_route", Target: route, ID: "bot", Access: WorkflowAccessRead}
	api := &StreamingAPI{chatStore: store}
	req := QueryRequest{PresetQueryID: "demo", SelectedFolder: "Workflow/demo", BotPlatform: "slack", BotChannelID: "C123", BotThreadTS: "source.1"}
	api.botExecutionSessions.Store("root", botExecutionSession{Claims: claims, Request: req})
	virtualtools.RegisterParentChat("script-1", &virtualtools.ParentChatContext{SessionID: "root"})
	virtualtools.RegisterParentChat("script-2", &virtualtools.ParentChatContext{SessionID: "root"})
	defer virtualtools.UnregisterParentChat("script-1")
	defer virtualtools.UnregisterParentChat("script-2")
	posts := 0
	api.postSlackMessage = func(_ context.Context, channel, thread, message string) (string, error) {
		posts++
		if channel != "C123" || thread != "source.1" {
			t.Fatalf("wrong reply destination: %s %s", channel, thread)
		}
		return fmt.Sprintf("reply.%d", posts), nil
	}
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, "script-1")
	args := map[string]interface{}{"route_id": "C123", "message": "progress", "idempotency_key": "step-1"}
	raw, err := api.sendSlackMessageFromTool(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	var sent slackSendRecord
	if err := json.Unmarshal([]byte(raw), &sent); err != nil {
		t.Fatal(err)
	}
	again, err := api.sendSlackMessageFromTool(ctx, args)
	if err != nil || again != raw || posts != 1 {
		t.Fatalf("duplicate delivery: %s %v posts=%d", again, err, posts)
	}
	ctx = context.WithValue(context.Background(), common.ChatSessionIDKey, "script-2")
	args["thread_ref"] = sent.ThreadRef
	args["message"] = "RCA"
	args["idempotency_key"] = "step-2"
	if _, err := api.sendSlackMessageFromTool(ctx, args); err != nil {
		t.Fatal(err)
	}
	args["message"] = "different"
	if _, err := api.sendSlackMessageFromTool(ctx, args); err == nil {
		t.Fatal("accepted idempotency conflict")
	}
	foreign, err := saveSlackThreadReference(ctx, "COTHER", route, "foreign.1")
	if err != nil {
		t.Fatal(err)
	}
	args["thread_ref"] = foreign
	args["idempotency_key"] = "foreign"
	if _, err := api.sendSlackMessageFromTool(ctx, args); err == nil {
		t.Fatal("cross-route reference accepted")
	}
	args["thread_ref"] = sent.ThreadRef
	cfg.AllowedChannels = "{}"
	if _, err := store.UpsertBotConnectorConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	args["idempotency_key"] = "revoked"
	if _, err := api.sendSlackMessageFromTool(ctx, args); err == nil {
		t.Fatal("revoked route sent")
	}
	if posts != 2 {
		t.Fatalf("posts=%d", posts)
	}
}

func TestSlackPrincipalRevalidationSeparatesResourceOwnerAndAuditActor(t *testing.T) {
	store, err := chathistory.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{chatStore: store}
	route := ChannelRoute{ProfileID: "work", ConversationKey: "acme", WorkspacePath: "Chats/Work/projects/acme", WorkspaceUserID: "alice", BotGrant: "owner"}
	save := func(grant string) {
		route.BotGrant = grant
		encoded, _ := json.Marshal(map[string]ChannelRoute{"C123": route})
		if _, err := store.UpsertBotConnectorConfig(context.Background(), &chathistory.CreateBotConnectorConfigRequest{ID: "slack", Enabled: true, BotMode: true, AllowedChannels: string(encoded)}); err != nil {
			t.Fatal(err)
		}
	}
	save("owner")
	claims := botRouteUserClaims("synthetic", route)
	ctx := context.WithValue(context.Background(), UserContextKey, claims)
	req := QueryRequest{AgentProfileID: "work", AgentProfileConversationKey: "acme", SelectedFolder: route.WorkspacePath, BotPlatform: "slack", BotChannelID: "C123", BotUserID: "sender-a"}
	first, err := api.revalidateExecutionPrincipal(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	principal := GetUserFromContext(first)
	if principal.UserID != "alice" || principal.ExecutionPrincipal.ID == "alice" || principal.ExecutionPrincipal.Access != WorkflowAccessOwner {
		t.Fatalf("principal=%+v", principal)
	}
	req.BotUserID = "sender-b"
	second, err := api.revalidateExecutionPrincipal(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if GetUserFromContext(second).ExecutionPrincipal.ID != principal.ExecutionPrincipal.ID || GetUserFromContext(second).ExecutionPrincipal.AuditActor != "sender-b" {
		t.Fatal("sender changed authority")
	}
	save("run")
	third, err := api.revalidateExecutionPrincipal(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if GetUserFromContext(third).ExecutionPrincipal.Access != WorkflowAccessRead {
		t.Fatal("downgrade ignored")
	}
	req.AgentProfileConversationKey = "other"
	if _, err := api.revalidateExecutionPrincipal(ctx, req); err == nil {
		t.Fatal("cross-project request admitted")
	}
}
func TestSlackDistinctFullRunsAndRestartBinding(t *testing.T) {
	server, _ := newFakeWorkspaceServer(t)
	t.Setenv("WORKSPACE_API_URL", server.URL)
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	if err := os.MkdirAll(filepath.Join(docs, "Workflow/demo"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := chathistory.NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	route := ChannelRoute{WorkflowID: "demo", WorkspacePath: "Workflow/demo", BotGrant: "run"}
	encoded, _ := json.Marshal(map[string]ChannelRoute{"C123": route})
	if _, err := store.UpsertBotConnectorConfig(context.Background(), &chathistory.CreateBotConnectorConfigRequest{ID: "slack", Enabled: true, BotMode: true, AllowedChannels: string(encoded)}); err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{chatStore: store}
	req := QueryRequest{PresetQueryID: "demo", SelectedFolder: route.WorkspacePath, BotPlatform: "slack", BotChannelID: "C123", BotThreadTS: "root.1", ExecutionOptions: &ExecutionOptions{SelectedRunFolder: "iteration-0"}}
	ctx := context.WithValue(context.Background(), UserContextKey, botRouteUserClaims("bot", route))
	ctx, err = api.revalidateExecutionPrincipal(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.bindSlackInvocation(ctx, &req, "conversation"); err != nil {
		t.Fatal(err)
	}
	initial := req.ExecutionOptions.SelectedRunFolder
	value, _ := api.scheduleInvocations.Load("conversation")
	binding := value.(*stepworkflow.ExternalInvocation)
	first, err := binding.Next(ctx, "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := binding.Next(ctx, "execution-1")
	if err != nil || repeat.RunFolder != first.RunFolder {
		t.Fatalf("retry changed folder: %+v %v", repeat, err)
	}
	second, err := binding.Next(ctx, "execution-2")
	if err != nil {
		t.Fatal(err)
	}
	if first.RunFolder != initial || first.RunFolder == second.RunFolder || binding.BoundRunFolder() != second.RunFolder {
		t.Fatal("full runs shared a mutable folder")
	}
	restarted := &StreamingAPI{chatStore: store}
	if err := restarted.bindSlackInvocation(ctx, &req, "conversation"); err != nil {
		t.Fatal(err)
	}
	if req.ExecutionOptions.SelectedRunFolder != second.RunFolder {
		t.Fatal("restart lost active invocation")
	}
}

func TestSlackBindingSurvivesConnectorRestartAndRejectsRepointing(t *testing.T) {
	server, _ := newFakeWorkspaceServer(t)
	t.Setenv("WORKSPACE_API_URL", server.URL)
	thread := services.ThreadID{Platform: "slack", ChannelID: "C123", ThreadTS: "1.2"}
	saved := services.BotSessionBinding{SessionID: "durable-session", RouteKey: "project-acme"}
	if err := (&services.SlackService{}).SaveBotSessionBinding(context.Background(), thread, saved); err != nil {
		t.Fatal(err)
	}
	restarted := &services.SlackService{}
	got, ok, err := restarted.LoadBotSessionBinding(context.Background(), thread, saved.RouteKey)
	if err != nil || !ok || got.SessionID != saved.SessionID {
		t.Fatalf("restore: %+v %v %v", got, ok, err)
	}
	if _, ok, err := restarted.LoadBotSessionBinding(context.Background(), thread, "other-project"); err != nil || ok {
		t.Fatalf("repointed route restored: %v %v", ok, err)
	}
	if err := restarted.ClearBotSessionBinding(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := restarted.LoadBotSessionBinding(context.Background(), thread, saved.RouteKey); ok {
		t.Fatal("cleared binding restored")
	}
}

func TestSlackRetentionPreservesActiveUnknownAndOtherRunFamilies(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	workspace := "Workflow/demo"
	write := func(path, value string) {
		t.Helper()
		p := filepath.Join(docs, workspace, path)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("workflow.json", `{"run_retention_count":1}`)
	for _, name := range []string{"iteration-1-slack-a1", "iteration-2-slack-a2", "iteration-3-slack-a3", "iteration-4-slack-a4"} {
		write("runs/"+name+"/.slack-run-id", name)
		write("evaluation/runs/"+name+"/artifact", "evaluation")
	}
	write("runs/iteration-1-slack-a1/group/run_metadata.json", `{"status":"completed","completed_at":"2026-09-01T00:00:00Z"}`)
	write("runs/iteration-2-slack-a2/group/run_metadata.json", `{"status":"completed","completed_at":"2026-09-02T00:00:00Z"}`)
	write("runs/iteration-3-slack-a3/group/run_metadata.json", `{"status":"completed","completed_at":"2026-08-01T00:00:00Z"}`)
	write("runs/iteration-0/artifact", "builder")
	write("runs/iteration-5-sched/artifact", "schedule")
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{"active": {Status: "running"}}}
	api.scheduleInvocations.Store("active", &stepworkflow.ExternalInvocation{Kind: "slack", RunFolder: "iteration-3-slack-a3"})
	if err := api.pruneSlackRuns(workspace); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"runs/iteration-1-slack-a1", "evaluation/runs/iteration-1-slack-a1"} {
		if _, err := os.Stat(filepath.Join(docs, workspace, path)); !os.IsNotExist(err) {
			t.Fatalf("expired path remains: %s %v", path, err)
		}
	}
	for _, path := range []string{"runs/iteration-0/artifact", "runs/iteration-5-sched/artifact", "runs/iteration-2-slack-a2", "runs/iteration-3-slack-a3", "runs/iteration-4-slack-a4"} {
		if _, err := os.Stat(filepath.Join(docs, workspace, path)); err != nil {
			t.Fatalf("protected path lost: %s %v", path, err)
		}
	}
}

func TestSlackConversationCannotChangeReplyThreadDuringExecution(t *testing.T) {
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{"shared": {Status: "running"}}}
	claims := &UserClaims{ExecutionPrincipal: &ExecutionPrincipal{Kind: "bot_route"}}
	ctx := context.WithValue(context.Background(), UserContextKey, claims)
	req := QueryRequest{BotPlatform: "slack", BotChannelID: "C123", BotThreadTS: "first"}
	if err := api.bindSlackInvocation(ctx, &req, "shared"); err != nil {
		t.Fatal(err)
	}
	req.BotThreadTS = "other"
	if err := api.bindSlackInvocation(ctx, &req, "shared"); err == nil {
		t.Fatal("replaced active source thread")
	}
	bound, _ := api.botExecutionForSession("shared")
	if bound.Request.BotThreadTS != "first" {
		t.Fatal("failed request changed reply destination")
	}
}

func TestSlackProfileTurnUsesWebConversationWithoutWhatsApp(t *testing.T) {
	server, _ := newFakeWorkspaceServer(t)
	t.Setenv("WORKSPACE_API_URL", server.URL)
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("MULTI_USER_MODE", "true")
	profile := singletonConversationProfile()
	registry := agentprofiles.NewRegistry()
	if err := registry.RegisterProfile(profile); err != nil {
		t.Fatal(err)
	}
	api := &StreamingAPI{agentProfiles: registry}
	webRequest := profileRouteRequest("POST", "/", nil, "alice")
	conversation, err := api.resolveAgentProfileConversation(webRequest, profile, "main")
	if err != nil {
		t.Fatal(err)
	}
	web, err := prepareProductConversationTurn(webRequest.Context(), "alice", profile, AgentProfileChatRequest{Message: "hello"}, conversation)
	if err != nil {
		t.Fatal(err)
	}
	bot, sid, handled, err := api.botProfileTurn(context.Background(), "bot-route", services.BotIncomingMessage{Platform: "slack", UserID: "external-sender", Text: "hello", PresetProfile: &services.ProfileRoute{ProfileID: profile.ID, ConversationKey: "main", WorkspaceUserID: "alice"}}, services.ThreadID{Platform: "slack", ChannelID: "C123", ThreadTS: "1.2"})
	if err != nil || !handled || sid != conversation.SessionID {
		t.Fatalf("Slack binding: %s %v %v", sid, handled, err)
	}
	delete(bot, "bot_platform")
	delete(bot, "bot_channel_id")
	delete(bot, "triggered_by")
	delete(bot, "_trusted_resume_target")
	expected, err := queryRequestToMap(web)
	if err != nil {
		t.Fatal(err)
	}
	actualJSON, _ := json.Marshal(bot)
	expectedJSON, _ := json.Marshal(expected)
	if string(actualJSON) != string(expectedJSON) {
		t.Fatalf("web/Slack request drift:\nweb=%s\nbot=%s", expectedJSON, actualJSON)
	}
}

func TestSlackAllocatorAcrossProcesses(t *testing.T) {
	if os.Getenv("PR198_ALLOCATOR_CHILD") == "1" {
		folder, err := allocateSlackRunFolder("Workflow/concurrent", "same-delivery")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Print(folder)
		os.Exit(0)
	}
	docs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(docs, "Workflow/concurrent"), 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	results := make([]string, 4)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(executable, "-test.run=^TestSlackAllocatorAcrossProcesses$")
			cmd.Env = append(os.Environ(), "PR198_ALLOCATOR_CHILD=1", "WORKSPACE_DOCS_PATH="+docs)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("allocator: %s %v", out, err)
			}
			results[i] = string(out)
		}(i)
	}
	wg.Wait()
	for _, result := range results {
		if result != results[0] || !strings.HasPrefix(result, "iteration-1-slack-") {
			t.Fatalf("retry allocated twice: %q", results)
		}
	}
}
