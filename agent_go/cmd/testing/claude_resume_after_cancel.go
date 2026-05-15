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
2. Start a resumed turn and cancel it while it is running.
3. Start another resumed turn and verify that:
   - Claude Code still resumes from the original native session ID
   - previous native session context is still available

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

		sessionID, codeword, err := establishClaudeSession(llmInstance, logger)
		if err != nil {
			return err
		}

		if err := cancelResumedTurn(llmInstance, sessionID, logger); err != nil {
			return err
		}

		if err := verifyResumeAfterCancel(llmInstance, sessionID, codeword, logger); err != nil {
			return err
		}

		logger.Info("✅ Claude resume-after-cancel test passed")
		return nil
	},
}

func establishClaudeSession(llmInstance llmtypes.Model, logger loggerv2.Logger) (string, string, error) {
	logger.Info("--- Step 1: Establish Claude session ---")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	codeword := "CLAUDE_RESUME_AFTER_CANCEL_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	resp, err := llmInstance.GenerateContent(ctx, []llmtypes.MessageContent{
		textMessage(llmtypes.ChatMessageTypeHuman, "Remember this codeword for later: "+codeword+". Reply with exactly READY and nothing else."),
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to establish initial Claude session: %w", err)
	}

	sessionID := extractClaudeSessionID(resp)
	if sessionID == "" {
		return "", "", fmt.Errorf("Claude response did not include claude_code_session_id")
	}

	logger.Info(fmt.Sprintf("Captured Claude session ID: %s", sessionID))
	return sessionID, codeword, nil
}

func cancelResumedTurn(llmInstance llmtypes.Model, sessionID string, logger loggerv2.Logger) error {
	logger.Info("--- Step 2: Cancel a resumed turn while it is running ---")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := llmInstance.GenerateContent(
			ctx,
			[]llmtypes.MessageContent{
				textMessage(llmtypes.ChatMessageTypeHuman, "Write the word STILL_RUNNING on separate numbered lines from 1 to 5000. Start immediately with line 1 and do not add any intro or summary."),
			},
			llm.WithResumeSessionID(sessionID),
		)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			return fmt.Errorf("resumed turn completed before cancellation could be tested")
		}
		return fmt.Errorf("resumed turn failed before cancellation: %w", err)
	case <-time.After(3 * time.Second):
	}

	logger.Info("Canceling in-flight resumed turn")
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

func verifyResumeAfterCancel(llmInstance llmtypes.Model, sessionID string, codeword string, logger loggerv2.Logger) error {
	logger.Info("--- Step 3: Verify later turn still resumes native Claude context ---")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := llmInstance.GenerateContent(
		ctx,
		[]llmtypes.MessageContent{
			textMessage(llmtypes.ChatMessageTypeHuman, "OLD_CONTEXT_SHOULD_NOT_BE_SENT"),
			textMessage(llmtypes.ChatMessageTypeAI, "OLD_ASSISTANT_SHOULD_NOT_BE_SENT"),
			textMessage(llmtypes.ChatMessageTypeHuman, "What codeword did I ask you to remember? Reply exactly with the codeword and nothing else."),
		},
		llm.WithResumeSessionID(sessionID),
	)
	if err != nil {
		return fmt.Errorf("resumed verification turn failed: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return fmt.Errorf("resumed verification response has no choices")
	}
	if !strings.Contains(strings.TrimSpace(resp.Choices[0].Content), codeword) {
		return fmt.Errorf("resumed verification response %q did not include codeword %q", resp.Choices[0].Content, codeword)
	}

	logger.Info("Verified Claude native resume still works after canceling a resumed turn")
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
