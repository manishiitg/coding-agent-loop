package testing

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/cmd/server/services"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var searchWebLLMProvidersTestCmd = &cobra.Command{
	Use:   "search-web-llm-providers",
	Short: "Test search_web_llm across published search providers",
	Long: `Test the provider-backed search_web_llm path across published providers.

This command calls the real search_web_llm executor directly, so it exercises
workspace config/published-llms.json routing, provider auth, CLI runtime
availability, and the provider's native web-search implementation.`,
	RunE: runSearchWebLLMProvidersTest,
}

func runSearchWebLLMProvidersTest(cmd *cobra.Command, args []string) error {
	loadTestingEnvFiles()

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	InitTestLogger(logFile, logLevel)
	logger := GetTestLogger()

	workspaceURL := strings.TrimSpace(viper.GetString("search-web-llm-providers.workspace-url"))
	if workspaceURL == "" {
		workspaceURL = strings.TrimSpace(os.Getenv("WORKSPACE_API_URL"))
	}
	if workspaceURL == "" {
		workspaceURL = "http://127.0.0.1:8081"
	}
	if err := os.Setenv("WORKSPACE_API_URL", workspaceURL); err != nil {
		return fmt.Errorf("failed to set WORKSPACE_API_URL: %w", err)
	}

	query := strings.TrimSpace(viper.GetString("search-web-llm-providers.query"))
	if query == "" {
		query = `Use web search to find https://example.com and answer with one concise sentence that includes the word "example".`
	}
	expectAny := parseCSVList(viper.GetString("search-web-llm-providers.expect-any"))
	if len(expectAny) == 0 {
		expectAny = []string{"example"}
	}

	timeoutValue := strings.TrimSpace(viper.GetString("search-web-llm-providers.provider-timeout"))
	providerTimeout, err := time.ParseDuration(timeoutValue)
	if err != nil {
		return fmt.Errorf("invalid --provider-timeout %q: %w", timeoutValue, err)
	}

	modelOverrides := parseProviderModelOverrides(viper.GetString("search-web-llm-providers.models"))
	providers := parseCSVList(viper.GetString("search-web-llm-providers.providers"))
	if len(providers) == 0 {
		providers = []string{"codex-cli", "cursor-cli", "claude-code", "vertex"}
	}

	published, exists, err := services.LoadPublishedLLMs(context.Background(), workspaceURL)
	if err != nil {
		return fmt.Errorf("failed to load config/published-llms.json from workspace: %w", err)
	}
	if !exists || len(published) == 0 {
		return fmt.Errorf("config/published-llms.json is required for search_web_llm provider tests")
	}

	keys, err := loadSearchWebProviderKeys(context.Background(), workspaceURL)
	if err != nil {
		logger.Warn(fmt.Sprintf("Provider key store unavailable; provider auth preflight may skip more tests: %v", err))
	}

	executor := virtualtools.CreateSearchWebLLMProviderTestExecutor(workspaceURL)
	if executor == nil {
		return fmt.Errorf("search_web_llm executor is not available")
	}

	includeUnconfigured := viper.GetBool("search-web-llm-providers.include-unconfigured")
	failFast := viper.GetBool("search-web-llm-providers.fail-fast")

	var passed, skipped, failed int
	var failures []string
	fmt.Printf("Testing search_web_llm providers\n")
	fmt.Printf("Workspace URL: %s\n", workspaceURL)
	fmt.Printf("Query: %s\n\n", query)

	for _, provider := range providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		modelID := strings.TrimSpace(modelOverrides[provider])

		if !includeUnconfigured {
			if reason := searchWebProviderSkipReason(provider, modelID, published, keys); reason != "" {
				skipped++
				fmt.Printf("[SKIP] %s/%s: %s\n", provider, displaySearchModel(modelID), reason)
				continue
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), providerTimeout)
		args := map[string]any{
			"query":    query,
			"provider": provider,
		}
		if modelID != "" {
			args["model_id"] = modelID
		}

		start := time.Now()
		result, err := executor(ctx, args)
		cancel()

		if err != nil {
			failed++
			msg := fmt.Sprintf("%s/%s failed after %s: %v", provider, displaySearchModel(modelID), time.Since(start).Round(time.Millisecond), err)
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}

		if strings.TrimSpace(result) == "" {
			failed++
			msg := fmt.Sprintf("%s/%s returned an empty response", provider, displaySearchModel(modelID))
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}
		if len(expectAny) > 0 && !responseContainsAny(result, expectAny) {
			failed++
			msg := fmt.Sprintf("%s/%s response did not contain any expected marker %v: %s", provider, displaySearchModel(modelID), expectAny, oneLinePreview(result, 220))
			failures = append(failures, msg)
			fmt.Printf("[FAIL] %s\n", msg)
			if failFast {
				break
			}
			continue
		}

		passed++
		fmt.Printf("[PASS] %s/%s in %s: %s\n", provider, displaySearchModel(modelID), time.Since(start).Round(time.Millisecond), oneLinePreview(result, 220))
	}

	fmt.Printf("\nSummary: %d passed, %d skipped, %d failed\n", passed, skipped, failed)
	if len(failures) > 0 {
		sort.Strings(failures)
		for _, failure := range failures {
			fmt.Printf("- %s\n", failure)
		}
		return fmt.Errorf("search_web_llm provider matrix had %d failure(s)", failed)
	}
	return nil
}

