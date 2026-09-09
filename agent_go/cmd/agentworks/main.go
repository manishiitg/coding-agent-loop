// AgentWorks is a hosted API CLI and a stdio MCP bridge.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentworksclient"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

type options struct {
	serverURL, configPath string
	jsonOutput            bool
	stdin                 io.Reader
	stdout, stderr        io.Writer
	getenv                func(string) string
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	o := &options{stdin: stdin, stdout: stdout, stderr: stderr, getenv: getenv}
	cmd := newCommand(o)
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
		code := 1
		var apiErr *agentworksclient.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.Status {
			case 401, 403:
				code = 3
			case 409:
				code = 4
			}
		}
		if o.jsonOutput {
			detail := map[string]any{"code": "client_error", "message": err.Error()}
			if apiErr != nil {
				detail = map[string]any{"code": apiErr.Code, "message": apiErr.Message, "status": apiErr.Status}
			}
			_ = json.NewEncoder(stderr).Encode(map[string]any{"error": detail})
		} else {
			fmt.Fprintln(stderr, "Error:", err)
		}
		return code
	}
	return 0
}

func newCommand(o *options) *cobra.Command {
	root := &cobra.Command{Use: "agentworks", Short: "Control a hosted AgentWorks server or expose its tools over MCP", SilenceUsage: true, SilenceErrors: true}
	root.SetIn(o.stdin)
	root.SetOut(o.stdout)
	root.SetErr(o.stderr)
	root.PersistentFlags().StringVar(&o.serverURL, "server", "", "Hosted AgentWorks HTTPS URL (or AGENTWORKS_SERVER)")
	root.PersistentFlags().StringVar(&o.configPath, "config", "", "Private connection config path")
	root.PersistentFlags().BoolVar(&o.jsonOutput, "json", false, "Emit compact JSON results and structured errors")
	root.AddCommand(loginCommand(o), logoutCommand(o))
	toolsCmd := &cobra.Command{Use: "tools", Short: "Discover and call the server's current tools"}
	toolsCmd.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List tools with authoritative JSON schemas", RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := o.client()
		if err != nil {
			return err
		}
		tools, err := client.Tools(cmd.Context())
		if err != nil {
			return err
		}
		return o.output(map[string]any{"tools": tools})
	}})
	call := &cobra.Command{Use: "call NAME", Args: cobra.ExactArgs(1), Short: "Call any server tool; --input accepts a JSON object file or stdin"}
	addOperationFlags(call, "")
	call.RunE = func(cmd *cobra.Command, args []string) error { return o.call(cmd, args[0]) }
	toolsCmd.AddCommand(call)
	root.AddCommand(toolsCmd)
	groups := []struct {
		name, description string
		operations        []struct{ command, tool string }
	}{
		{"workflows", "Discover workflows", []struct{ command, tool string }{{"list", "list_workflows"}, {"get", "get_workflow"}}},
		{"files", "Read and edit ordinary workspace files; plan files require plan tools", []struct{ command, tool string }{{"list", "list_files"}, {"read", "read_file"}, {"search", "search_files"}, {"write", "write_file"}, {"patch", "patch_file"}}},
		{"runs", "Inspect workflow activity", []struct{ command, tool string }{{"list", "list_runs"}, {"get", "get_run"}, {"logs", "get_logs"}}},
		{"builder", "Send messages to existing Workflow Builder and follow its session", []struct{ command, tool string }{{"chat", "builder_chat"}, {"status", "builder_status"}, {"reply", "builder_reply_input"}, {"cancel", "builder_cancel"}}},
	}
	for _, group := range groups {
		groupCmd := &cobra.Command{Use: group.name, Short: group.description}
		for _, op := range group.operations {
			toolName := op.tool
			cmd := &cobra.Command{Use: op.command, Args: cobra.NoArgs, Short: "Call " + toolName}
			addOperationFlags(cmd, toolName)
			cmd.RunE = func(cmd *cobra.Command, _ []string) error { return o.call(cmd, toolName) }
			groupCmd.AddCommand(cmd)
		}
		root.AddCommand(groupCmd)
	}
	plan := &cobra.Command{
		Use: "plan OPERATION", Args: cobra.ExactArgs(1),
		Short: "Get a plan or invoke its native tools (e.g. update-scripted-step)",
		Long:  "Read with 'plan get'. For mutations, use the native tool name with hyphens or underscores.\nDiscover available names and fields with 'tools list'. Pass native fields via --input.\nRead the revision with 'plan get', then supply --expected-revision to every mutation.\nExample: agentworks plan update-scripted-step --workflow ID --expected-revision REV --input change.json",
	}
	addOperationFlags(plan, "plan")
	plan.RunE = func(cmd *cobra.Command, args []string) error {
		name := strings.ReplaceAll(args[0], "-", "_")
		if name == "get" {
			name = "get_plan"
		}
		return o.call(cmd, name)
	}
	root.AddCommand(plan)
	mcpCmd := &cobra.Command{Use: "mcp", Short: "Expose this hosted connection as MCP"}
	mcpCmd.AddCommand(&cobra.Command{Use: "serve", Args: cobra.NoArgs, Short: "Serve MCP over stdio (stdout is reserved for protocol messages)", RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := o.client()
		if err != nil {
			return err
		}
		bridge, err := agentworksclient.NewMCPServer(cmd.Context(), client)
		if err != nil {
			return err
		}
		return server.NewStdioServer(bridge).Listen(cmd.Context(), o.stdin, o.stdout)
	}})
	root.AddCommand(mcpCmd)
	return root
}

