// Package enginedetect provides detection, auth-configuration, and live
// validation of the four supported local coding-agent engines: Claude Code,
// OpenAI Codex CLI, Cursor CLI, and Pi CLI.
//
// The detection + validation logic here is a faithful copy of the equivalent
// server-side logic (cmd/server/multiagent_llm_tools.go and
// cmd/server/llm_config_handlers.go), adapted to build provider API keys from
// environment variables only (no workspace key store, no model catalog).
package enginedetect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/mcpagent/llm"
)

// Engine describes a single coding-agent engine: whether its runtime CLI is
// installed, whether auth is configured, and whether it is ready to use.
type Engine struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	RuntimeCommand   string `json:"runtime_command,omitempty"`
	RuntimeAvailable bool   `json:"runtime_available"`
	AuthConfigured   bool   `json:"auth_configured"`
	Usable           bool   `json:"usable"`
	SetupHint        string `json:"setup_hint,omitempty"`
	Deprecated       bool   `json:"deprecated,omitempty"`
}

// codingEngineIDs is the ordered list of the four supported coding agents.
var codingEngineIDs = []string{
	string(llm.ProviderClaudeCode),
	string(llm.ProviderCodexCLI),
	string(llm.ProviderCursorCLI),
	string(llm.ProviderPiCLI),
}

// engineDisplayNames maps engine id -> human display name (copied from
// providerStaticInfoMap in cmd/server/llm_provider_manifest.go).
var engineDisplayNames = map[string]string{
	string(llm.ProviderClaudeCode): "Claude Code",
	string(llm.ProviderCodexCLI):   "OpenAI Codex CLI",
	string(llm.ProviderCursorCLI):  "Cursor CLI",
	string(llm.ProviderPiCLI):      "Pi CLI",
}

// Detect returns detection info for the four supported coding-agent engines,
// building provider API keys from environment variables only.
func Detect(ctx context.Context) []Engine {
	keys := buildProviderAPIKeysFromEnv()

	engines := make([]Engine, 0, len(codingEngineIDs))
	for _, id := range codingEngineIDs {
		authConfigured, _ := providerAuthConfigured(id, keys)
		usable, runtime, runtimeOK := providerUsable(id, authConfigured)

		runtimeAvailable := runtimeOK != nil && *runtimeOK
		runtimeMissing := runtimeOK != nil && !*runtimeOK

		hint := ""
		if !usable {
			hint = discoverySetupHint(id, runtimeMissing)
		}

		engines = append(engines, Engine{
			ID:               id,
			Name:             engineDisplayNames[id],
			RuntimeCommand:   runtime,
			RuntimeAvailable: runtimeAvailable,
			AuthConfigured:   authConfigured,
			Usable:           usable,
			SetupHint:        hint,
			Deprecated:       false,
		})
	}
	return engines
}

// --- helpers copied from cmd/server/multiagent_llm_tools.go ---

func normalizeManagedProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func providerRuntime(provider string) string {
	normalized := normalizeManagedProvider(provider)
	switch normalized {
	case string(llm.ProviderClaudeCode):
		return "claude"
	case string(llm.ProviderCodexCLI):
		return "codex"
	case string(llm.ProviderCursorCLI):
		return "cursor-agent"
	case string(llm.ProviderAgyCLI):
		return "agy"
	case string(llm.ProviderPiCLI):
		return "pi"
	}
	return ""
}

func runtimeAvailableForProvider(provider string) (string, error) {
	command := providerRuntime(provider)
	if command == "" {
		return "", nil
	}
	if command == "pi" {
		if explicit := strings.TrimSpace(os.Getenv("PI_BIN")); explicit != "" {
			if info, err := os.Stat(explicit); err == nil && !info.IsDir() {
				return explicit, nil
			}
			return explicit, fmt.Errorf("PI_BIN is set to %q but no executable file was found", explicit)
		}
		if path, err := exec.LookPath("pi"); err == nil {
			return path, nil
		}
		if path, err := exec.LookPath("npx"); err == nil {
			return path, nil
		}
		return "pi", fmt.Errorf("Pi CLI not found. Install @earendil-works/pi-coding-agent so pi is available on PATH, or install Node.js/npm so npx can run it.")
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return command, fmt.Errorf("%s not found. Install the CLI so %s is available on the backend PATH.", command, command)
	}
	return path, nil
}

func providerRuntimeAvailable(provider string) *bool {
	command := providerRuntime(provider)
	if command == "" {
		return nil
	}
	_, err := runtimeAvailableForProvider(provider)
	ok := err == nil
	return &ok
}

func providerAuthConfigured(provider string, keys *llm.ProviderAPIKeys) (bool, string) {
	if keys == nil {
		keys = &llm.ProviderAPIKeys{}
	}
	switch normalizeManagedProvider(provider) {
	case string(llm.ProviderClaudeCode):
		return true, "Claude Code CLI login"
	case string(llm.ProviderCodexCLI):
		return true, "Codex CLI login or CODEX_API_KEY/workspace provider auth"
	case string(llm.ProviderCursorCLI):
		if keys.CursorCLI != nil && strings.TrimSpace(*keys.CursorCLI) != "" {
			return true, "CURSOR_API_KEY or workspace provider auth"
		}
		configured, _ := cursorCLILocalAuthState()
		return configured, "Cursor CLI login or CURSOR_API_KEY/workspace provider auth"
	case string(llm.ProviderPiCLI):
		return piProviderAuthConfigured(keys), "Provider-specific Pi API key or workspace provider auth"
	default:
		return false, "unknown provider"
	}
}

