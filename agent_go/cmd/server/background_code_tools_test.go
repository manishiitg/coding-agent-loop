package server

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/workproduct"
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

func TestTriggerAndAutoNotifyIsExposedInWritableCrew(t *testing.T) {
	if !triggerAutoNotifyAvailable(nil, "user-1", "", false, false, newProductToolGate(nil)) {
		t.Fatal("ordinary Builder chat must expose the trigger tool")
	}
	profile := &resolvedAgentProfile{Definition: workproduct.BuiltinAgentProfile()}
	gate := newProductToolGate(profile)
	workspace := "_users/user-1/Chats/Work/projects/demo"
	if !triggerAutoNotifyAvailable(profile, "user-1", workspace, false, false, gate) {
		t.Fatal("writable Crew project must expose the trigger tool")
	}
	registrar := &gateRecordingRegistrar{gate: gate}
	api := &StreamingAPI{bgAgentRegistry: NewBackgroundAgentRegistry()}
	if err := api.registerBackgroundCodeTools(registrar, QueryRequest{SelectedFolder: workspace}, "session-1", "user-1"); err != nil {
		t.Fatal(err)
	}
	if len(registrar.admitted) != 1 || registrar.admitted[0] != "trigger_and_auto_notify" {
		t.Fatalf("Crew admitted tools = %v", registrar.admitted)
	}
	if triggerAutoNotifyAvailable(profile, "user-1", workspace, false, true, gate) ||
		triggerAutoNotifyAvailable(profile, "user-1", "Chats/Work/projects", false, false, gate) ||
		triggerAutoNotifyAvailable(profile, "user-1", workspace, true, false, gate) {
		t.Fatal("trigger tool escaped Crew reader, landing-chat, or workflow-phase boundary")
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
