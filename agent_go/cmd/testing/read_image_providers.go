package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var readImageProvidersTestCmd = &cobra.Command{
	Use:   "read-image-providers",
	Short: "Test read_image across supported image-analysis providers",
	Long: `Test the provider-backed read_image analysis path across supported providers.

This command calls the wrapped read_image executor directly with an explicit
provider/model in context, so each provider is tested independently from the
normal agent decision loop and from config/image-analysis-config.json routing.`,
	RunE: runReadImageProvidersTest,
}

type readImageProviderResult struct {
	Filepath string `json:"filepath"`
	Query    string `json:"query"`
	Response string `json:"response"`
}

func runReadImageProvidersTest(cmd *cobra.Command, args []string) error {
	loadTestingEnvFiles()

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	workspaceURL := strings.TrimSpace(viper.GetString("read-image-providers.workspace-url"))
	if workspaceURL == "" {
		workspaceURL = strings.TrimSpace(os.Getenv("WORKSPACE_API_URL"))
	}
	if workspaceURL == "" {
		workspaceURL = "http://127.0.0.1:8081"
	}
	if err := os.Setenv("WORKSPACE_API_URL", workspaceURL); err != nil {
		return fmt.Errorf("failed to set WORKSPACE_API_URL: %w", err)
	}

	userID := strings.TrimSpace(viper.GetString("read-image-providers.user-id"))
	if userID == "" {
		userID = "default"
	}

	imagePath := strings.TrimSpace(viper.GetString("read-image-providers.image-path"))
	defaultImagePath := defaultReadImageProviderTestPath()
	if imagePath == "" {
		imagePath = defaultImagePath
	}
	if imagePath == "" {
		return fmt.Errorf("image path is required; pass --image-path with a full absolute workspace-docs image path")
	}
	if !filepath.IsAbs(imagePath) {
		return fmt.Errorf("--image-path must be a full absolute workspace-docs path, got %q", imagePath)
	}

	query := strings.TrimSpace(viper.GetString("read-image-providers.query"))
	if query == "" {
		query = "Describe the visible image content in one concise sentence."
	}
	expectAny := parseCSVList(viper.GetString("read-image-providers.expect-any"))
	if len(expectAny) == 0 && imagePath == defaultImagePath && strings.HasSuffix(filepath.ToSlash(imagePath), "/google.png") {
		expectAny = []string{"google"}
	}

	timeoutValue := strings.TrimSpace(viper.GetString("read-image-providers.provider-timeout"))
	providerTimeout, err := time.ParseDuration(timeoutValue)
	if err != nil {
		return fmt.Errorf("invalid --provider-timeout %q: %w", timeoutValue, err)
	}

	modelOverrides := parseProviderModelOverrides(viper.GetString("read-image-providers.models"))
	providers := parseCSVList(viper.GetString("read-image-providers.providers"))
	if len(providers) == 0 {
		providers = []string{"vertex", "z-ai", "kimi", "codex-cli", "claude-code"}
	}

	keys, err := loadReadImageProviderKeys(context.Background(), workspaceURL)
	if err != nil {
		logger.Warn(fmt.Sprintf("Provider key store unavailable; falling back to env/CLI auth only: %v", err))
	}

	executor := virtualtools.CreateReadImageProviderTestExecutor(workspaceURL, userID)
	if executor == nil {
		return fmt.Errorf("read_image executor is not available")
	}

	includeUnconfigured := viper.GetBool("read-image-providers.include-unconfigured")
	failFast := viper.GetBool("read-image-providers.fail-fast")

	var passed, skipped, failed int
	var failures []string
	fmt.Printf("Testing read_image providers\n")
	fmt.Printf("Workspace URL: %s\n", workspaceURL)
	fmt.Printf("User ID: %s\n", userID)
	fmt.Printf("Image: %s\n\n", imagePath)

	for _, provider := range providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		modelID := strings.TrimSpace(modelOverrides[provider])
		if modelID == "" {
			modelID = defaultReadImageProviderModel(provider)
		}
		if modelID == "" {
			failed++
			msg := fmt.Sprintf("%s: no default model known; pass --models %s=<model>", provider, provider)
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}

		apiKey := readImageProviderAPIKey(provider, keys)
		if !includeUnconfigured {
			if reason := readImageProviderSkipReason(provider, apiKey); reason != "" {
				skipped++
				fmt.Printf("[SKIP] %s/%s: %s\n", provider, modelID, reason)
				continue
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), providerTimeout)
		llmConfig := mcpagent.LLMModel{
			Provider: provider,
			ModelID:  modelID,
			APIKey:   apiKey,
		}
		ctx = context.WithValue(ctx, mcpagent.ToolExecutionLLMConfigKey, llmConfig)

		start := time.Now()
		result, err := executor(ctx, map[string]any{
			"filepath": imagePath,
			"query":    query,
		})
		cancel()

		if err != nil {
			failed++
			msg := fmt.Sprintf("%s/%s failed after %s: %v", provider, modelID, time.Since(start).Round(time.Millisecond), err)
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}

		var parsed readImageProviderResult
		if err := json.Unmarshal([]byte(result), &parsed); err != nil {
			failed++
			msg := fmt.Sprintf("%s/%s returned invalid JSON: %v", provider, modelID, err)
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}
		if strings.TrimSpace(parsed.Response) == "" {
			failed++
			msg := fmt.Sprintf("%s/%s returned an empty response", provider, modelID)
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}
		if responseIndicatesMissingImage(parsed.Response) {
			failed++
			msg := fmt.Sprintf("%s/%s did not appear to receive the image: %s", provider, modelID, oneLinePreview(parsed.Response, 180))
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}
		if len(expectAny) > 0 && !responseContainsAny(parsed.Response, expectAny) {
			failed++
			msg := fmt.Sprintf("%s/%s response did not contain any expected marker %v: %s", provider, modelID, expectAny, oneLinePreview(parsed.Response, 180))
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}

		passed++
		fmt.Printf("[PASS] %s/%s in %s: %s\n", provider, modelID, time.Since(start).Round(time.Millisecond), oneLinePreview(parsed.Response, 180))
	}

	fmt.Printf("\nSummary: %d passed, %d skipped, %d failed\n", passed, skipped, failed)
	if len(failures) > 0 {
		sort.Strings(failures)
		for _, failure := range failures {
			fmt.Printf("- %s\n", failure)
		}
		return fmt.Errorf("read_image provider matrix had %d failure(s)", failed)
	}
	return nil
}

