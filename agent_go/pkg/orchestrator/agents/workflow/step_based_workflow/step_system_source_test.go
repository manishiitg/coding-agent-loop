package step_based_workflow

import (
	"testing"
	"text/template"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/guidance"
)

// Guard the consumer boundary too: a future inline runtime edit must not
// silently diverge from the source the builder reads before authoring a step.
func TestStepRuntimeUsesBuilderPromptSource(t *testing.T) {
	for name, runtime := range map[string]*template.Template{
		"execution":    executionOnlySystemTemplate,
		"orchestrator": orchestratorSystemTemplate,
	} {
		if runtime.Tree.Root.String() != guidance.StepSystemPromptTemplate(name) {
			t.Fatalf("%s runtime diverged from builder-reference/step-system-prompts", name)
		}
	}
	for access, name := range map[string]string{
		DBAccessRead:      "managed-db-read",
		DBAccessReadWrite: "managed-db-write",
	} {
		if BuildManagedWorkflowDBGuidance(access) != guidance.StepSystemPromptTemplate(name) {
			t.Fatalf("%s database guidance diverged from builder reference", access)
		}
	}
}
