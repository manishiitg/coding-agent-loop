package guidance

import (
	"fmt"
	"text/template"
)

// One embedded Markdown file supplies both runtime templates and the builder's
// on-demand reference. Parse at startup so a malformed branch fails immediately.
var stepSystemPrompts = template.Must(template.New("step-system-prompts").Parse(RenderSystemDoc("step-system-prompts")))

// StepSystemPromptTemplate returns a named body for the runtime's strict
// template registry to render with the effective run context. The reference's
// explanatory preamble is never sent to execution agents.
func StepSystemPromptTemplate(name string) string {
	t := stepSystemPrompts.Lookup(name)
	if t == nil {
		panic(fmt.Sprintf("guidance: unknown step system template %q", name))
	}
	return t.Tree.Root.String()
}