func loadSearchWebProviderKeys(ctx context.Context, workspaceURL string) (map[string]string, error) {
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

func searchWebProviderSkipReason(provider, modelID string, published []services.PublishedLLM, keys map[string]string) string {
	if !hasPublishedSearchProvider(provider, modelID, published) {
		if modelID != "" {
			return "provider/model is not published as search-capable in config/published-llms.json"
		}
		return "provider is not published as search-capable in config/published-llms.json"
	}

	switch provider {
	case "codex-cli":
		if _, err := exec.LookPath("codex"); err != nil {
			return "codex CLI is not installed or not on PATH"
		}
	case "claude-code":
		if _, err := exec.LookPath("claude"); err != nil {
			return "claude CLI is not installed or not on PATH"
		}
		if strings.TrimSpace(keys["anthropic"]) == "" {
			return "no anthropic auth found in workspace provider keys"
		}
	case "vertex":
		if strings.TrimSpace(keys["vertex"]) == "" {
			return "no vertex auth found in workspace provider keys"
		}
	default:
		return "unsupported search_web_llm provider"
	}
	return ""
}

func hasPublishedSearchProvider(provider, modelID string, published []services.PublishedLLM) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelID = strings.TrimSpace(modelID)
	for _, entry := range published {
		entryProvider := strings.ToLower(strings.TrimSpace(entry.Provider))
		entryModelID := strings.TrimSpace(entry.ModelID)
		if entryProvider != provider {
			continue
		}
		if modelID != "" && !strings.EqualFold(entryModelID, modelID) {
			continue
		}
		if !isPublishedSearchCapable(entryProvider, entryModelID) {
			continue
		}
		return true
	}
	return false
}

func isPublishedSearchCapable(provider, modelID string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude-code", "codex-cli", "cursor-cli":
		return true
	case "vertex":
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelID)), "gemini")
	default:
		return false
	}
}

func displaySearchModel(modelID string) string {
	if strings.TrimSpace(modelID) == "" {
		return "auto"
	}
	return modelID
}

func init() {
	searchWebLLMProvidersTestCmd.Flags().String("workspace-url", "", "Workspace API URL (default: WORKSPACE_API_URL or http://127.0.0.1:8081)")
	searchWebLLMProvidersTestCmd.Flags().String("query", "", "Web search query to run")
	searchWebLLMProvidersTestCmd.Flags().String("expect-any", "", "Comma-separated response markers; at least one must appear. Defaults to example")
	searchWebLLMProvidersTestCmd.Flags().String("providers", "", "Comma-separated providers to test (default: codex-cli,cursor-cli,claude-code,vertex)")
	searchWebLLMProvidersTestCmd.Flags().String("models", "", "Comma-separated provider=model overrides, e.g. codex-cli=gpt-5.3-codex-spark,vertex=gemini-3-flash-preview")
	searchWebLLMProvidersTestCmd.Flags().String("provider-timeout", "2m", "Timeout per provider")
	searchWebLLMProvidersTestCmd.Flags().Bool("include-unconfigured", false, "Attempt providers even when auth/runtime/published preflight is missing")
	searchWebLLMProvidersTestCmd.Flags().Bool("fail-fast", false, "Stop after the first provider failure")

	viper.BindPFlag("search-web-llm-providers.workspace-url", searchWebLLMProvidersTestCmd.Flags().Lookup("workspace-url"))
	viper.BindPFlag("search-web-llm-providers.query", searchWebLLMProvidersTestCmd.Flags().Lookup("query"))
	viper.BindPFlag("search-web-llm-providers.expect-any", searchWebLLMProvidersTestCmd.Flags().Lookup("expect-any"))
	viper.BindPFlag("search-web-llm-providers.providers", searchWebLLMProvidersTestCmd.Flags().Lookup("providers"))
	viper.BindPFlag("search-web-llm-providers.models", searchWebLLMProvidersTestCmd.Flags().Lookup("models"))
	viper.BindPFlag("search-web-llm-providers.provider-timeout", searchWebLLMProvidersTestCmd.Flags().Lookup("provider-timeout"))
	viper.BindPFlag("search-web-llm-providers.include-unconfigured", searchWebLLMProvidersTestCmd.Flags().Lookup("include-unconfigured"))
	viper.BindPFlag("search-web-llm-providers.fail-fast", searchWebLLMProvidersTestCmd.Flags().Lookup("fail-fast"))
}