func loadTestingEnvFiles() {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")
}

func defaultReadImageProviderTestPath() string {
	return firstExistingWorkspaceDocsAbsoluteTestPath(
		"_users/default/Chats/misc-topic/google.png",
		"_users/default/Downloads/hdfc_after_password_attempt_1.png",
		"Downloads/hdfc_after_password_attempt_1.png",
	)
}

func defaultReadImageProviderModel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "vertex":
		return "gemini-3-pro-preview"
	case "z-ai":
		return "glm-4.6v"
	case "kimi":
		return "kimi-k2.6"
	case "codex-cli":
		return "gpt-5.4-mini"
	case "claude-code":
		return "claude-code"
	default:
		return ""
	}
}

func parseCSVList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func parseProviderModelOverrides(value string) map[string]string {
	overrides := map[string]string{}
	for _, part := range parseCSVList(value) {
		provider, model, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		model = strings.TrimSpace(model)
		if provider != "" && model != "" {
			overrides[provider] = model
		}
	}
	return overrides
}

func loadReadImageProviderKeys(ctx context.Context, workspaceURL string) (map[string]string, error) {
	raw, exists, err := services.LoadProviderKeys(ctx, workspaceURL)
	if err != nil || !exists {
		return nil, err
	}
	keys := map[string]string{}
	for key, value := range raw {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			keys[key] = strings.TrimSpace(text)
		}
	}
	return keys, nil
}

func readImageProviderAPIKey(provider string, keys map[string]string) *string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	candidates := readImageProviderKeyCandidates(provider)
	for _, keyName := range candidates {
		if keys != nil {
			if value := strings.TrimSpace(keys[keyName]); value != "" {
				return &value
			}
		}
	}
	for _, envName := range readImageProviderEnvCandidates(provider) {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return &value
		}
	}
	return nil
}

func readImageProviderKeyCandidates(provider string) []string {
	switch provider {
	case "vertex":
		return []string{"vertex"}
	case "z-ai":
		return []string{"z-ai"}
	case "kimi":
		return []string{"kimi"}
	case "codex-cli":
		return []string{"codex_cli"}
	default:
		return nil
	}
}