func piProviderAuthConfigured(keys *llm.ProviderAPIKeys) bool {
	if keys == nil {
		return false
	}
	if keys.PiCLI != nil && strings.TrimSpace(*keys.PiCLI) != "" {
		return true
	}
	for _, value := range keys.PiProviderKeys {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func providerUsable(provider string, authConfigured bool) (bool, string, *bool) {
	runtime := providerRuntime(provider)
	runtimeOK := providerRuntimeAvailable(provider)
	usable := authConfigured
	if runtimeOK != nil {
		usable = usable && *runtimeOK
	}
	return usable, runtime, runtimeOK
}

// --- cursor auth probe (copied from cmd/server/multiagent_llm_tools.go) ---

var cursorCLIAuthProbeCache = struct {
	sync.Mutex
	checkedAt     time.Time
	authenticated bool
	conclusive    bool
}{}

var cursorCLIStatusJSON = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "cursor-agent", "status", "--format", "json").Output()
}

func cursorCLILoginRequiredMessage() string {
	return "Cursor Agent CLI is installed but not logged in. Run `cursor-agent login` in a terminal, or set CURSOR_API_KEY, then try again."
}

func cursorCLILocalAuthState() (authenticated, conclusive bool) {
	if _, err := exec.LookPath("cursor-agent"); err != nil {
		return false, false
	}

	cursorCLIAuthProbeCache.Lock()
	defer cursorCLIAuthProbeCache.Unlock()

	if !cursorCLIAuthProbeCache.checkedAt.IsZero() && time.Since(cursorCLIAuthProbeCache.checkedAt) < 30*time.Second {
		return cursorCLIAuthProbeCache.authenticated, cursorCLIAuthProbeCache.conclusive
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := cursorCLIStatusJSON(ctx)
	authenticated, conclusive = cursorCLIAuthStatus(out)
	if err != nil && !conclusive {
		// A timeout or transient status-command failure is not evidence that the
		// user logged out. Preserve a previously confirmed login and otherwise
		// report an inconclusive probe so execution can try the real adapter.
		if cursorCLIAuthProbeCache.conclusive && cursorCLIAuthProbeCache.authenticated {
			authenticated = true
			conclusive = true
		}
	}

	cursorCLIAuthProbeCache.checkedAt = time.Now()
	cursorCLIAuthProbeCache.authenticated = authenticated
	cursorCLIAuthProbeCache.conclusive = conclusive
	return authenticated, conclusive
}

func cursorCLIAuthStatus(out []byte) (authenticated, conclusive bool) {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return false, false
	}

	var status struct {
		IsAuthenticated *bool  `json:"isAuthenticated"`
		Status          string `json:"status"`
	}
	if json.Unmarshal(out, &status) == nil {
		if status.IsAuthenticated != nil {
			return *status.IsAuthenticated, true
		}
		switch strings.ToLower(strings.TrimSpace(status.Status)) {
		case "authenticated", "logged_in", "logged-in":
			return true, true
		case "unauthenticated", "not_authenticated", "not-authenticated", "logged_out", "logged-out":
			return false, true
		default:
			return false, false
		}
	}

	lower := strings.ToLower(text)
	if strings.Contains(lower, "logged in as") || strings.Contains(lower, "status: authenticated") {
		return true, true
	}
	if strings.Contains(lower, "not logged in") || strings.Contains(lower, "status: unauthenticated") || strings.Contains(lower, "logged out") {
		return false, true
	}
	return false, false
}

// --- setup hint (copied from cmd/server/llm_config_handlers.go) ---

func discoverySetupHint(provider string, runtimeMissing bool) string {
	if runtimeMissing {
		switch provider {
		case "codex-cli":
			return "Install Codex CLI so the codex command is available on the backend PATH."
		case "cursor-cli":
			return "Install Cursor CLI so the cursor-agent command is available on the backend PATH."
		case "agy-cli":
			return "Install Antigravity CLI so the agy command is available on the backend PATH."
		case "pi-cli":
			return "Install Pi CLI with npm install -g @earendil-works/pi-coding-agent, or ensure npx is available on the backend PATH."
		case "claude-code":
			return "Install Claude Code so the claude command is available on the backend PATH."
		default:
			return "Install the provider CLI so its command is available on the backend PATH."
		}
	}

	switch provider {
	case "codex-cli":
		return "Run codex login or set CODEX_API_KEY, then test again."
	case "cursor-cli":
		return "Run cursor-agent login or set CURSOR_API_KEY, then test again."
	case "agy-cli":
		return "Run agy locally and complete Antigravity sign-in, then test again."
	case "pi-cli":
		return "Set PI_API_KEY, GEMINI_API_KEY, or GOOGLE_API_KEY, then test again."
	case "claude-code":
		return "Run claude to finish Claude Code authentication, then test again."
	default:
		return "Provider auth was not detected in the server environment or workspace provider keys."
	}
}

