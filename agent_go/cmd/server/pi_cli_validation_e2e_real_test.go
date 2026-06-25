package server

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manishiitg/mcpagent/llm"
)

func TestPiCLIBuilderValidationGeminiE2E(t *testing.T) {
	if os.Getenv("RUN_PI_CLI_BUILDER_E2E") != "1" {
		t.Skip("set RUN_PI_CLI_BUILDER_E2E=1 to run the real Pi CLI builder validation smoke test")
	}

	loadPiCLIE2EEnv(t)
	apiKey := firstNonEmptyPiEnv("PI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY")
	if apiKey == "" {
		t.Skip("PI_API_KEY, GEMINI_API_KEY, or GOOGLE_API_KEY is required")
	}

	t.Cleanup(func() {
		_ = llm.CleanupPiCLIInteractiveSessions(context.Background())
	})

	resp := validatePiCLI(apiKey, "google/gemini-3.5-flash", map[string]interface{}{
		"pi_provider": "google",
	})
	if !resp.Valid {
		t.Fatalf("validatePiCLI returned invalid response: message=%q error=%q", resp.Message, resp.Error)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "pi cli is working") {
		t.Fatalf("validation message = %q, want Pi CLI success marker", resp.Message)
	}
}

func loadPiCLIE2EEnv(t *testing.T) {
	t.Helper()
	if firstNonEmptyPiEnv("PI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY") != "" {
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		return
	}
	for _, candidate := range []string{
		filepath.Clean(filepath.Join(wd, "../../../../multi-llm-provider-go/.env")),
		filepath.Clean(filepath.Join(wd, "../../../multi-llm-provider-go/.env")),
	} {
		if loadPiEnvFile(candidate) {
			return
		}
	}
}

func loadPiEnvFile(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	loaded := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		switch key {
		case "PI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY":
		default:
			continue
		}
		if os.Getenv(key) != "" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value == "" {
			continue
		}
		_ = os.Setenv(key, value)
		loaded = true
	}
	return loaded
}

func firstNonEmptyPiEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