func (o *options) path() (string, error) {
	if o.configPath != "" {
		return o.configPath, nil
	}
	return agentworksclient.DefaultConfigPath()
}

func (o *options) connection(forLogin bool) (agentworksclient.Config, string, error) {
	path, err := o.path()
	if err != nil {
		return agentworksclient.Config{}, "", err
	}
	cfg, err := agentworksclient.LoadConfig(path)
	if err != nil {
		return cfg, path, err
	}
	requested := o.serverURL
	if requested == "" {
		requested = o.getenv("AGENTWORKS_SERVER")
	}
	if requested == "" {
		requested = cfg.Server
	}
	if requested == "" {
		return cfg, path, errors.New("server is required: use --server or agentworks login")
	}
	normalized, err := agentworksclient.ValidateServer(requested)
	if err != nil {
		return cfg, path, err
	}
	saved, _ := agentworksclient.ValidateServer(cfg.Server)
	// A command targeting another host must never reuse saved credentials.
	if saved != normalized {
		cfg.Token = ""
	}
	cfg.Server = normalized
	if !forLogin {
		if token := o.getenv("AGENTWORKS_TOKEN"); token != "" {
			cfg.Token = token
		}
	}
	return cfg, path, nil
}

func (o *options) client() (*agentworksclient.Client, error) {
	cfg, _, err := o.connection(false)
	if err != nil {
		return nil, err
	}
	return agentworksclient.New(cfg.Server, cfg.Token)
}

func loginCommand(o *options) *cobra.Command {
	var tokenStdin bool
	cmd := &cobra.Command{Use: "login", Args: cobra.NoArgs, Short: "Connect using an access token generated in your AgentWorks account menu", Long: "Generate an access token in AgentWorks: account menu → Access tokens.\nThen run: agentworks login --server https://your-server --token-stdin\nSupply the token on stdin. Password login is only available in the app.", RunE: func(cmd *cobra.Command, _ []string) error {
		if !tokenStdin {
			return errors.New("generate an access token in the AgentWorks account menu, then use --token-stdin")
		}
		cfg, path, err := o.connection(true)
		if err != nil {
			return err
		}
		secret, err := io.ReadAll(io.LimitReader(o.stdin, 1025))
		if err != nil {
			return err
		}
		value := strings.TrimSpace(string(secret))
		if !strings.HasPrefix(value, "aw_pat_") || len(value) != 71 {
			return errors.New("expected an AgentWorks access token from account menu → Access tokens")
		}
		cfg.Token = value
		client, err := agentworksclient.New(cfg.Server, cfg.Token)
		if err != nil {
			return err
		}
		if _, err := client.Tools(cmd.Context()); err != nil {
			return fmt.Errorf("verify token access: %w", err)
		}
		if err := agentworksclient.SaveConfig(path, cfg); err != nil {
			return err
		}
		return o.output(map[string]any{"server": cfg.Server, "logged_in": true})
	}}
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read an app-generated personal access token from stdin")
	return cmd
}

func logoutCommand(o *options) *cobra.Command {
	return &cobra.Command{Use: "logout", Args: cobra.NoArgs, Short: "Remove saved credentials (does not revoke tokens or unset environment variables)", RunE: func(_ *cobra.Command, _ []string) error {
		path, err := o.path()
		if err != nil {
			return err
		}
		cfg, err := agentworksclient.LoadConfig(path)
		if err != nil {
			return err
		}
		if cfg.Server != "" {
			cfg.Token = ""
			if err := agentworksclient.SaveConfig(path, cfg); err != nil {
				return err
			}
		}
		return o.output(map[string]any{"logged_out": true, "environment_token_active": o.getenv("AGENTWORKS_TOKEN") != ""})
	}}
}