// --- ENV-only provider API keys (copied from cmd/server/llm_config_handlers.go
// buildProviderAPIKeysFromEnv) ---

func buildProviderAPIKeysFromEnv() *llm.ProviderAPIKeys {
	keys := &llm.ProviderAPIKeys{}
	setProviderKeyFromEnv := func(provider llm.Provider, envNames ...string) {
		for _, envName := range envNames {
			if s := strings.TrimSpace(os.Getenv(envName)); s != "" {
				keys.SetKeyForProvider(provider, &s)
				return
			}
		}
	}

	setProviderKeyFromEnv(llm.ProviderOpenAI, "OPENAI_API_KEY")
	setProviderKeyFromEnv(llm.ProviderOpenRouter, "OPENROUTER_API_KEY", "OPEN_ROUTER_API_KEY")
	setProviderKeyFromEnv(llm.ProviderAnthropic, "ANTHROPIC_API_KEY")
	setProviderKeyFromEnv(llm.ProviderZAI, "ZAI_API_KEY")
	setProviderKeyFromEnv(llm.ProviderKimi, "KIMI_API_KEY")
	if s := os.Getenv("VERTEX_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GOOGLE_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GEMINI_API_KEY"); s != "" {
		keys.Vertex = &s
	} else if s := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); s != "" {
		keys.Vertex = &s
	}
	if region := os.Getenv("BEDROCK_REGION"); region != "" {
		keys.Bedrock = &llm.BedrockConfig{Region: region}
	}
	// Codex CLI: only use explicit CODEX_API_KEY (not OPENAI_API_KEY).
	// Codex CLI has its own stored auth via `codex login`.
	if s := os.Getenv("CODEX_API_KEY"); s != "" {
		keys.CodexCLI = &s
	}
	if s := os.Getenv("CURSOR_API_KEY"); s != "" {
		keys.CursorCLI = &s
	}
	if s := os.Getenv("AGY_API_KEY"); s != "" {
		keys.AgyCLI = &s
	}
	if s := os.Getenv("PI_API_KEY"); s != "" {
		keys.PiCLI = &s
	} else if s := os.Getenv("GEMINI_API_KEY"); s != "" {
		keys.PiCLI = &s
	} else if s := os.Getenv("GOOGLE_API_KEY"); s != "" {
		keys.PiCLI = &s
	}
	if s := os.Getenv("MINIMAX_API_KEY"); s != "" {
		keys.MiniMax = &s
	}
	keys.PiProviderKeys = buildPiProviderKeysFromEnv()
	if s := os.Getenv("ELEVENLABS_API_KEY"); s != "" {
		keys.ElevenLabs = &s
	}
	if s := os.Getenv("DEEPGRAM_API_KEY"); s != "" {
		keys.Deepgram = &s
	}
	if endpoint := os.Getenv("AZURE_AI_ENDPOINT"); endpoint != "" {
		apiKey := os.Getenv("AZURE_AI_API_KEY")
		apiVer := os.Getenv("AZURE_AI_API_VERSION")
		region := os.Getenv("AZURE_AI_REGION")
		keys.Azure = &llm.AzureAPIConfig{
			Endpoint:   endpoint,
			APIKey:     apiKey,
			APIVersion: apiVer,
			Region:     region,
		}
	}
	return keys
}

func buildPiProviderKeysFromEnv() map[string]string {
	envByProvider := map[string][]string{
		"google":            {"PI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"},
		"google-vertex":     {"PI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"},
		"openai":            {"OPENAI_API_KEY"},
		"anthropic":         {"ANTHROPIC_API_KEY"},
		"openrouter":        {"OPENROUTER_API_KEY"},
		"deepseek":          {"DEEPSEEK_API_KEY"},
		"nvidia":            {"NVIDIA_API_KEY"},
		"mistral":           {"MISTRAL_API_KEY"},
		"groq":              {"GROQ_API_KEY"},
		"cerebras":          {"CEREBRAS_API_KEY"},
		"xai":               {"XAI_API_KEY"},
		"zai":               {"ZAI_API_KEY"},
		"zai-coding-cn":     {"ZAI_CODING_CN_API_KEY"},
		"opencode":          {"OPENCODE_API_KEY"},
		"opencode-go":       {"OPENCODE_API_KEY"},
		"fireworks":         {"FIREWORKS_API_KEY"},
		"together":          {"TOGETHER_API_KEY"},
		"kimi-coding":       {"KIMI_API_KEY"},
		"moonshotai":        {"KIMI_API_KEY"},
		"moonshotai-cn":     {"KIMI_API_KEY"},
		"minimax":           {"MINIMAX_API_KEY"},
		"minimax-cn":        {"MINIMAX_CN_API_KEY"},
		"vercel-ai-gateway": {"AI_GATEWAY_API_KEY"},
	}
	result := map[string]string{}
	for provider, envNames := range envByProvider {
		for _, envName := range envNames {
			if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
				result[provider] = value
				break
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
