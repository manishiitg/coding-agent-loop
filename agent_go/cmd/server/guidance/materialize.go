package guidance

import (
	"fmt"
	"sort"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// MaterializeReferenceSkills converts every entry in referenceKinds into a
// standalone llmtypes.Skill — one skill per reference doc — so providers that
// can't reach the get_reference_doc MCP tool (cursor-cli, opencode-cli,
// codex-cli, gemini-cli, agy-cli) still see the same canonical reference
// material via their native skill mechanism. Adapters in multi-llm-provider-go
// project each Skill into the CLI's skills directory at session launch.
//
// Naming: "workflow-ref-<kind>". The "workflow-ref-" prefix namespaces these
// off from user-authored workspace skills so descriptions don't collide in
// each CLI's skill listing.
//
// mode filters by allowed workshop modes ("workshop", "run", "multi-agent",
// or whatever the registry uses). Empty string returns every kind.
func MaterializeReferenceSkills(mode string) []*llmtypes.Skill {
	return materializeFromRegistry(mode, referenceKinds, "workflow-ref-", renderReferenceKind)
}

// MaterializeGuidanceSkills does the same for allKinds (procedural guided
// flows). Note: procedural flows benefit from per-call Focus / Iteration
// rendering via get_workflow_command_guidance; the materialized version is
// the no-context rendering, intended as a fallback for non-MCP CLIs.
func MaterializeGuidanceSkills(mode string) []*llmtypes.Skill {
	return materializeFromRegistry(mode, allKinds, "workflow-cmd-", renderKind)
}

// AttachReferenceSurface attaches the full reference surface to the agent:
// the system-tools meta-skill (which advertises get_reference_doc and the
// precondition-gate semantics) plus a materialized SKILL.md per reference
// doc and per procedural-guidance kind. Both surfaces coexist on purpose:
//
//   - Materialized skills give every CLI a browseable, file-mounted view of
//     the reference content via its native skill UI (.claude/skills/,
//     .cursor/skills/, .agents/skills/).
//   - The meta-skill + get_reference_doc tool path remains the authoritative
//     way to satisfy precondition gates (DocReadTracker only marks a kind
//     loaded when the tool is actually called — reading a static SKILL.md
//     doesn't trip the tracker). Gated tools like harden_workflow keep
//     refusing until the agent makes the tool call.
//
// Pass attach as the agent's AttachSkill function. The mode string is the
// workshop mode ("workshop", "run", "multi-agent") and filters kinds by
// their per-mode allow-list.
func AttachReferenceSurface(mode string, attach func(*llmtypes.Skill)) {
	if meta := BuildSystemToolsSkill(mode); meta != nil {
		attach(meta)
	}
	for _, s := range MaterializeReferenceSkills(mode) {
		attach(s)
	}
	for _, s := range MaterializeGuidanceSkills(mode) {
		attach(s)
	}
}

func materializeFromRegistry(
	mode string,
	registry map[string]kindMeta,
	namePrefix string,
	render func(string, tmplData) (string, error),
) []*llmtypes.Skill {
	kinds := kindEnumFrom(registry)
	sort.Strings(kinds)

	out := make([]*llmtypes.Skill, 0, len(kinds))
	for _, kind := range kinds {
		meta := registry[kind]
		if mode != "" && !modeAllowedIn(kind, mode, registry) {
			continue
		}
		text, err := render(kind, tmplData{WorkshopMode: mode})
		if err != nil {
			panic(fmt.Sprintf("guidance: materialize %s%s: %v", namePrefix, kind, err))
		}
		out = append(out, &llmtypes.Skill{
			Name:        namePrefix + kind,
			Description: meta.Description,
			Content:     text,
			Metadata: map[string]string{
				"group": meta.Group,
				"modes": strings.Join(meta.Modes, ","),
				"kind":  kind,
			},
			Source: llmtypes.SkillSource{Origin: "builtin"},
		})
	}
	return out
}
