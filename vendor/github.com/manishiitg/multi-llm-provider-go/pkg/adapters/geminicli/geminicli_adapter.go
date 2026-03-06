package geminicli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// pendingToolCall tracks a tool call that has started but hasn't received its result yet
type pendingToolCall struct {
	toolName  string
	toolID    string
	toolArgs  string
	startTime time.Time
}

// GeminiCLIAdapter implements the LLM interface for the Gemini CLI.
type GeminiCLIAdapter struct {
	apiKey  string
	modelID string
	logger  interfaces.Logger
}

// NewGeminiCLIAdapter creates a new instance of the GeminiCLIAdapter.
func NewGeminiCLIAdapter(apiKey string, modelID string, logger interfaces.Logger) *GeminiCLIAdapter {
	return &GeminiCLIAdapter{
		apiKey:  apiKey,
		modelID: modelID,
		logger:  logger,
	}
}

// inactivityTimeout is the maximum time to wait for output from the Gemini CLI
// before killing the process. This prevents indefinite hangs when the CLI stops
// producing output (e.g., due to corrupted session history).
// Note: the watchdog is suppressed while tool calls are in flight — only idle
// silence (no pending tools) counts toward this threshold.
const inactivityTimeout = 10 * time.Minute

// resolveProjectDir determines the per-invocation project directory.
// Priority: explicit dirID from metadata > resumeID-based > generate new unique ID.
// Returns the project directory path and the directory ID (for storing in response metadata).
func resolveProjectDir(opts *llmtypes.CallOptions, resumeID string) (projectDir string, dirID string) {
	// 1. Check for explicit project dir ID from metadata (highest priority)
	if opts.Metadata != nil && opts.Metadata.Custom != nil {
		if id, ok := opts.Metadata.Custom[MetadataKeyProjectDirID].(string); ok && id != "" {
			dirID = id
			projectDir = filepath.Join(os.TempDir(), "gemini-cli-project-"+dirID)
			return
		}
	}

	// 2. If resuming, derive from resume session ID (so resume finds its own dir)
	if resumeID != "" {
		dirID = resumeID
		projectDir = filepath.Join(os.TempDir(), "gemini-cli-project-"+dirID)
		return
	}

	// 3. Generate a unique directory ID for new invocations
	dirID = fmt.Sprintf("%d-%d", time.Now().UnixMilli(), rand.Intn(100000))
	projectDir = filepath.Join(os.TempDir(), "gemini-cli-project-"+dirID)
	return
}

