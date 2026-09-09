package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentworksclient"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestCLIPlanAndFileArguments(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		input  string
		tool   string
		fields map[string]any
	}{
		{[]string{"plan", "update-scripted-step", "--workflow", "wf-1", "--expected-revision", "rev", "--title", "New title", "--input", "-"}, `{"existing_step_id":"step-1","reason":"clarify","enabled":false}`, "update_scripted_step", map[string]any{"workflow_id": "wf-1", "expected_revision": "rev", "title": "New title", "existing_step_id": "step-1", "reason": "clarify", "enabled": false}},
		{[]string{"files", "write", "--workflow", "wf-1", "--path", "notes.md", "--content-file", "-", "--expected-revision", "missing"}, "# Notes\nKeep this newline.\n", "write_file", map[string]any{"workflow_id": "wf-1", "path": "notes.md", "content": "# Notes\nKeep this newline.\n", "expected_revision": "missing"}},
		{[]string{"builder", "status", "--workflow", "wf-1", "--session", "s-1", "--since-index", "-1", "--limit", "10"}, "", "builder_status", map[string]any{"workflow_id": "wf-1", "session_id": "s-1", "since_index": float64(-1), "limit": float64(10)}},
		{[]string{"builder", "reply", "--workflow", "wf-1", "--session", "s-1", "--request-id", "input-1", "--response", "Use the existing invoice schema."}, "", "builder_reply_input", map[string]any{"workflow_id": "wf-1", "session_id": "s-1", "request_id": "input-1", "response": "Use the existing invoice schema."}},
		{[]string{"tools", "call", "native_future_tool", "--set", `nested={"enabled":true}`, "--set", `count=9007199254740993`}, "", "native_future_tool", map[string]any{"nested": map[string]any{"enabled": true}}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			var called bool
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.URL.Path != "/api/external/v1/call" || r.Header.Get("Authorization") != "Bearer jwt" {
					t.Errorf("request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
				}
				var body struct {
					Name      string
					Arguments map[string]any
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Name != tc.tool {
					t.Errorf("name=%s", body.Name)
				}
				for k, v := range tc.fields {
					actual, _ := json.Marshal(body.Arguments[k])
					expected, _ := json.Marshal(v)
					if string(actual) != string(expected) {
						t.Errorf("%s: got %s want %s", k, actual, expected)
					}
				}
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer s.Close()
			var stdout, stderr bytes.Buffer
			args := append([]string{"--config", filepath.Join(t.TempDir(), "config.json"), "--server", s.URL, "--json"}, tc.args...)
			code := run(context.Background(), args, strings.NewReader(tc.input), &stdout, &stderr, func(key string) string {
				if key == "AGENTWORKS_TOKEN" {
					return "jwt"
				}
				return ""
			})
			if code != 0 || !called || stdout.String() != "{\"ok\":true}\n" || stderr.Len() != 0 {
				t.Fatalf("code=%d called=%v out=%s err=%s", code, called, &stdout, &stderr)
			}
		})
	}
}

func TestInvalidInputDoesNotCallServer(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{} {}`, `{"x":`} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"--config", filepath.Join(t.TempDir(), "missing"), "tools", "call", "get_plan", "--input", "-"}, strings.NewReader(raw), &stdout, &stderr, func(string) string { return "" })
		if code == 0 || !strings.Contains(stderr.String(), "--input") {
			t.Fatalf("input %q: code %d %s", raw, code, &stderr)
		}
	}
}

func TestLoginLogoutAndServerCredentialIsolation(t *testing.T) {
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer aw_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Error("wrong login token")
		}
		_, _ = w.Write([]byte(`{"tools":[]}`))
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "config.json")
	var stdout, stderr bytes.Buffer
	env := func(string) string { return "" }
	code := run(context.Background(), []string{"--config", path, "--server", s.URL, "login", "--token-stdin"}, strings.NewReader("aw_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"), &stdout, &stderr, env)
	if code != 0 {
		t.Fatalf("login: %s", &stderr)
	}
	if strings.Contains(stdout.String(), "aw_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatal("token exposed in output")
	}
	cfg, err := agentworksclient.LoadConfig(path)
	if err != nil || cfg.Token != "aw_pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("saved: %#v %v", cfg, err)
	}
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"--config", path, "--server", target.URL, "tools", "list"}, strings.NewReader(""), &stdout, &stderr, env)
	if code == 0 || targetCalls.Load() != 0 {
		t.Fatal("saved token sent to server override")
	}
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"--config", path, "logout"}, strings.NewReader(""), &stdout, &stderr, env)
	if code != 0 {
		t.Fatalf("logout: %s", &stderr)
	}
	cfg, err = agentworksclient.LoadConfig(path)
	if err != nil || cfg.Token != "" {
		t.Fatalf("logout retained token: %#v %v", cfg, err)
	}
}

func TestStructuredConflictExit(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"code":"revision_conflict","message":"stale revision"}}`))
	}))
	defer s.Close()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--config", filepath.Join(t.TempDir(), "missing"), "--server", s.URL, "--json", "plan", "get", "--workflow", "wf"}, strings.NewReader(""), &stdout, &stderr, func(key string) string {
		if key == "AGENTWORKS_TOKEN" {
			return "jwt"
		}
		return ""
	})
	if code != 4 || stdout.Len() != 0 || !json.Valid(stderr.Bytes()) || !strings.Contains(stderr.String(), "revision_conflict") {
		t.Fatalf("code=%d out=%s err=%s", code, &stdout, &stderr)
	}
}

