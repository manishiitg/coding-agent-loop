package testing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const codexMCPExpectedOutput = "PONG_FROM_CODEX_MCP_PROBE"

var codexMCPToolCallTestCmd = &cobra.Command{
	Use:   "codex-mcp-tool-call",
	Short: "Test Codex CLI non-interactive MCP tool execution",
	Long: `Tests that Codex CLI can execute an MCP tool through the provider adapter.

This catches Codex CLI approval regressions where MCP calls immediately return
"user cancelled MCP tool call" in non-interactive exec mode.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logLevel := "debug"
		logFile := viper.GetString("log-file")
		if logFile == "" {
			logFile = filepath.Join(os.TempDir(), fmt.Sprintf("codex-mcp-tool-call-%d.log", time.Now().UnixNano()))
		}
		if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}
		if err := os.Remove(logFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to clear previous log file: %w", err)
		}

		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		modelID := viper.GetString("test.model")
		if modelID == "" {
			modelID = "gpt-5.3-codex-spark"
		}

		logger.Info("=== Codex MCP Tool Call Test ===")
		logger.Info(fmt.Sprintf("Using model: %s", modelID))
		logger.Info(fmt.Sprintf("Debug log file: %s", logFile))

		serverPath, cleanup, err := writeCodexProbeMCPServer()
		if err != nil {
			return err
		}
		defer cleanup()

		llmInstance, err := llm.InitializeLLM(llm.Config{
			Provider:    llm.ProviderCodexCLI,
			ModelID:     modelID,
			Temperature: 0,
			Logger:      logger,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize Codex CLI LLM: %w", err)
		}

		if err := verifyCodexMCPToolCall(llmInstance, serverPath, logger); err != nil {
			return err
		}

		logger.Info("✅ Codex MCP tool-call test passed")
		return nil
	},
}

func writeCodexProbeMCPServer() (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "codex-mcp-probe-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp dir for MCP probe: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	serverPath := filepath.Join(tmpDir, "probe_mcp_server.py")
	serverCode := `from mcp.server.fastmcp import FastMCP

mcp = FastMCP("probe")

@mcp.tool()
def ping() -> str:
    return "` + codexMCPExpectedOutput + `"

if __name__ == "__main__":
    mcp.run()
`
	if err := os.WriteFile(serverPath, []byte(serverCode), 0o755); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write MCP probe server: %w", err)
	}
	return serverPath, cleanup, nil
}

func verifyCodexMCPToolCall(llmInstance llmtypes.Model, serverPath string, logger loggerv2.Logger) error {
	streamChan := make(chan llmtypes.StreamChunk, 128)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	overrides := []string{
		`mcp_servers.probe.command="python3"`,
		fmt.Sprintf("mcp_servers.probe.args=[%q]", serverPath),
		"mcp_servers.probe.tool_timeout_sec=30",
	}

	resp, err := llmInstance.GenerateContent(
		ctx,
		[]llmtypes.MessageContent{
			textMessage(llmtypes.ChatMessageTypeHuman, "Use the probe ping MCP tool exactly once. Then reply with exactly the tool output and nothing else."),
		},
		llm.WithCodexDisableShellTool(),
		llm.WithCodexApprovalPolicy("never"),
		llm.WithCodexConfigOverrides(overrides),
		llmtypes.WithStreamingChan(streamChan),
	)
	if err != nil {
		return fmt.Errorf("Codex MCP tool-call generation failed: %w", err)
	}

	var sawToolStart bool
	var sawExpectedToolResult bool
	var sawCancelledToolCall bool
	for chunk := range streamChan {
		switch chunk.Type {
		case llmtypes.StreamChunkTypeToolCallStart:
			if strings.Contains(chunk.ToolName, "probe") && strings.Contains(chunk.ToolName, "ping") {
				sawToolStart = true
				logger.Info(fmt.Sprintf("Observed Codex MCP tool start: %s", chunk.ToolName))
			}
		case llmtypes.StreamChunkTypeToolCallEnd:
			result := strings.TrimSpace(chunk.ToolResult)
			if strings.Contains(result, "user cancelled MCP tool call") {
				sawCancelledToolCall = true
			}
			if strings.Contains(result, codexMCPExpectedOutput) {
				sawExpectedToolResult = true
				logger.Info(fmt.Sprintf("Observed Codex MCP tool result: %s", result))
			}
		}
	}

	content := ""
	if resp != nil && len(resp.Choices) > 0 {
		content = strings.TrimSpace(resp.Choices[0].Content)
	}

	if sawCancelledToolCall || strings.Contains(content, "user cancelled MCP tool call") {
		return fmt.Errorf("Codex MCP tool call was cancelled instead of executed")
	}
	if !sawToolStart {
		return fmt.Errorf("Codex did not stream an MCP probe ping tool start")
	}
	if !sawExpectedToolResult {
		return fmt.Errorf("Codex did not stream expected MCP tool result %q", codexMCPExpectedOutput)
	}
	if !strings.Contains(content, codexMCPExpectedOutput) {
		return fmt.Errorf("Codex final response %q did not include expected MCP output %q", content, codexMCPExpectedOutput)
	}

	return nil
}