// GenerateContent generates content using the Gemini CLI.
func (g *GeminiCLIAdapter) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// 0. Check for 'gemini' binary
	if _, err := exec.LookPath("gemini"); err != nil {
		return nil, fmt.Errorf("gemini cli not found in PATH. Please install it first (npm install -g @anthropic-ai/gemini-cli or see https://github.com/google-gemini/gemini-cli)")
	}

	// Parse options
	opts := &llmtypes.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	// 1. Prepare Command Arguments
	args := []string{"--output-format", "stream-json"}

	// Set approval mode only if explicitly provided (Policy Engine handles tool approval via TOML rules)
	if opts.Metadata != nil && opts.Metadata.Custom != nil {
		if mode, ok := opts.Metadata.Custom[MetadataKeyApprovalMode].(string); ok && mode != "" {
			args = append(args, "--approval-mode", mode)
		}
	}

	// Set model if specified via metadata or adapter default
	modelToUse := g.modelID
	if opts.Metadata != nil && opts.Metadata.Custom != nil {
		if model, ok := opts.Metadata.Custom[MetadataKeyGeminiModel].(string); ok && model != "" {
			modelToUse = model
		}
	}
	// Pass --model explicitly. Gemini CLI supports aliases: "pro", "flash", "flash-lite"
	// as well as full model names like "gemini-2.5-flash", "gemini-2.5-pro".
	// Default to "auto" — lets Gemini CLI pick the best model automatically.
	// Frontend can pass specific models: "pro", "flash", "flash-lite", or full names.
	if modelToUse == "" || modelToUse == "gemini-cli" {
		modelToUse = "auto"
	}
	args = append(args, "--model", modelToUse)

	// Handle --allowed-tools (deprecated in Gemini CLI 0.30+, use Policy Engine instead).
	if opts.Metadata != nil && opts.Metadata.Custom != nil {
		if allowedTools, ok := opts.Metadata.Custom[MetadataKeyAllowedTools].(string); ok && allowedTools != "" {
			for _, tool := range strings.Split(allowedTools, ",") {
				tool = strings.TrimSpace(tool)
				if tool != "" {
					args = append(args, "--allowed-tools", tool)
				}
			}
		}
	}

	// Handle resume session
	resumeID := ""
	if opts.Metadata != nil && opts.Metadata.Custom != nil {
		if rid, ok := opts.Metadata.Custom[MetadataKeyResumeSessionID].(string); ok && rid != "" {
			resumeID = rid
			args = append(args, "--resume", resumeID)
		}
	}

	// Extract system prompt and conversation messages
	var systemPrompts []string
	var convoMessages []llmtypes.MessageContent

	for _, msg := range messages {
		if msg.Role == llmtypes.ChatMessageTypeSystem {
			for _, part := range msg.Parts {
				if textPart, ok := part.(llmtypes.TextContent); ok {
					systemPrompts = append(systemPrompts, textPart.Text)
				}
			}
		} else {
			convoMessages = append(convoMessages, msg)
		}
	}

	// Handle system prompt via GEMINI_SYSTEM_MD temp file
	var systemPromptTempFile string
	systemPromptFile := ""
	if opts.Metadata != nil && opts.Metadata.Custom != nil {
		if spf, ok := opts.Metadata.Custom[MetadataKeySystemPromptFile].(string); ok && spf != "" {
			systemPromptFile = spf
		}
	}

	if systemPromptFile == "" && len(systemPrompts) > 0 {
		// Write system prompt to a temp file
		tmpFile, err := os.CreateTemp("", "gemini-system-*.md")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file for system prompt: %w", err)
		}
		systemPromptTempFile = tmpFile.Name()
		if _, err := tmpFile.WriteString(strings.Join(systemPrompts, "\n\n")); err != nil {
			tmpFile.Close()
			os.Remove(systemPromptTempFile)
			return nil, fmt.Errorf("failed to write system prompt to temp file: %w", err)
		}
		tmpFile.Close()
		systemPromptFile = systemPromptTempFile
	}

	// Ensure temp file cleanup
	if systemPromptTempFile != "" {
		defer os.Remove(systemPromptTempFile)
	}

	// StreamChan will be closed manually before return (not via defer)
	// to allow the retry logic to stream additional chunks if needed

	// 2. Extract the prompt text
	// Gemini CLI takes the prompt as a positional argument (plain text), not stream-json input
	var promptText string
	if resumeID != "" {
		// Resuming: only send the last user message (CLI has full history internally)
		for i := len(convoMessages) - 1; i >= 0; i-- {
			if convoMessages[i].Role == llmtypes.ChatMessageTypeHuman {
				promptText = extractTextFromMessage(convoMessages[i])
				break
			}
		}
	} else if len(convoMessages) > 1 {
		// Multiple messages without resume: build a conversation transcript
		// so the model has full context (Gemini CLI doesn't support stream-json input)
		var parts []string
		for _, msg := range convoMessages {
			role := "User"
			if msg.Role == llmtypes.ChatMessageTypeAI {
				role = "Assistant"
			}
			text := extractTextFromMessage(msg)
			if text != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", role, text))
			}
		}
		promptText = strings.Join(parts, "\n\n")
	} else {
		// Single message: send it directly
		for i := len(convoMessages) - 1; i >= 0; i-- {
			if convoMessages[i].Role == llmtypes.ChatMessageTypeHuman {
				promptText = extractTextFromMessage(convoMessages[i])
				break
			}
		}
	}

	// Use -p/--prompt flag for non-interactive (headless) mode.
	// Passing the prompt as a positional arg defaults to interactive mode.
	if promptText != "" {
		args = append(args, "--prompt", promptText)
	}

	// 3. Execute Command
	g.logger.Infof("Executing Gemini CLI: gemini %v", args)
	cmd := exec.CommandContext(ctx, "gemini", args...)

	// Build environment: inherit current env + add custom vars
	env := os.Environ()
	if g.apiKey != "" {
		env = append(env, "GEMINI_API_KEY="+g.apiKey)
	}
	if systemPromptFile != "" {
		env = append(env, "GEMINI_SYSTEM_MD="+systemPromptFile)
	}
	cmd.Env = env

	// If project settings JSON is provided, create a per-invocation project directory with
	// .gemini/settings.json and run the CLI from there. Each invocation gets its own
	// directory to prevent concurrent processes from corrupting each other's session files.
	// Resume calls reuse the same directory (keyed by session ID or explicit dir ID).
	var projectDir string
	var projectDirID string
	if opts.Metadata != nil && opts.Metadata.Custom != nil {
		if settingsJSON, ok := opts.Metadata.Custom[MetadataKeyProjectSettings].(string); ok && settingsJSON != "" {
			projectDir, projectDirID = resolveProjectDir(opts, resumeID)
			os.MkdirAll(projectDir, 0755)
			geminiDir := filepath.Join(projectDir, ".gemini")
			os.MkdirAll(geminiDir, 0755)
			os.WriteFile(filepath.Join(geminiDir, "settings.json"), []byte(settingsJSON), 0644)
			cmd.Dir = projectDir
			g.logger.Infof("Using project dir with settings: %s (dirID=%s, resume=%s)", projectDir, projectDirID, resumeID)
		}
	}

	// Use Pipe for stdout to parse as a stream
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Capture stderr via pipe for real-time 503/overload detection
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Put Gemini CLI in its own process group so we can kill it and all its
	// child processes (e.g. Node.js workers) as a group. Without this, killing
	// only the parent leaves child processes holding the stdout pipe open,
	// causing scanner.Scan() to block indefinitely after the watchdog fires.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start gemini cli: %w", err)
	}

	// Monitor stderr in real-time to detect 503/overload errors and notify the user.
	// Previously stderr was captured all-at-once after process exit, meaning
	// a 503 would only surface after the 3-minute inactivity watchdog fired.
	// Now we detect it immediately and send a status message to the stream so
	// the user knows Gemini is overloaded and retrying — without killing the process.
	var stderrBuf strings.Builder
	var detectedOverload atomic.Bool
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stderrBuf.WriteString(line + "\n")
			// Detect 503 / high-demand overload from Gemini API retry messages.
			// Only notify once — Gemini CLI will keep retrying on its own.
			if !detectedOverload.Load() && (strings.Contains(line, "503") ||
				strings.Contains(line, "high demand") ||
				strings.Contains(line, "UNAVAILABLE") ||
				strings.Contains(line, "Service Unavailable")) {
				detectedOverload.Store(true)
				g.logger.Errorf("Gemini CLI: 503 overload detected in stderr (process kept alive for retry): %s", truncate(line, 300))
				// Notify the user via the stream so they see feedback immediately
				if opts.StreamChan != nil {
					opts.StreamChan <- llmtypes.StreamChunk{
						Type:    llmtypes.StreamChunkTypeContent,
						Content: "\n⚠️ Gemini model is experiencing high demand (503). Retrying automatically, please wait…\n",
					}
				}
			}
		}
	}()

	// 4. Parse Streamed Output (JSONL, one JSON object per line)
	var finalResponse *llmtypes.ContentResponse
	var emptyResultSessionID string
	decodeDone := make(chan struct{}) // closed (not sent) to broadcast to all waiting goroutines
	pendingTools := make(map[string]*pendingToolCall)

	scanner := bufio.NewScanner(stdoutPipe)
	// Increase buffer size for potentially large JSON lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var sessionID string
	var resolvedModel string
	var accumulatedText strings.Builder

	// Inactivity watchdog: track the last time we received output.
	// pendingToolCalls counts tool_use events that haven't yet received a tool_result.
	// The watchdog skips the kill while any tool call is in flight.
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	var pendingToolCalls atomic.Int64

	// Progress heartbeat: if Gemini is silent for >30s (thinking, retrying, slow API),
	// send a single status message so the user knows something is happening.
	// Only one heartbeat is sent — repeated pings would just pollute the response.
	var lastContentTime atomic.Int64
	lastContentTime.Store(time.Now().UnixNano())
	var heartbeatSent atomic.Bool
	heartbeatDone := make(chan struct{})
	go func() {
		defer func() {
			g.logger.Infof("[LIFECYCLE] heartbeat goroutine exiting")
			close(heartbeatDone)
		}()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if heartbeatSent.Load() {
					continue // already notified user, don't spam
				}
				lastNano := lastContentTime.Load()
				elapsed := time.Since(time.Unix(0, lastNano))
				if elapsed >= 30*time.Second && opts.StreamChan != nil {
					heartbeatSent.Store(true)
					g.logger.Infof("Gemini CLI: no content for %ds, sending one-time progress heartbeat to user", int(elapsed.Seconds()))
					select {
					case opts.StreamChan <- llmtypes.StreamChunk{
						Type:    llmtypes.StreamChunkTypeContent,
						Content: "\n⏳ Gemini is still working on it, please wait…\n",
					}:
					default:
					}
				}
			case <-decodeDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start inactivity watchdog goroutine
	watchdogDone := make(chan struct{})
	go func() {
		defer func() {
			g.logger.Infof("[LIFECYCLE] watchdog goroutine exiting")
			close(watchdogDone)
		}()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				lastNano := lastActivity.Load()
				elapsed := time.Since(time.Unix(0, lastNano))
				if elapsed >= inactivityTimeout {
					if pendingToolCalls.Load() > 0 {
						// A tool call is still in flight — reset the clock and wait.
						g.logger.Infof("Inactivity watchdog: no output for %v but %d tool call(s) in flight, resetting timer", elapsed, pendingToolCalls.Load())
						lastActivity.Store(time.Now().UnixNano())
						continue
					}
					g.logger.Errorf("Inactivity watchdog: no output for %v, killing Gemini CLI process group", elapsed)
					if cmd.Process != nil {
						// Kill entire process group (negative PID) to also kill child processes
						// (e.g. Node.js workers) that hold the stdout pipe open.
						syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					}
					return
				}
			case <-decodeDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		g.logger.Infof("Starting Gemini stream decode loop...")
		for scanner.Scan() {
			lastActivity.Store(time.Now().UnixNano())
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				g.logger.Errorf("Failed to decode Gemini stream-json line: %v (line: %s)", err, truncate(line, 200))
				continue
			}
			g.logger.Infof("Decoded Gemini stream object of type: %v", raw["type"])
			g.logger.Debugf("Gemini CLI raw stream line: %s", truncate(line, 1000))

			msgType, _ := raw["type"].(string)
			switch msgType {
			case "init":
				// Extract session_id and model from init event
				if sid, ok := raw["session_id"].(string); ok {
					sessionID = sid
				}
				if m, ok := raw["model"].(string); ok && m != "" {
					resolvedModel = m
				}

			case "message":
				// Stream text content from assistant messages
				role, _ := raw["role"].(string)
				if role != "assistant" {
					continue
				}
				if content, ok := raw["content"].(string); ok && content != "" {
					accumulatedText.WriteString(content)
					lastContentTime.Store(time.Now().UnixNano()) // reset heartbeat timer
					heartbeatSent.Store(false)                   // allow one more heartbeat if Gemini goes quiet again
					if opts.StreamChan != nil {
						opts.StreamChan <- llmtypes.StreamChunk{
							Type:    llmtypes.StreamChunkTypeContent,
							Content: content,
						}
					}
				}
				// Also handle content as array of parts
				if contentArr, ok := raw["content"].([]interface{}); ok {
					for _, part := range contentArr {
						if partMap, ok := part.(map[string]interface{}); ok {
							if text, ok := partMap["text"].(string); ok && text != "" {
								accumulatedText.WriteString(text)
								if opts.StreamChan != nil {
									opts.StreamChan <- llmtypes.StreamChunk{
										Type:    llmtypes.StreamChunkTypeContent,
										Content: text,
									}
								}
							}
						}
					}
				}

			case "tool_use":
				// Tool call started
				// Gemini CLI uses "tool_name" while Claude uses "name"
				toolName, _ := raw["tool_name"].(string)
				if toolName == "" {
					toolName, _ = raw["name"].(string)
				}
				// Gemini CLI uses "tool_id" while Claude uses "tool_use_id"
				toolID, _ := raw["tool_id"].(string)
				if toolID == "" {
					toolID, _ = raw["tool_use_id"].(string)
				}
				if toolID == "" {
					toolID, _ = raw["id"].(string)
				}
				// Gemini CLI uses "parameters" while Claude uses "args"/"input"
				toolArgsRaw, _ := raw["parameters"]
				if toolArgsRaw == nil {
					toolArgsRaw, _ = raw["args"]
				}
				if toolArgsRaw == nil {
					toolArgsRaw, _ = raw["input"]
				}
				toolArgsJSON := ""
				if toolArgsRaw != nil {
					if argsBytes, err := json.Marshal(toolArgsRaw); err == nil {
						toolArgsJSON = string(argsBytes)
					}
				}

				pendingTools[toolID] = &pendingToolCall{
					toolName:  toolName,
					toolID:    toolID,
					toolArgs:  toolArgsJSON,
					startTime: time.Now(),
				}
				pendingToolCalls.Add(1) // signal watchdog: tool is in flight, don't kill

				lastContentTime.Store(time.Now().UnixNano()) // reset heartbeat timer on tool activity
				if opts.StreamChan != nil {
					opts.StreamChan <- llmtypes.StreamChunk{
						Type:       llmtypes.StreamChunkTypeToolCallStart,
						ToolName:   toolName,
						ToolCallID: toolID,
						ToolArgs:   toolArgsJSON,
					}
				}

			case "tool_result":
				// Tool call completed
				// Gemini CLI uses "tool_id" while Claude uses "tool_use_id"
				toolID, _ := raw["tool_id"].(string)
				if toolID == "" {
					toolID, _ = raw["tool_use_id"].(string)
				}
				if toolID == "" {
					toolID, _ = raw["id"].(string)
				}
				// Gemini CLI uses "output" while Claude uses "content"
				resultContent, _ := raw["output"].(string)
				if resultContent == "" {
					resultContent, _ = raw["content"].(string)
				}

			lastContentTime.Store(time.Now().UnixNano()) // reset heartbeat timer on tool activity
				if pt, ok := pendingTools[toolID]; ok {
					duration := time.Since(pt.startTime)
					if opts.StreamChan != nil {
						opts.StreamChan <- llmtypes.StreamChunk{
							Type:         llmtypes.StreamChunkTypeToolCallEnd,
							ToolName:     pt.toolName,
							ToolCallID:   pt.toolID,
							ToolArgs:     pt.toolArgs,
							ToolResult:   resultContent,
							ToolDuration: duration,
						}
					}
					delete(pendingTools, toolID)
					pendingToolCalls.Add(-1) // tool done; watchdog may kill again if idle
				}

			case "error":
				errMsg, _ := raw["message"].(string)
				if errMsg == "" {
					errMsg, _ = raw["error"].(string)
				}
				g.logger.Errorf("Gemini CLI error event: %s", errMsg)

			case "result":
				// Flush any remaining pending tool calls
				for _, pt := range pendingTools {
					if opts.StreamChan != nil {
						opts.StreamChan <- llmtypes.StreamChunk{
							Type:         llmtypes.StreamChunkTypeToolCallEnd,
							ToolName:     pt.toolName,
							ToolCallID:   pt.toolID,
							ToolArgs:     pt.toolArgs,
							ToolDuration: time.Since(pt.startTime),
						}
					}
				}
				pendingTools = make(map[string]*pendingToolCall)

				// Extract session_id from result if not already captured
				if sid, ok := raw["session_id"].(string); ok && sid != "" {
					sessionID = sid
				}

				// Parse the final result, passing accumulated text since result event
				// doesn't contain the response text itself
				finalResponse = g.mapResultToContentResponse(raw, sessionID, resolvedModel, accumulatedText.String())

				// Detect empty result: only retry if status is "success" and text is empty.
				// If status is "error" (e.g. 503 API failure), retrying with a finalization
				// prompt is pointless — there's no session content to summarize.
				resultStatus, _ := raw["status"].(string)
				if accumulatedText.String() == "" && sessionID != "" && resultStatus != "error" {
					emptyResultSessionID = sessionID
					g.logger.Infof("Detected empty result with sessionID=%s (status=%s), may need retry", emptyResultSessionID, resultStatus)
				} else if resultStatus == "error" {
					errObj, _ := raw["error"].(map[string]interface{})
					errMsg, _ := errObj["message"].(string)
					g.logger.Errorf("Gemini CLI result error (status=error), skipping retry: %s", errMsg)
				}
				// Send SIGTERM to let the CLI write session files before exiting.
				// SIGKILL would destroy session state and break --resume on next call.
				// If it doesn't exit within 5s, the watchdog or ctx cancellation will SIGKILL.
				g.logger.Infof("Gemini CLI result received, sending SIGTERM for graceful shutdown")
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
				}
			}
		}

		if err := scanner.Err(); err != nil {
			g.logger.Errorf("Scanner error reading Gemini CLI stdout: %v", err)
		}

		g.logger.Infof("[LIFECYCLE] decode goroutine done, broadcasting close(decodeDone)")
		close(decodeDone) // broadcast to all goroutines waiting on decodeDone
	}()

	// Wait for command completion or context cancellation
	g.logger.Infof("[LIFECYCLE] main: waiting for decodeDone or ctx.Done...")
	var cmdErr error
	select {
	case <-ctx.Done():
		g.logger.Errorf("Context cancelled/timed out: %v", ctx.Err())
		if cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		// Wait for decode goroutine to finish before closing StreamChan,
		// otherwise it may send on a closed channel causing a panic.
		<-decodeDone
		cmd.Wait()
		cmdErr = ctx.Err()
	case <-decodeDone:
		cmdErr = cmd.Wait()
	}
	g.logger.Infof("[LIFECYCLE] main: cmd.Wait() done, cmdErr=%v", cmdErr)

	// Wait for watchdog, heartbeat, and stderr goroutines to exit
	g.logger.Infof("[LIFECYCLE] main: waiting for watchdog goroutine...")
	<-watchdogDone
	g.logger.Infof("[LIFECYCLE] main: waiting for heartbeat goroutine...")
	<-heartbeatDone
	g.logger.Infof("[LIFECYCLE] main: waiting for stderr goroutine...")
	<-stderrDone
	g.logger.Infof("[LIFECYCLE] main: all goroutines joined, proceeding to cleanup")

	// Log stderr output
	if stderrOutput := stderrBuf.String(); stderrOutput != "" {
		g.logger.Infof("Gemini CLI stderr:\n%s", stderrOutput)
	}

	if cmdErr != nil {
		g.logger.Errorf("[LIFECYCLE] Gemini CLI failed with error: %v. stderr: %s", cmdErr, stderrBuf.String())
		if finalResponse == nil {
			g.logger.Infof("[LIFECYCLE] No finalResponse, closing StreamChan and returning error")
			if opts.StreamChan != nil {
				close(opts.StreamChan)
			}
			if detectedOverload.Load() {
				return nil, fmt.Errorf("gemini cli overloaded: model is experiencing high demand (503). Please try again later")
			}
			return nil, fmt.Errorf("gemini cli execution failed: %w", cmdErr)
		}
		g.logger.Infof("[LIFECYCLE] cmdErr but have finalResponse, continuing with partial result")
	}

	if finalResponse == nil {
		g.logger.Infof("[LIFECYCLE] No finalResponse and no cmdErr, closing StreamChan and returning error")
		if opts.StreamChan != nil {
			close(opts.StreamChan)
		}
		return nil, fmt.Errorf("failed to receive final result from gemini cli")
	}
	g.logger.Infof("[LIFECYCLE] GenerateContent returning successfully")

	// Store the project dir ID in the response so callers can track and reuse it
	if projectDirID != "" && finalResponse != nil && len(finalResponse.Choices) > 0 &&
		finalResponse.Choices[0].GenerationInfo != nil {
		finalResponse.Choices[0].GenerationInfo.Additional["gemini_project_dir_id"] = projectDirID
	}

	// If result was empty and we have a session ID, retry with a finalization prompt
	if emptyResultSessionID != "" {
		g.logger.Infof("Empty result detected, retrying with finalization prompt (sessionID=%s)", emptyResultSessionID)
		retryResp, retryErr := g.retryForFinalAnswer(ctx, emptyResultSessionID, opts, systemPromptFile, projectDir)
		if retryErr != nil {
			g.logger.Errorf("Retry for final answer failed: %v", retryErr)
		} else if retryResp != nil && len(retryResp.Choices) > 0 && retryResp.Choices[0].Content != "" {
			g.logger.Infof("Retry produced final answer (%d chars)", len(retryResp.Choices[0].Content))
			finalResponse = retryResp
			// Preserve project dir ID in retry response
			if projectDirID != "" && finalResponse.Choices[0].GenerationInfo != nil {
				finalResponse.Choices[0].GenerationInfo.Additional["gemini_project_dir_id"] = projectDirID
			}
		} else {
			g.logger.Infof("Retry produced empty result, using original response")
		}
	}

	if opts.StreamChan != nil {
		close(opts.StreamChan)
	}

	return finalResponse, nil
}

