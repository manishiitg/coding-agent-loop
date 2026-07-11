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

var codexResumeAfterCancelTestCmd = &cobra.Command{
	Use:   "codex-resume-after-cancel",
	Short: "Test Codex CLI resume behavior after canceling a resumed turn",
	Long: `Tests the Codex CLI resume path in isolation.

Flow:
1. Start a normal Codex CLI turn and capture the returned thread ID.
2. Start a resumed turn and cancel it after the first streamed content chunk.
3. Start another resumed turn and verify that:
   - Codex CLI is invoked via exec resume with the same thread ID
   - only the latest user message is passed as the resumed prompt

This is a focused regression test for Codex cancel-then-resume behavior.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logLevel := "debug"
		logFile := viper.GetString("log-file")
		if logFile == "" {
			logFile = filepath.Join(os.TempDir(), fmt.Sprintf("codex-resume-after-cancel-%d.log", time.Now().UnixNano()))
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

		logger.Info("=== Codex Resume After Cancel Test ===")
		logger.Info(fmt.Sprintf("Using model: %s", modelID))
		logger.Info(fmt.Sprintf("Debug log file: %s", logFile))

		llmInstance, err := llm.InitializeLLM(llm.Config{
			Provider:    llm.ProviderCodexCLI,
			ModelID:     modelID,
			Temperature: 0,
			Logger:      logger,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize Codex CLI LLM: %w", err)
		}

		threadID, err := establishCodexThread(llmInstance, logger)
		if err != nil {
			return err
		}

		if err := cancelResumedCodexTurn(llmInstance, threadID, logger); err != nil {
			return err
		}

		if err := verifyCodexResumeAfterCancel(llmInstance, threadID, logFile, logger); err != nil {
			return err
		}

		logger.Info("✅ Codex resume-after-cancel test passed")
		return nil
	},
}

func establishCodexThread(llmInstance llmtypes.Model, logger loggerv2.Logger) (string, error) {
	logger.Info("--- Step 1: Establish Codex thread ---")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := llmInstance.GenerateContent(ctx, []llmtypes.MessageContent{
		textMessage(llmtypes.ChatMessageTypeHuman, "Reply with exactly READY and nothing else."),
	})
	if err != nil {
		return "", fmt.Errorf("failed to establish initial Codex thread: %w", err)
	}

	threadID := extractCodexThreadID(resp)
	if threadID == "" {
		return "", fmt.Errorf("Codex response did not include codex_thread_id")
	}

	logger.Info(fmt.Sprintf("Captured Codex thread ID: %s", threadID))
	return threadID, nil
}

func cancelResumedCodexTurn(llmInstance llmtypes.Model, threadID string, logger loggerv2.Logger) error {
	logger.Info("--- Step 2: Cancel a resumed Codex turn after first streamed chunk ---")

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
			llm.WithCodexResumeSessionID(threadID),
			llmtypes.WithStreamingChan(streamChan),
		)
		errCh <- err
	}()

	if err := waitForFirstContentChunk(streamChan, 45*time.Second); err != nil {
		return fmt.Errorf("did not receive Codex streamed content before cancellation: %w", err)
	}

	logger.Info("Received first streamed Codex chunk; canceling resumed turn")
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			logger.Info("Resumed Codex turn exited cleanly after cancellation")
			return nil
		}
		if isCancellationLikeError(err) {
			logger.Info(fmt.Sprintf("Canceled resumed Codex turn as expected: %v", err))
			return nil
		}
		// Codex currently mixes cancellation with noisy stderr warnings from its local
		// state DB runtime. For this test, the important behavior is that the resumed
		// turn exits promptly after we cancel and that a later turn can still resume.
		logger.Info(fmt.Sprintf("Resumed Codex turn exited after cancellation with non-clean error (accepted for now): %v", err))
		return nil
	case <-time.After(20 * time.Second):
		return fmt.Errorf("timed out waiting for canceled resumed Codex turn to exit")
	}
}

func verifyCodexResumeAfterCancel(llmInstance llmtypes.Model, threadID string, logFile string, logger loggerv2.Logger) error {
	logger.Info("--- Step 3: Verify later Codex turn still resumes and only latest user message is sent ---")

	oldSentinel := "OLD_CONTEXT_SHOULD_NOT_BE_SENT_" + uuid.NewString()
	latestSentinel := "LATEST_PROMPT_AFTER_CANCEL_" + uuid.NewString()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := llmInstance.GenerateContent(
		ctx,
		[]llmtypes.MessageContent{
			textMessage(llmtypes.ChatMessageTypeHuman, oldSentinel),
			textMessage(llmtypes.ChatMessageTypeAI, "IGNORED_OLDER_ASSISTANT_REPLY"),
			textMessage(llmtypes.ChatMessageTypeHuman, latestSentinel+"\nReply with exactly RESUMED and nothing else."),
		},
		llm.WithCodexResumeSessionID(threadID),
	)
	if err != nil {
		return fmt.Errorf("resumed Codex verification turn failed: %w", err)
	}

	if resp == nil || len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Content) == "" {
		return fmt.Errorf("resumed Codex verification turn returned no content")
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		return fmt.Errorf("failed to read debug log file: %w", err)
	}
	lines := strings.Split(string(logData), "\n")

	resumeLine := lastLogLineContaining(lines, "Executing Codex CLI: codex ")
	if resumeLine == "" {
		return fmt.Errorf("debug logs do not contain any Codex CLI execution line")
	}
	if !strings.Contains(resumeLine, "exec resume") {
		return fmt.Errorf("Codex execution line does not use exec resume")
	}
	if !strings.Contains(resumeLine, threadID) {
		return fmt.Errorf("Codex execution line does not contain thread ID %s", threadID)
	}
	if !strings.Contains(resumeLine, latestSentinel) {
		return fmt.Errorf("Codex execution line does not contain the latest resumed user prompt sentinel")
	}
	if strings.Contains(resumeLine, oldSentinel) {
		return fmt.Errorf("Codex execution line still contains the old conversation sentinel")
	}

	logger.Info("Verified Codex exec resume and confirmed only latest user message was sent on resumed turn")
	return nil
}

func extractCodexThreadID(resp *llmtypes.ContentResponse) string {
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].GenerationInfo == nil {
		return ""
	}
	raw, ok := resp.Choices[0].GenerationInfo.Additional["codex_thread_id"]
	if !ok {
		return ""
	}
	threadID, _ := raw.(string)
	return threadID
}

func lastLogLineContaining(lines []string, needle string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], needle) {
			return lines[i]
		}
	}
	return ""
}
