package server

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type triggerToolRecordingRegistrar struct{ names []string }

func (r *triggerToolRecordingRegistrar) RegisterCustomTool(name, _ string, _ map[string]interface{}, _ func(context.Context, map[string]interface{}) (string, error), _ string) error {
	r.names = append(r.names, name)
	return nil
}

func (r *triggerToolRecordingRegistrar) RegisterCustomToolWithTimeout(name, _ string, _ map[string]interface{}, _ func(context.Context, map[string]interface{}) (string, error), _ time.Duration, _ string) error {
	r.names = append(r.names, name)
	return nil
}

func TestRegisterTriggerAndAutoNotifyIsOneTool(t *testing.T) {
	api := &StreamingAPI{bgAgentRegistry: NewBackgroundAgentRegistry()}
	registrar := &triggerToolRecordingRegistrar{}
	if err := api.registerBackgroundCodeTools(registrar, QueryRequest{}, "session-1", "user-1"); err != nil {
		t.Fatal(err)
	}
	if len(registrar.names) != 1 || registrar.names[0] != "trigger_and_auto_notify" {
		t.Fatalf("registered tools = %v", registrar.names)
	}
}

func TestTriggerPythonRunsPlainCodeAndCapturesOutput(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	stdout, stderr, err := executeTriggerPython(
		context.Background(), python, "",
		`import time
time.sleep(0.01)
print("condition met")`,
	)
	if err != nil {
		t.Fatalf("executeTriggerPython: %v; stderr=%s", err, stderr)
	}
	if stdout != "condition met" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestTriggerPythonReportsFailure(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	_, stderr, err := executeTriggerPython(
		context.Background(), python, "",
		`raise RuntimeError("watch failed")`,
	)
	if err == nil || !strings.Contains(stderr, "RuntimeError: watch failed") {
		t.Fatalf("error=%v stderr=%q", err, stderr)
	}
}

func TestTriggerPythonHonorsContextTimeout(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err = executeTriggerPython(ctx, python, "", `import time; time.sleep(5)`)
	if err == nil {
		t.Fatal("expected timed-out trigger process to fail")
	}
}

func TestTriggerAutoNotifyTimeoutValidation(t *testing.T) {
	got, err := triggerAutoNotifyTimeout(nil)
	if err != nil || got != triggerAutoNotifyDefaultTimeout {
		t.Fatalf("default timeout = %v, %v", got, err)
	}
	got, err = triggerAutoNotifyTimeout(float64(12))
	if err != nil || got != 12*time.Second {
		t.Fatalf("explicit timeout = %v, %v", got, err)
	}
	if _, err := triggerAutoNotifyTimeout(1.5); err == nil {
		t.Fatal("expected fractional timeout to fail")
	}
}

func TestTriggerEnvironmentDoesNotForwardSecrets(t *testing.T) {
	t.Setenv("SECRET_TRIGGER_TEST", "do-not-forward")
	t.Setenv("LANG", "en_US.UTF-8")
	joined := strings.Join(triggerAutoNotifyEnvironment(), "\n")
	if strings.Contains(joined, "SECRET_TRIGGER_TEST") {
		t.Fatalf("secret was forwarded: %s", joined)
	}
	if !strings.Contains(joined, "LANG=en_US.UTF-8") {
		t.Fatalf("safe environment field missing: %s", joined)
	}
}
