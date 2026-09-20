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
	"path/filepath"
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

// cliOperationGroups maps CLI subcommands to external tools. Every tool named
// here must stay admitted by product.yaml's run mode — external_tools or
// the proxied run.tools (see TestCLIOperationsStayAdmitted); `plan get` maps
// to get_plan the same way, while `tools call` and `mcp serve` resolve
// against the live server catalog instead. Structured run arguments
// (script_parameters, human_inputs, route_selections) travel via --set.
var cliOperationGroups = []struct {
	name, description string
	operations        []struct{ command, tool string }
}{
	{"workflows", "Discover workflows", []struct{ command, tool string }{{"list", "list_workflows"}, {"get", "get_workflow"}}},
	{"files", "Read ordinary workspace files", []struct{ command, tool string }{{"link", "get_file_link"}, {"list", "list_files"}, {"read", "read_file"}, {"search", "search_files"}}},
	{"runs", "Start, steer, stop, and inspect runs", []struct{ command, tool string }{{"list", "list_runs"}, {"get", "get_run"}, {"logs", "get_logs"}, {"start-step", "execute_step"}, {"start-workflow", "run_full_workflow"}, {"status", "run_status"}, {"message", "send_step_message"}, {"stop", "stop_step"}, {"stop-all", "stop_all_executions"}, {"executions", "list_executions"}}},
	{"schedules", "List, inspect, and trigger schedules", []struct{ command, tool string }{{"list", "list_schedules"}, {"runs", "get_schedule_runs"}, {"trigger", "trigger_schedule"}}},
	{"guidance", "Load server-owned external guidance", []struct{ command, tool string }{{"context", "get_agent_context"}, {"topics", "list_guidance_topics"}, {"topic", "get_guidance_topic"}}},
	{"knowledge", "Inspect workflow learnings, notes, and skills", []struct{ command, tool string }{{"list", "list_workflow_knowledge"}, {"read", "read_workflow_knowledge"}}},
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
	root.AddCommand(loginCommand(o), logoutCommand(o), skillsCommand(o), versionCommand(o), updateCommand(o))
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
	call := &cobra.Command{Use: "call NAME", Args: cobra.MaximumNArgs(1), Short: "Call any server tool; --input accepts a JSON object file or stdin"}
	addOperationFlags(call, "")
	call.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return o.call(cmd, args[0])
	}
	toolsCmd.AddCommand(call)
	root.AddCommand(toolsCmd)
	for _, group := range cliOperationGroups {
		groupCmd := &cobra.Command{Use: group.name, Short: group.description}
		for _, op := range group.operations {
			toolName := op.tool
			cmd := &cobra.Command{Use: op.command, Args: cobra.NoArgs, Short: "Call " + toolName}
			addOperationFlags(cmd, toolName)
			cmd.RunE = func(cmd *cobra.Command, _ []string) error { return o.call(cmd, toolName) }
			groupCmd.AddCommand(cmd)
		}
		if group.name == "files" {
			download := &cobra.Command{Use: "download", Short: "Download an asset to a new local file (including large binary files)", Args: cobra.NoArgs}
			download.Flags().String("workflow", "", "Workflow ID")
			download.Flags().String("path", "", "Workflow-relative asset path")
			download.Flags().String("output", "", "New local destination file; existing files are never overwritten")
			download.RunE = func(cmd *cobra.Command, _ []string) error {
				workflow, _ := cmd.Flags().GetString("workflow")
				p, _ := cmd.Flags().GetString("path")
				output, _ := cmd.Flags().GetString("output")
				client, err := o.client()
				if err != nil {
					return err
				}
				size, err := client.Download(cmd.Context(), workflow, p, output)
				if err != nil {
					return err
				}
				absolute, _ := filepath.Abs(output)
				return o.output(map[string]any{"path": absolute, "size": size})
			}
			groupCmd.AddCommand(download)
		}
		root.AddCommand(groupCmd)
	}
	plan := &cobra.Command{
		Use: "plan get", Args: cobra.MaximumNArgs(1),
		Short: "Read a workflow plan and configuration",
		Long:  "Read with 'plan get'. Plan mutations are not exposed: tokens read and run, never author.",
	}
	addOperationFlags(plan, "get_plan")
	plan.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		if args[0] != "get" {
			return fmt.Errorf("unknown plan operation %q: v1 supports only 'plan get'", args[0])
		}
		return o.call(cmd, "get_plan")
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

