package guidance

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// MaterializeReferenceSkill bundles every mode-allowed entry in
// referenceKinds into ONE Anthropic-pattern skill. Workshop/run modes keep the
// historical "workflow-reference" name; multi-agent chat gets an explicitly
// chat-scoped name so CLI skill matching does not treat it as workflow-only
// guidance.
// the SKILL.md body is a table of contents; the deep content lives in
// references/<kind>.md supporting files. The agent's CLI matches the skill
// by description, then reads the specific reference file it needs.
//
// Returns nil if no kinds are allowed in the given mode (so callers can skip
// attaching without an extra check).
func MaterializeReferenceSkill(mode string) *llmtypes.Skill {
	spec := referenceSkillSpecForMode(mode)
	return buildMegaSkill(buildMegaSkillSpec{
		Mode:        mode,
		Registry:    referenceKinds,
		Name:        spec.Name,
		Description: spec.Description,
		Intro:       spec.Intro,
		Render:      renderReferenceKind,
	})
}

type referenceSkillSpec struct {
	Name        string
	Description string
	Intro       string
}

func referenceSkillSpecForMode(mode string) referenceSkillSpec {
	if mode == "multi-agent" {
		return referenceSkillSpec{
			Name: "multiagent-reference",
			Description: "Multi-agent chat reference docs — detailed contracts and rules to consult before specific actions: " +
				"LLM/provider configuration via tools, delegation, skill management, memory, browser/media tools, " +
				"schedule and secret management, backup, debugging, and MCP bridge usage. Match this skill when you need deep " +
				"multi-agent chat reference material, then read the matching file under references/.",
			Intro: "This skill bundles multi-agent chat reference documentation. Match it when you need detailed rules, patterns, or contracts for any of the topics below — especially LLM/provider configuration, which is managed through dedicated tools and not by reading or editing `config/` files. Read the single matching file under `references/`. You don't need to read more than one unless the action spans multiple topics.",
		}
	}

	return referenceSkillSpec{
		Name: "workflow-reference",
		Description: "Workflow workshop reference docs — detailed contracts and rules to consult before specific actions: " +
			"LLM/provider configuration via tools, main.py authoring, persistent stores (skill/kb/db), routing and " +
			"message-sequence patterns, workflow composition patterns, plan-design, report-plan, evaluation-plan, " +
			"optimizer playbook, file layout, schedule and secret " +
			"management. Match this skill when you need deep reference material for any of those topics, then read the " +
			"matching file under references/.",
		Intro: "This skill bundles the workflow workshop's reference documentation. Match it when you need detailed rules, patterns, or contracts for any of the topics below — especially LLM/provider configuration, which is managed through dedicated tools and not by reading or editing `config/` files. Read the single matching file under `references/`. You don't need to read more than one unless the action spans multiple topics.",
	}
}

// MaterializeGuidanceSkill bundles every mode-allowed entry in allKinds into
// ONE skill named "workflow-commands". Same Anthropic pattern: SKILL.md is
// the TOC, references/<kind>.md is the procedural flow for each slash
// command (design-plan, improve-evaluation, define-success, goal-advisor, ...).
//
// Procedural flows benefit from Focus/Iteration context when invoked via
// get_workflow_command_guidance — the materialized version is the no-context
// rendering, intended as a fallback for callers that don't go through that
// tool.
func MaterializeGuidanceSkill(mode string) *llmtypes.Skill {
	return buildMegaSkill(buildMegaSkillSpec{
		Mode:     mode,
		Registry: allKinds,
		Name:     "workflow-commands",
		Description: "Workflow workshop slash-command flows — canonical procedural guidance for design-plan, improve-evaluation, " +
			"review-speed/cost/code/artifact-drift, bug-review, llm-ops-review, define-success, pulse, pulse-setup, pulse-fixer, " +
			"improve-knowledge, improve-learnings, improve-database, improve-report, goal-advisor, design-plan. Match this skill when the user " +
			"invokes one of those slash commands or describes the same intent in chat, then read the matching file under " +
			"references/.",
		Intro:  "This skill bundles the workshop's canonical slash-command procedures. Match it when the user invokes one of these commands (e.g. `/design-plan`, `/improve-evaluation`) or describes the same intent in plain chat. Read the single matching file under `references/` — the prose there is your instructions for the turn, follow it verbatim.",
		Render: renderKind,
	})
}

