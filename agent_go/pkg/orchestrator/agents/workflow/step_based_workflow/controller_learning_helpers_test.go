package step_based_workflow

import "testing"

// resolveLearningsWriteMethod is now hardcoded to "direct" — agent mode is
// retired. Any LearningsWriteMethod value (including legacy "agent" from old
// plan.json files) must coerce to direct.
func TestResolveLearningsWriteMethodAlwaysDirect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cfg  *AgentConfigs
	}{
		{"nil agent configs", nil},
		{"empty write method", &AgentConfigs{}},
		{"explicit direct", &AgentConfigs{LearningsWriteMethod: "direct"}},
		{"legacy agent (must coerce)", &AgentConfigs{LearningsWriteMethod: "agent"}},
		{"unknown value (must coerce)", &AgentConfigs{LearningsWriteMethod: "manual"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveLearningsWriteMethod(tc.cfg); got != LearnWriteMethodDirect {
				t.Fatalf("resolveLearningsWriteMethod = %q, want %q (agent mode is retired)", got, LearnWriteMethodDirect)
			}
		})
	}
}
