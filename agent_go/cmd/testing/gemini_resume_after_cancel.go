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

const geminiResumeAfterCancelProjectSettings = `{}`

var geminiResumeAfterCancelTestCmd = &cobra.Command{
	Use:   "gemini-resume-after-cancel",
	Short: "Test Gemini CLI resume behavior after canceling a resumed turn",
	Long: `Tests the Gemini CLI resume path in isolation.

Flow:
1. Start a normal Gemini CLI turn and capture the returned session ID and project dir ID.
2. Start a resumed turn and cancel it after the first streamed content chunk.
3. Start another resumed turn and verify that:
   - Gemini CLI is invoked with --resume
   - only the latest user message is passed via --prompt when resuming
   - the same Gemini project dir is reused across resumed turns

This is a focused regression test for Gemini cancel-then-resume behavior.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logLevel := "debug"
		logFile := viper.GetString("log-file")
		if logFile == "" {
			logFile = filepath.Join(os.TempDir(), fmt.Sprintf("gemini-resume-after-cancel-%d.log", time.Now().UnixNano()))
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
			modelID = "gemini-cli"
		}

		logger.Info("=== Gemini Resume After Cancel Test ===")
		logger.Info(fmt.Sprintf("Using model: %s", modelID))
		logger.Info(fmt.Sprintf("Debug log file: %s", logFile))

		llmInstance, err := llm.InitializeLLM(llm.Config{
			Provider:    llm.ProviderGeminiCLI,
			ModelID:     modelID,
			Temperature: 0,
			Logger:      logger,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize Gemini CLI LLM: %w", err)
		}

		sessionID, projectDirID, err := establishGeminiSession(llmInstance, logger)
		if err != nil {
			return err
		}

		if err := cancelResumedGeminiTurn(llmInstance, sessionID, projectDirID, logger); err != nil {
			return err
		}

		if err := verifyGeminiResumeAfterCancel(llmInstance, sessionID, projectDirID, logFile, logger); err != nil {
			return err
		}

		logger.Info("✅ Gemini resume-after-cancel test passed")
		return nil
	},
}

func establishGeminiSession(llmInstance llmtypes.Model, logger loggerv2.Logger) (string, string, error) {
	logger.Info("--- Step 1: Establish Gemini session ---")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := llmInstance.GenerateContent(
		ctx,
		[]llmtypes.MessageContent{
			textMessage(llmtypes.ChatMessageTypeHuman, "Reply with exactly READY and nothing else."),
		},
		llm.WithGeminiProjectSettings(geminiResumeAfterCancelProjectSettings),
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to establish initial Gemini session: %w", err)
	}

	sessionID := extractGeminiSessionID(resp)
	if sessionID == "" {
		return "", "", fmt.Errorf("Gemini response did not include gemini_session_id")
	}

	projectDirID := extractGeminiProjectDirID(resp)
	if projectDirID == "" {
		return "", "", fmt.Errorf("Gemini response did not include gemini_project_dir_id")
	}

	logger.Info(fmt.Sprintf("Captured Gemini session ID: %s", sessionID))
	logger.Info(fmt.Sprintf("Captured Gemini project dir ID: %s", projectDirID))
	return sessionID, projectDirID, nil
}

func cancelResumedGeminiTurn(llmInstance llmtypes.Model, sessionID string, projectDirID string, logger loggerv2.Logger) error {
	logger.Info("--- Step 2: Cancel a resumed Gemini turn after first streamed chunk ---")

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
			llm.WithGeminiResumeSessionID(sessionID),
			llm.WithGeminiProjectDirID(projectDirID),
			llm.WithGeminiProjectSettings(geminiResumeAfterCancelProjectSettings),
			llmtypes.WithStreamingChan(streamChan),
		)
		errCh <- err
	}()

	if err := waitForFirstContentChunk(streamChan, 45*time.Second); err != nil {
		return fmt.Errorf("did not receive Gemini streamed content before cancellation: %w", err)
	}

	logger.Info("Received first streamed Gemini chunk; canceling resumed turn")
	cancel()

	select {
	case err := <-errCh:
		if !isCancellationLikeError(err) {
			return fmt.Errorf("expected cancellation error after interrupting resumed Gemini turn, got: %w", err)
		}
		logger.Info(fmt.Sprintf("Canceled resumed Gemini turn as expected: %v", err))
		return nil
	case <-time.After(20 * time.Second):
		return fmt.Errorf("timed out waiting for canceled resumed Gemini turn to exit")
	}
}

func verifyGeminiResumeAfterCancel(llmInstance llmtypes.Model, sessionID string, projectDirID string, logFile string, logger loggerv2.Logger) error {
	logger.Info("--- Step 3: Verify later Gemini turn still resumes and only latest user message is sent ---")

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
		llm.WithGeminiResumeSessionID(sessionID),
		llm.WithGeminiProjectDirID(projectDirID),
		llm.WithGeminiProjectSettings(geminiResumeAfterCancelProjectSettings),
	)
	if err != nil {
		return fmt.Errorf("resumed Gemini verification turn failed: %w", err)
	}

	if resp == nil || len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Content) == "" {
		return fmt.Errorf("resumed Gemini verification turn returned no content")
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		return fmt.Errorf("failed to read debug log file: %w", err)
	}
	lines := strings.Split(string(logData), "\n")

	resumeLine := lastLogLineContaining(lines, "Executing Gemini CLI: gemini ")
	if resumeLine == "" {
		return fmt.Errorf("debug logs do not contain any Gemini CLI execution line")
	}
	if !strings.Contains(resumeLine, "--resume") || !strings.Contains(resumeLine, sessionID) {
		return fmt.Errorf("Gemini execution line does not show --resume %s", sessionID)
	}
	if !strings.Contains(resumeLine, latestSentinel) {
		return fmt.Errorf("Gemini execution line does not contain the latest resumed user prompt sentinel")
	}
	if strings.Contains(resumeLine, oldSentinel) {
		return fmt.Errorf("Gemini execution line still contains the old conversation sentinel")
	}

	projectDirLine := lastLogLineContaining(lines, "Using project dir with settings:")
	if projectDirLine == "" {
		return fmt.Errorf("debug logs do not contain the Gemini project dir reuse line")
	}
	if !strings.Contains(projectDirLine, "dirID="+projectDirID) {
		return fmt.Errorf("Gemini project dir log line does not contain dirID=%s", projectDirID)
	}
	if !strings.Contains(projectDirLine, "resume="+sessionID) {
		return fmt.Errorf("Gemini project dir log line does not contain resume=%s", sessionID)
	}

	logger.Info("Verified Gemini --resume, prompt trimming, and project dir reuse in logs")
	return nil
}

func extractGeminiSessionID(resp *llmtypes.ContentResponse) string {
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].GenerationInfo == nil {
		return ""
	}
	raw, ok := resp.Choices[0].GenerationInfo.Additional["gemini_session_id"]
	if !ok {
		return ""
	}
	sessionID, _ := raw.(string)
	return sessionID
}

func extractGeminiProjectDirID(resp *llmtypes.ContentResponse) string {
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].GenerationInfo == nil {
		return ""
	}
	raw, ok := resp.Choices[0].GenerationInfo.Additional["gemini_project_dir_id"]
	if !ok {
		return ""
	}
	projectDirID, _ := raw.(string)
	return projectDirID
}

func lastLogLineContaining(lines []string, needle string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], needle) {
			return lines[i]
		}
	}
	return ""
}