// AttachReferenceSurface attaches the consolidated reference surface to the
// agent — at most three skills:
//
//   - system-tools (existing meta-skill: explains the tool surface,
//     get_reference_doc, precondition gates)
//   - workflow-reference / multiagent-reference (mega-skill bundling every
//     reference doc allowed in the current mode; SKILL.md TOC +
//     references/<kind>.md per topic)
//   - workflow-commands (mega-skill bundling every procedural flow)
//
// Why three folders instead of one per kind: ~25 individual skill folders
// per session bloats every CLI's skill listing (each entry costs prompt
// tokens), confuses description-based matching, and clutters the
// projection adapters' output dirs. Bundling related material under one
// skill with references/ subfiles is exactly the progressive-disclosure
// shape Anthropic's skill spec is designed for.
//
// Both surfaces still coexist with the get_reference_doc tool path — the
// DocReadTracker only marks a kind loaded when the tool is actually
// called, so precondition gates on deep maintenance and store tools
// mutations keep enforcing regardless of what the agent reads off disk.
func AttachReferenceSurface(mode string, attach func(*llmtypes.Skill)) {
	if meta := BuildSystemToolsSkill(mode); meta != nil {
		attach(meta)
	}
	if refs := MaterializeReferenceSkill(mode); refs != nil {
		attach(refs)
	}
	if cmds := MaterializeGuidanceSkill(mode); cmds != nil {
		attach(cmds)
	}
}

// buildMegaSkillSpec captures the inputs for buildMegaSkill so the two
// mega-skill constructors don't have to repeat the same plumbing.
type buildMegaSkillSpec struct {
	Mode        string
	Registry    map[string]kindMeta
	Name        string
	Description string
	Intro       string
	Render      func(kind string, data tmplData) (string, error)
}

// buildMegaSkill assembles one Anthropic-pattern skill from a kind registry:
// SKILL.md body = Intro + a TOC listing every mode-allowed kind with its
// description and a pointer to references/<kind>.md; SupportingFiles = one
// rendered template per kind. Returns nil if no kinds are allowed (so the
// caller can skip attachment).
func buildMegaSkill(spec buildMegaSkillSpec) *llmtypes.Skill {
	kinds := kindEnumFrom(spec.Registry)
	sort.Strings(kinds)

	allowed := make([]string, 0, len(kinds))
	for _, k := range kinds {
		if spec.Mode == "" || modeAllowedIn(k, spec.Mode, spec.Registry) {
			allowed = append(allowed, k)
		}
	}
	if len(allowed) == 0 {
		return nil
	}

	files := make([]llmtypes.SkillFile, 0, len(allowed))
	var toc strings.Builder
	for _, k := range allowed {
		meta := spec.Registry[k]
		text, err := spec.Render(k, tmplData{WorkshopMode: spec.Mode})
		if err != nil {
			// A render failure is a programming error in an embedded
			// template, but crashing the session over one bad kind is
			// worse than attaching the skill without it — the kind is
			// still reachable via get_reference_doc, whose render path
			// reports its own error.
			log.Printf("[GUIDANCE] materialize %s/%s: %v (skipping this reference)", spec.Name, k, err)
			continue
		}
		files = append(files, llmtypes.SkillFile{
			RelPath: "references/" + k + ".md",
			Content: []byte(text),
		})
		fmt.Fprintf(&toc, "- `references/%s.md` — %s\n", k, meta.Description)
	}

	body := spec.Intro + "\n\n## Available references\n\n" + toc.String()

	return &llmtypes.Skill{
		Name:            spec.Name,
		Description:     spec.Description,
		Content:         body,
		SupportingFiles: files,
		Metadata: map[string]string{
			"mode":  spec.Mode,
			"kinds": strings.Join(allowed, ","),
		},
		Source: llmtypes.SkillSource{Origin: "builtin"},
	}
}
