package handlers

import "testing"

func TestIsAllowedShellExtraEnvKey(t *testing.T) {
	allowed := []string{
		"MCP_API_URL",
		"SECRET_TOKEN",
		"VAR_GROUP_NAME",
		"STEP_OUTPUT_DIR",
		"SCRIPT_VERBOSE",
		"RUNLOOP_STEP_ID",
		"DB_PATH",
		"PYTHONDONTWRITEBYTECODE",
	}
	for _, key := range allowed {
		if !isAllowedShellExtraEnvKey(key) {
			t.Errorf("expected %s to be allowed", key)
		}
	}

	for _, key := range []string{"PATH", "HOME", "AWS_PROFILE", "DATABASE_URL", "DB_PASSWORD"} {
		if isAllowedShellExtraEnvKey(key) {
			t.Errorf("expected %s to remain blocked", key)
		}
	}
}