// GetModelID returns the model ID.
func (g *GeminiCLIAdapter) GetModelID() string {
	return g.modelID
}

// GetModelMetadata returns metadata for the model.
func (g *GeminiCLIAdapter) GetModelMetadata(modelID string) (*llmtypes.ModelMetadata, error) {
	return &llmtypes.ModelMetadata{
		ModelID:   modelID,
		Provider:  "gemini-cli",
		ModelName: "Gemini CLI",
		// Gemini CLI is free tier — zero pricing
		InputCostPer1MTokens:  0,
		OutputCostPer1MTokens: 0,
	}, nil
}

// --- Helper Functions ---

func extractTextFromMessage(msg llmtypes.MessageContent) string {
	var parts []string
	for _, part := range msg.Parts {
		if textPart, ok := part.(llmtypes.TextContent); ok {
			parts = append(parts, textPart.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (g *GeminiCLIAdapter) mapResultToContentResponse(raw map[string]interface{}, sessionID string, resolvedModel string, accumulatedText string) *llmtypes.ContentResponse {
	// The result event doesn't contain response text — use accumulated text from message events
	resultText := accumulatedText

	// Extract usage stats from "stats" object
	// Gemini CLI result stats: total_tokens, input_tokens, output_tokens, cached, input, duration_ms, tool_calls
	var inputTokens, outputTokens, totalTokens, cachedTokens, toolCalls int
	var durationMs float64
	if stats, ok := raw["stats"].(map[string]interface{}); ok {
		if v, ok := stats["input_tokens"].(float64); ok {
			inputTokens = int(v)
		}
		if v, ok := stats["output_tokens"].(float64); ok {
			outputTokens = int(v)
		}
		if v, ok := stats["total_tokens"].(float64); ok {
			totalTokens = int(v)
		}
		if v, ok := stats["cached"].(float64); ok {
			cachedTokens = int(v)
		}
		if v, ok := stats["duration_ms"].(float64); ok {
			durationMs = v
		}
		if v, ok := stats["tool_calls"].(float64); ok {
			toolCalls = int(v)
		}
	}
	// Also check top-level usage field (fallback)
	if usage, ok := raw["usage"].(map[string]interface{}); ok {
		if v, ok := usage["input_tokens"].(float64); ok && inputTokens == 0 {
			inputTokens = int(v)
		}
		if v, ok := usage["output_tokens"].(float64); ok && outputTokens == 0 {
			outputTokens = int(v)
		}
		if v, ok := usage["total_tokens"].(float64); ok && totalTokens == 0 {
			totalTokens = int(v)
		}
	}

	if totalTokens == 0 {
		totalTokens = inputTokens + outputTokens
	}

	usage := &llmtypes.Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
	}

	additional := map[string]interface{}{
		"gemini_session_id": sessionID,
	}
	if resolvedModel != "" {
		additional["gemini_model"] = resolvedModel
	}
	if cachedTokens > 0 {
		additional["gemini_cached_tokens"] = cachedTokens
	}
	if durationMs > 0 {
		additional["gemini_duration_ms"] = durationMs
	}
	if toolCalls > 0 {
		additional["gemini_tool_calls"] = toolCalls
	}

	genInfo := &llmtypes.GenerationInfo{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
		Additional:   additional,
	}

	// Set cache tokens in GenerationInfo if available
	if cachedTokens > 0 {
		genInfo.CachedContentTokens = &cachedTokens
	}

	return &llmtypes.ContentResponse{
		Choices: []*llmtypes.ContentChoice{
			{
				Content:        resultText,
				GenerationInfo: genInfo,
			},
		},
		Usage: usage,
	}
}

// retryForFinalAnswer resumes a Gemini CLI session that produced an empty result
// and asks it to provide a final summary. projectDir is the same directory used
// by the original invocation (so --resume can find the session).
func (g *GeminiCLIAdapter) retryForFinalAnswer(
	ctx context.Context,
	sessionID string,
	opts *llmtypes.CallOptions,
	systemPromptFile string,
	projectDir string,
) (*llmtypes.ContentResponse, error) {
	finalizationPrompt := "You have run out of turns. Please provide your final answer now based on what you have accomplished so far. Summarize results, findings, and any remaining work."

	args := []string{
		"--output-format", "stream-json",
		"--resume", sessionID,
	}

	// Set approval mode only if explicitly provided (Policy Engine handles tool approval via TOML rules)
	if opts.Metadata != nil && opts.Metadata.Custom != nil {
		if mode, ok := opts.Metadata.Custom[MetadataKeyApprovalMode].(string); ok && mode != "" {
			args = append(args, "--approval-mode", mode)
		}
	}

	// Use -p/--prompt for non-interactive (headless) mode
	args = append(args, "--prompt", finalizationPrompt)

	g.logger.Infof("Retry: executing Gemini CLI: gemini %v", args)
	cmd := exec.CommandContext(ctx, "gemini", args...)

	// Build environment
	env := os.Environ()
	if g.apiKey != "" {
		env = append(env, "GEMINI_API_KEY="+g.apiKey)
	}
	if systemPromptFile != "" {
		env = append(env, "GEMINI_SYSTEM_MD="+systemPromptFile)
	}
	cmd.Env = env

	// Use the same project directory as the original invocation
	if projectDir != "" {
		cmd.Dir = projectDir
		g.logger.Infof("Retry: using same project dir: %s", projectDir)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("retry: failed to create stdout pipe: %w", err)
	}

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	// Put Gemini CLI in its own process group so the watchdog can kill all child processes.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("retry: failed to start gemini cli: %w", err)
	}

	// Simplified decode loop
	var retryResponse *llmtypes.ContentResponse
	var retryAccumulatedText strings.Builder
	var retrySessionID string
	var retryResolvedModel string
	decodeDone := make(chan struct{}) // closed (not sent) to broadcast to all waiting goroutines

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	// Inactivity watchdog for retry
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				lastNano := lastActivity.Load()
				elapsed := time.Since(time.Unix(0, lastNano))
				if elapsed >= inactivityTimeout {
					g.logger.Errorf("Retry: inactivity watchdog: no output for %v, killing process group", elapsed)
					if cmd.Process != nil {
						syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					}
					return
				}
			case <-decodeDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		for scanner.Scan() {
			lastActivity.Store(time.Now().UnixNano())
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				g.logger.Errorf("Retry: failed to decode stream-json: %v", err)
				continue
			}

			msgType, _ := raw["type"].(string)
			switch msgType {
			case "init":
				if sid, ok := raw["session_id"].(string); ok {
					retrySessionID = sid
				}
				if m, ok := raw["model"].(string); ok && m != "" {
					retryResolvedModel = m
				}

			case "message":
				role, _ := raw["role"].(string)
				if role != "assistant" {
					continue
				}
				if content, ok := raw["content"].(string); ok && content != "" {
					retryAccumulatedText.WriteString(content)
					if opts.StreamChan != nil {
						opts.StreamChan <- llmtypes.StreamChunk{
							Type:    llmtypes.StreamChunkTypeContent,
							Content: content,
						}
					}
				}
				if contentArr, ok := raw["content"].([]interface{}); ok {
					for _, part := range contentArr {
						if partMap, ok := part.(map[string]interface{}); ok {
							if text, ok := partMap["text"].(string); ok && text != "" {
								retryAccumulatedText.WriteString(text)
								if opts.StreamChan != nil {
									opts.StreamChan <- llmtypes.StreamChunk{
										Type:    llmtypes.StreamChunkTypeContent,
										Content: text,
									}
								}
							}
						}
					}
				}

			case "result":
				if sid, ok := raw["session_id"].(string); ok && sid != "" {
					retrySessionID = sid
				}
				retryResponse = g.mapResultToContentResponse(raw, retrySessionID, retryResolvedModel, retryAccumulatedText.String())
				// Send SIGTERM to let the CLI write session files before exiting.
				g.logger.Infof("Retry: Gemini CLI result received, sending SIGTERM for graceful shutdown")
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
				}
			}
		}
		close(decodeDone) // broadcast to all goroutines waiting on decodeDone
	}()

	var cmdErr error
	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-decodeDone
		cmd.Wait()
		cmdErr = ctx.Err()
	case <-decodeDone:
		cmdErr = cmd.Wait()
	}

	// Wait for watchdog goroutine to exit
	<-watchdogDone

	if stderrOutput := stderrBuf.String(); stderrOutput != "" {
		g.logger.Infof("Retry: Gemini CLI stderr:\n%s", stderrOutput)
	}

	if cmdErr != nil {
		g.logger.Errorf("Retry: Gemini CLI failed: %v", cmdErr)
		if retryResponse == nil {
			return nil, fmt.Errorf("retry: gemini cli execution failed: %w", cmdErr)
		}
	}

	return retryResponse, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