func skillsCommand(o *options) *cobra.Command {
	cmd := &cobra.Command{Use: "skills", Short: "Install the AgentWorks skill into a client skill directory"}
	var dir string
	var force bool
	install := &cobra.Command{Use: "install", Args: cobra.NoArgs, Short: "Install skills/agentworks/SKILL.md for Claude Code, Codex, and other skill clients", Long: "Copies the bundled AgentWorks skill (an entry pointer to the server-served guidance tools) into <dir>/agentworks/SKILL.md. Point --dir at a client skill directory, e.g. ~/.claude/skills or a project .claude/skills.", RunE: func(cmd *cobra.Command, _ []string) error {
		if dir == "" {
			dir = filepath.Join(".agents", "skills")
		}
		target, err := agentworksclient.InstallSkill(dir, force)
		if err != nil {
			return err
		}
		return o.output(map[string]any{"installed": target})
	}}
	install.Flags().StringVar(&dir, "dir", "", "Client skill directory (default .agents/skills)")
	install.Flags().BoolVar(&force, "force", false, "Overwrite an existing installed copy")
	cmd.AddCommand(install)
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
	// Global guidance topics accept no workflow_id; offering the flag would
	// only produce an avoidable invalid_arguments response.
	if tool != "list_workflows" && tool != "list_guidance_topics" && tool != "get_guidance_topic" {
		f.String("workflow", "", "Workflow ID (workflow_id)")
	}
	if tool == "" {
		f.String("expected-revision", "", "Revision from read_file/get_plan (reserved for a future write-enabled API)")
	}
	f.StringArray("set", nil, "Set a native argument as key=JSON; repeatable (quote string JSON values)")
	if strings.Contains(tool, "file") || tool == "read_workflow_knowledge" {
		f.String("path", "", "Workspace-relative file or directory path")
	}
	if tool == "get_guidance_topic" {
		f.String("topic", "", "Guidance topic from guidance topics")
	}
	if tool == "list_workflows" || tool == "list_files" || tool == "search_files" || tool == "list_runs" || tool == "get_run" || tool == "get_logs" || tool == "run_status" || tool == "get_schedule_runs" {
		f.Int("limit", 0, "Maximum results")
		f.Int("offset", 0, "Result offset")
	}
	if tool == "list_files" || tool == "search_files" {
		f.Int("depth", 0, "Directory traversal depth (1..8)")
	}
	if tool == "search_files" || tool == "list_workflows" {
		f.String("query", "", "Search query")
	}
	if tool == "get_run" || tool == "get_logs" {
		f.String("run-folder", "", "Run folder from list_runs")
	}
	if tool == "execute_step" {
		f.String("step-id", "", "Plan step ID or positional reference (e.g. '1')")
		f.String("group", "", "Variable group name")
		f.String("human-input", "", "Run-specific instructions or human_input response")
		f.String("tier", "", "LLM tier override: high, medium, or low")
	}
	if tool == "run_full_workflow" {
		f.String("group", "", "Variable group name to execute")
	}
	if tool == "run_status" || tool == "send_step_message" || tool == "stop_step" || tool == "stop_all_executions" {
		f.String("session", "", "Run session ID from a previous run call")
	}
	if tool == "run_status" {
		f.Int("since-index", -1, "Event index to poll from (-1 for the latest page)")
	}
	if tool == "send_step_message" || tool == "stop_step" {
		f.String("execution-id", "", "Execution ID from list_executions or run_status")
	}
	if tool == "send_step_message" {
		f.String("message", "", "Live correction for the running execution")
	}
	if tool == "get_schedule_runs" || tool == "trigger_schedule" {
		f.String("schedule-id", "", "Schedule ID from schedules list")
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
	for flagName, field := range map[string]string{"workflow": "workflow_id", "expected-revision": "expected_revision", "path": "path", "query": "query", "run-folder": "run_folder", "session": "session_id", "message": "message", "provider": "provider", "model": "model_id", "step": "existing_step_id", "title": "title", "reason": "reason", "request-id": "request_id", "response": "response", "action": "action", "topic": "topic", "step-id": "step_id", "execution-id": "execution_id", "schedule-id": "schedule_id", "group": "group_name", "human-input": "human_input", "tier": "tier"} {
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
