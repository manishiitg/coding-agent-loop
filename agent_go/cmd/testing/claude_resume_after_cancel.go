package testing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var claudeResumeAfterCancelTestCmd = &cobra.Command{
	Use:   "claude-resume-after-cancel",
	Short: "Test Claude Code resume behavior after canceling a resumed turn",
	Long: `Tests the Claude Code CLI resume path in isolation.

Flow:
1. Start a normal Claude Code turn and capture the returned session ID.
2. Start a resumed turn and cancel it after the first streamed content chunk.
3. Start another resumed turn and verify that:
   - Claude Code is invoked with --resume
   - only the latest user message is written to stdin when resuming

This is a focused regression test for cancel-then-resume behavior.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logLevel := "debug"
		logFile := viper.GetString("log-file")
		if logFile == "" {
			logFile = filepath.Join(os.TempDir(), fmt.Sprintf("claude-resume-after-cancel-%d.log", time.Now().UnixNano()))
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
			modelID = "claude-code"
		}

		logger.Info("=== Claude Resume After Cancel Test ===")
		logger.Info(fmt.Sprintf("Using model: %s", modelID))
		logger.Info(fmt.Sprintf("Debug log file: %s", logFile))

		llmInstance, err := llm.InitializeLLM(llm.Config{
			Provider:    llm.ProviderClaudeCode,
			ModelID:     modelID,
			Temperature: 0,
			Logger:      logger,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize Claude Code LLM: %w", err)
		}

		sessionID, err := establishClaudeSession(llmInstance, logger)
		if err != nil {
			return err
		}

		if err := cancelResumedTurn(llmInstance, sessionID, logger); err != nil {
			return err
		}

		if err := verifyResumeAfterCancel(llmInstance, sessionID, logFile, logger); err != nil {
			return err
		}

		logger.Info("✅ Claude resume-after-cancel test passed")
		return nil
	},
}

func establishClaudeSession(llmInstance llmtypes.Model, logger loggerv2.Logger) (string, error) {
	logger.Info("--- Step 1: Establish Claude session ---")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := llmInstance.GenerateContent(ctx, []llmtypes.MessageContent{
		textMessage(llmtypes.ChatMessageTypeHuman, "Reply with exactly READY and nothing else."),
	})
	if err != nil {
		return "", fmt.Errorf("failed to establish initial Claude session: %w", err)
	}

	sessionID := extractClaudeSessionID(resp)
	if sessionID == "" {
		return "", fmt.Errorf("Claude response did not include claude_code_session_id")
	}

	logger.Info(fmt.Sprintf("Captured Claude session ID: %s", sessionID))
	return sessionID, nil
}

func cancelResumedTurn(llmInstance llmtypes.Model, sessionID string, logger loggerv2.Logger) error {
	logger.Info("--- Step 2: Cancel a resumed turn after first streamed chunk ---")

	streamChan := make(chan llmtypes.StreamChunk, 128)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := llmInstance.GenerateContent(
			ctx,
			[]llmtypes.MessageContent{
				textMessage(llmtypes.ChatMessageTypeHuman, "Print the word STREAMING on separate numbered lines from 1 to 2000. Start immediately with line 1 and do not add any intro or summary."),
			},
			llm.WithResumeSessionID(sessionID),
			llmtypes.WithStreamingChan(streamChan),
		)
		errCh <- err
	}()

	if err := waitForFirstContentChunk(streamChan, 45*time.Second); err != nil {
		return fmt.Errorf("did not receive streamed content before cancellation: %w", err)
	}

	logger.Info("Received first streamed chunk; canceling resumed turn")
	cancel()

	select {
	case err := <-errCh:
		if !isCancellationLikeError(err) {
			return fmt.Errorf("expected cancellation error after interrupting resumed turn, got: %w", err)
		}
		logger.Info(fmt.Sprintf("Canceled resumed turn as expected: %v", err))
		return nil
	case <-time.After(20 * time.Second):
		return fmt.Errorf("timed out waiting for canceled resumed turn to exit")
	}
}

func verifyResumeAfterCancel(llmInstance llmtypes.Model, sessionID string, logFile string, logger loggerv2.Logger) error {
	logger.Info("--- Step 3: Verify later turn still resumes and only latest user message is sent ---")

	oldSentinel := "OLD_CONTEXT_SHOULD_NOT_BE_SENT_" + uuid.NewString()
	latestSentinel := "LATEST_PROMPT_AFTER_CANCEL_" + uuid.NewString()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, err := llmInstance.GenerateContent(
		ctx,
		[]llmtypes.MessageContent{
			textMessage(llmtypes.ChatMessageTypeHuman, oldSentinel),
			textMessage(llmtypes.ChatMessageTypeAI, "IGNORED_OLDER_ASSISTANT_REPLY"),
			textMessage(llmtypes.ChatMessageTypeHuman, latestSentinel+"\nReply with exactly RESUMED and nothing else."),
		},
		llm.WithResumeSessionID(sessionID),
	)
	if err != nil {
		return fmt.Errorf("resumed verification turn failed: %w", err)
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		return fmt.Errorf("failed to read debug log file: %w", err)
	}
	lines := strings.Split(string(logData), "\n")

	resumeLine := lastLogLineContaining(lines, "Executing Claude Code CLI: claude ")
	if resumeLine == "" {
		return fmt.Errorf("debug logs do not contain any Claude Code execution line")
	}
	if !strings.Contains(resumeLine, "--resume") || !strings.Contains(resumeLine, sessionID) {
		return fmt.Errorf("Claude execution line does not show --resume %s", sessionID)
	}

	inputLine := lastLogLineContaining(lines, "Input stream:")
	if inputLine == "" {
		return fmt.Errorf("debug logs do not contain the Claude input stream line")
	}
	if !strings.Contains(inputLine, latestSentinel) {
		return fmt.Errorf("Claude input stream line does not contain the latest resumed user prompt sentinel")
	}
	if strings.Contains(inputLine, oldSentinel) {
		return fmt.Errorf("Claude input stream line still contains the old conversation sentinel")
	}

	logger.Info("Verified Claude --resume and confirmed only latest user message was sent on resumed turn")
	return nil
}

func textMessage(role llmtypes.ChatMessageType, text string) llmtypes.MessageContent {
	return llmtypes.MessageContent{
		Role:  role,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: text}},
	}
}

func extractClaudeSessionID(resp *llmtypes.ContentResponse) string {
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].GenerationInfo == nil {
		return ""
	}
	raw, ok := resp.Choices[0].GenerationInfo.Additional["claude_code_session_id"]
	if !ok {
		return ""
	}
	sessionID, _ := raw.(string)
	return sessionID
}

func waitForFirstContentChunk(streamChan <-chan llmtypes.StreamChunk, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-streamChan:
			if !ok {
				return fmt.Errorf("stream closed before any content chunk arrived")
			}
			if chunk.Type == llmtypes.StreamChunkTypeContent && strings.TrimSpace(chunk.Content) != "" {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timed out after %s", timeout)
		}
	}
}

func isCancellationLikeError(err error) bool {
	if err == nil {
		return false
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "context canceled") ||
		strings.Contains(errText, "context canceled") ||
		strings.Contains(errText, "operation canceled") ||
		strings.Contains(errText, "execution failed: context canceled")
}