func addOperationFlags(cmd *cobra.Command, tool string) {
	f := cmd.Flags()
	f.String("input", "", "Tool arguments as a JSON object from a file, or - for stdin")
	if tool != "list_workflows" {
		f.String("workflow", "", "Workflow ID (workflow_id)")
	}
	if tool == "" || tool == "plan" || tool == "write_file" || tool == "patch_file" {
		f.String("expected-revision", "", "Revision from read_file/get_plan; use missing to create a file")
	}
	f.StringArray("set", nil, "Set a native argument as key=JSON; repeatable (quote string JSON values)")
	if strings.Contains(tool, "file") {
		f.String("path", "", "Workspace-relative file or directory path")
	}
	if tool == "list_workflows" || tool == "list_files" || tool == "search_files" || tool == "list_runs" || tool == "get_run" || tool == "get_logs" {
		f.Int("limit", 0, "Maximum results")
		f.Int("offset", 0, "Result offset")
	}
	if tool == "list_files" || tool == "search_files" {
		f.Int("depth", 0, "Directory traversal depth (1..8)")
	}
	if tool == "search_files" || tool == "list_workflows" {
		f.String("query", "", "Search query")
	}
	if tool == "write_file" {
		f.String("content-file", "", "Read new file content from a local file or - for stdin")
	}
	if tool == "patch_file" {
		f.String("diff-file", "", "Read unified diff from a local file or - for stdin")
	}
	if tool == "get_run" || tool == "get_logs" {
		f.String("run-folder", "", "Run folder from list_runs")
	}
	if strings.HasPrefix(tool, "builder_") {
		f.String("session", "", "Builder session ID (session_id)")
		if tool == "builder_chat" {
			f.String("message", "", "Message to Workflow Builder")
			f.String("provider", "", "Builder model provider")
			f.String("model", "", "Builder model ID")
		}
		if tool == "builder_reply_input" {
			f.String("request-id", "", "Pending input unique_id returned by builder status")
			f.String("response", "", "Response to the pending builder input request")
		}
		if tool == "builder_status" {
			f.Int("since-index", -1, "Return events after this index")
			f.Int("limit", 0, "Maximum events (1..200)")
		}
	}
	if tool == "plan" {
		f.String("step", "", "Existing step ID (existing_step_id)")
		f.String("title", "", "New step title")
		f.String("reason", "", "Reason for the plan mutation")
	}
}

func readInput(path string, stdin io.Reader) ([]byte, error) {
	var reader io.Reader = stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		reader = f
	}
	data, err := io.ReadAll(io.LimitReader(reader, agentworksclient.MaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > agentworksclient.MaxBodyBytes {
		return nil, errors.New("input exceeds 16 MiB limit")
	}
	return data, nil
}

func operationArguments(cmd *cobra.Command, stdin io.Reader) (map[string]any, error) {
	arguments := map[string]any{}
	input, _ := cmd.Flags().GetString("input")
	if input != "" {
		data, err := readInput(input, stdin)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&arguments); err != nil {
			return nil, fmt.Errorf("--input must contain a JSON object: %w", err)
		}
		if arguments == nil {
			return nil, errors.New("--input must contain a JSON object")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, errors.New("--input must contain exactly one JSON object")
		}
	}
	for flagName, field := range map[string]string{"workflow": "workflow_id", "expected-revision": "expected_revision", "path": "path", "query": "query", "run-folder": "run_folder", "session": "session_id", "message": "message", "provider": "provider", "model": "model_id", "step": "existing_step_id", "title": "title", "reason": "reason", "request-id": "request_id", "response": "response"} {
		if cmd.Flags().Changed(flagName) {
			value, _ := cmd.Flags().GetString(flagName)
			arguments[field] = value
		}
	}
	for flagName, field := range map[string]string{"limit": "limit", "offset": "offset", "depth": "depth", "since-index": "since_index"} {
		if cmd.Flags().Changed(flagName) {
			value, _ := cmd.Flags().GetInt(flagName)
			arguments[field] = value
		}
	}
	for flagName, field := range map[string]string{"content-file": "content", "diff-file": "diff"} {
		if cmd.Flags().Changed(flagName) {
			path, _ := cmd.Flags().GetString(flagName)
			if input == "-" && path == "-" {
				return nil, errors.New("stdin can only supply one input")
			}
			data, err := readInput(path, stdin)
			if err != nil {
				return nil, err
			}
			arguments[field] = string(data)
		}
	}
	sets, _ := cmd.Flags().GetStringArray("set")
	for _, entry := range sets {
		key, raw, ok := strings.Cut(entry, "=")
		if !ok || key == "" || !json.Valid([]byte(raw)) {
			return nil, errors.New("--set requires key=JSON, for example --set 'title=\"New title\"'")
		}
		var value any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		arguments[key] = value
	}
	return arguments, nil
}

func (o *options) call(cmd *cobra.Command, name string) error {
	arguments, err := operationArguments(cmd, o.stdin)
	if err != nil {
		return err
	}
	client, err := o.client()
	if err != nil {
		return err
	}
	result, err := client.Call(cmd.Context(), name, arguments)
	if err != nil {
		return err
	}
	return o.output(result)
}

func (o *options) output(value any) error {
	encoder := json.NewEncoder(o.stdout)
	if !o.jsonOutput {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}