func readImageProviderEnvCandidates(provider string) []string {
	switch provider {
	case "vertex":
		return []string{"VERTEX_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY"}
	case "z-ai":
		return []string{"Z_AI_API_KEY", "ZAI_API_KEY"}
	case "kimi":
		return []string{"KIMI_API_KEY", "MOONSHOT_API_KEY"}
	case "codex-cli":
		return []string{"CODEX_API_KEY"}
	default:
		return nil
	}
}

func readImageProviderSkipReason(provider string, apiKey *string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex-cli":
		if _, err := exec.LookPath("codex"); err != nil {
			return "codex CLI is not installed or not on PATH"
		}
	case "claude-code":
		if _, err := exec.LookPath("claude"); err != nil {
			return "claude CLI is not installed or not on PATH"
		}
	case "vertex":
		if apiKey == nil && strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")) == "" && strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")) == "" {
			return "no Vertex/Gemini API key or Google ADC environment found"
		}
	case "z-ai", "kimi":
		if apiKey == nil {
			return "no provider API key found in workspace auth or environment"
		}
	default:
		return "unsupported read_image provider"
	}
	return ""
}

func oneLinePreview(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func responseIndicatesMissingImage(value string) bool {
	lower := strings.ToLower(value)
	markers := []string{
		"no image",
		"don't see any image",
		"do not see any image",
		"image attached",
		"upload an image",
		"provide an image",
		"can't view images",
		"cannot view images",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func responseContainsAny(value string, markers []string) bool {
	lower := strings.ToLower(value)
	for _, marker := range markers {
		if marker = strings.ToLower(strings.TrimSpace(marker)); marker != "" && strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func init() {
	readImageProvidersTestCmd.Flags().String("workspace-url", "", "Workspace API URL (default: WORKSPACE_API_URL or http://127.0.0.1:8081)")
	readImageProvidersTestCmd.Flags().String("user-id", "default", "Workspace user ID to use when reading the image")
	readImageProvidersTestCmd.Flags().String("image-path", "", "Full absolute workspace-docs image path to test")
	readImageProvidersTestCmd.Flags().String("query", "", "Question to ask each image-analysis provider")
	readImageProvidersTestCmd.Flags().String("expect-any", "", "Comma-separated response markers; at least one must appear. Defaults to google for the built-in google.png test image")
	readImageProvidersTestCmd.Flags().String("providers", "", "Comma-separated providers to test (default: vertex,z-ai,kimi,codex-cli,claude-code)")
	readImageProvidersTestCmd.Flags().String("models", "", "Comma-separated provider=model overrides, e.g. vertex=gemini-3-flash-preview,kimi=kimi-k2.6")
	readImageProvidersTestCmd.Flags().String("provider-timeout", "2m", "Timeout per provider")
	readImageProvidersTestCmd.Flags().Bool("include-unconfigured", false, "Attempt providers even when auth/runtime preflight is missing")
	readImageProvidersTestCmd.Flags().Bool("fail-fast", false, "Stop after the first provider failure")

	viper.BindPFlag("read-image-providers.workspace-url", readImageProvidersTestCmd.Flags().Lookup("workspace-url"))
	viper.BindPFlag("read-image-providers.user-id", readImageProvidersTestCmd.Flags().Lookup("user-id"))
	viper.BindPFlag("read-image-providers.image-path", readImageProvidersTestCmd.Flags().Lookup("image-path"))
	viper.BindPFlag("read-image-providers.query", readImageProvidersTestCmd.Flags().Lookup("query"))
	viper.BindPFlag("read-image-providers.expect-any", readImageProvidersTestCmd.Flags().Lookup("expect-any"))
	viper.BindPFlag("read-image-providers.providers", readImageProvidersTestCmd.Flags().Lookup("providers"))
	viper.BindPFlag("read-image-providers.models", readImageProvidersTestCmd.Flags().Lookup("models"))
	viper.BindPFlag("read-image-providers.provider-timeout", readImageProvidersTestCmd.Flags().Lookup("provider-timeout"))
	viper.BindPFlag("read-image-providers.include-unconfigured", readImageProvidersTestCmd.Flags().Lookup("include-unconfigured"))
	viper.BindPFlag("read-image-providers.fail-fast", readImageProvidersTestCmd.Flags().Lookup("fail-fast"))
}