func TestArgumentPrecisionAndInputOverride(t *testing.T) {
	cmd := newCommand(&options{stdin: strings.NewReader(""), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, getenv: os.Getenv})
	plan, _, err := cmd.Find([]string{"plan"})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.ParseFlags([]string{"--input", "-", "--workflow", "new", "--set", "count=9007199254740993"}); err != nil {
		t.Fatal(err)
	}
	args, err := operationArguments(plan, strings.NewReader(`{"workflow_id":"old","number":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(args)
	if args["workflow_id"] != "new" || !strings.Contains(string(data), `"count":9007199254740993`) || !strings.Contains(string(data), `"number":9007199254740993`) {
		t.Fatalf("arguments %s", data)
	}
}

// Exercise actual stdio framing from a client, including discovery and a call
// forwarded to the hosted HTTP API. Any non-protocol stdout breaks this test.
func TestMCPStdioRoundtrip(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt" {
			t.Error("missing hosted token")
		}
		if r.URL.Path == "/api/external/v1/tools" {
			_, _ = w.Write([]byte(`{"tools":[{"name":"get_plan","description":"Plan","inputSchema":{"type":"object","properties":{"workflow_id":{"type":"string"}},"required":["workflow_id"]}}]}`))
			return
		}
		var body struct {
			Name      string
			Arguments map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Name != "get_plan" || body.Arguments["workflow_id"] != "wf" {
			t.Errorf("call: %#v", body)
		}
		_, _ = w.Write([]byte(`{"revision":"v1","plan":{"steps":[]}}`))
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	defer serverIn.Close()
	defer clientOut.Close()
	defer clientIn.Close()
	defer serverOut.Close()
	var stderr bytes.Buffer
	done := make(chan int, 1)
	configPath := filepath.Join(t.TempDir(), "missing")
	go func() {
		done <- run(ctx, []string{"--config", configPath, "--server", s.URL, "mcp", "serve"}, serverIn, serverOut, &stderr, func(key string) string {
			if key == "AGENTWORKS_TOKEN" {
				return "jwt"
			}
			return ""
		})
	}()
	client := mcpclient.NewClient(transport.NewIO(clientIn, clientOut, nil))
	defer client.Close()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}
	if _, err := client.Initialize(ctx, request); err != nil {
		t.Fatal(err)
	}
	listed, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "get_plan" {
		t.Fatalf("list: %#v %v", listed, err)
	}
	result, err := client.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "get_plan", Arguments: map[string]any{"workflow_id": "wf"}}})
	if err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("call: %#v %v", result, err)
	}
	content, ok := result.Content[0].(mcp.TextContent)
	if !ok || !strings.Contains(content.Text, `"revision":"v1"`) {
		t.Fatalf("result content: %#v", result.Content)
	}
	_ = clientOut.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("bridge exited %d: %s", code, &stderr)
		}
	case <-ctx.Done():
		t.Fatal("bridge failed to stop at EOF")
	}
}

func TestCLILoginRejectsPasswordAndSessionJWT(t *testing.T) {
	for _, args := range [][]string{{"login", "--username", "alice", "--password-stdin"}, {"login", "--token-stdin"}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), append([]string{"--config", filepath.Join(t.TempDir(), "config"), "--server", "http://127.0.0.1:1"}, args...), strings.NewReader("session.jwt.token"), &out, &stderr, func(string) string { return "" })
		if code == 0 {
			t.Fatal("legacy CLI login accepted")
		}
	}
}
