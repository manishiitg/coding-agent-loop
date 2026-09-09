package guidance

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// MaterializeReferenceSkill bundles every mode-allowed entry in
// referenceKinds into ONE Anthropic-pattern skill. Every mode uses the stable
// "builder-reference" identity so prompts and agents never need to know which
// transport or execution surface materialized the bundle.
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
			Name: "builder-reference",
			Description: "Product chat reference docs — detailed contracts and rules to consult before specific actions: " +
				"LLM/provider configuration via tools, skill management, memory, browser/media tools, " +
				"schedule and secret management, backup, debugging, and MCP bridge usage. Match this skill when you need deep " +
				"product chat reference material, then read the matching file under references/.",
			Intro: "This skill bundles product chat reference documentation. Match it when you need detailed rules, patterns, or contracts for any of the topics below — especially LLM/provider configuration, which is managed through dedicated tools and not by reading or editing `config/` files. Read the single matching file under `references/`. You don't need to read more than one unless the action spans multiple topics.",
		}
	}

	if mode == "run" {
		return referenceSkillSpec{
			Name:        "builder-reference",
			Description: "Workflow runtime references: executing and inspecting work, current results and costs, runtime context capture, human input and approvals, live reports, and tool/bridge contracts. Read the relevant reference before acting; Run cannot edit workflow design.",
			Intro:       "Read the reference matching this runtime request. The current mode and granted tools define authority; reading a reference does not permit design/config mutations or grant additional tools.",
		}
	}

	return referenceSkillSpec{
		Name: "builder-reference",
		Description: "Workflow workshop reference docs — detailed contracts and rules to consult before specific actions: " +
			"LLM/provider configuration via tools, main.py authoring, persistent stores (skill/kb/db), routing and " +
			"message-sequence patterns, workflow composition patterns, plan-design, report-plan, reporting-policy " +
			"(dashboard approval buttons and report-to-agent chat requests), human-in-the-loop " +
			"(choosing human review, approval, and feedback mechanisms), evaluation-plan, " +
			"optimizer playbook, file layout, schedule and secret " +
			"management. Match this skill when you need deep reference material for any of those topics, then read the " +
			"matching file under references/.",
		Intro: "This skill bundles the workflow workshop's reference documentation. Match it when you need detailed rules, patterns, or contracts for any of the topics below — especially LLM/provider configuration, which is managed through dedicated tools and not by reading or editing `config/` files. Read the single matching file under `references/`. You don't need to read more than one unless the action spans multiple topics.",
	}
}

// MaterializeGuidanceSkill bundles every mode-allowed entry in allKinds into
// ONE skill named "workflow-commands". Same Anthropic pattern: SKILL.md is
// the TOC, references/<kind>.md is the procedural flow for each slash
// command (design-plan, improve-evaluation, define-success, strategy-auditor, ...).
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
			"review-artifact-drift, ops-review, strategy-auditor, define-success, pulse, engineering-review, pulse-fixer, " +
			"improve-knowledge, improve-learnings, improve-database, improve-report, design-reporting-ui, specialize-advisors, design-plan. Match this skill when the user " +
			"invokes one of those slash commands or describes the same intent in chat, then read the matching file under " +
			"references/.",
		Intro:  "This skill bundles the workshop's canonical slash-command procedures. Match it when the user invokes one of these commands (e.g. `/design-plan`, `/improve-evaluation`) or describes the same intent in plain chat. Read the single matching file under `references/` — the prose there is your instructions for the turn, follow it verbatim.",
		Render: renderKind,
	})
}

// AttachReferenceSurface attaches the consolidated reference surface to the
// agent — at most three skills:
//
//   - system-tools (existing meta-skill: explains the tool surface and
//     read_skill)
//   - builder-reference (mode-filtered mega-skill bundling every
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
// mcpagent exposes every attached bundle through its intrinsic read_skill tool
// on API and coding-CLI transports. Native CLI projection is an optimization,
// not a separate access contract.
// MaterializeReferenceKindsAsSkills renders each named referenceKinds entry
// as its OWN individually-named skill, for a product that wants to declare
// reference material in profile.skills[] the way it would declare any other
// product-owned skill -- e.g. a product declaring "backup-strategy" or
// "secret-management" by name -- rather than relying on the mode-gated
// "builder-reference" bundle AttachReferenceSurface produces. mode gates
// eligibility exactly as it does there; requesting a kind not allowed in
// mode, or a name that doesn't exist, is an error (a product declaring a
// skill it can't actually have is a configuration bug, not something to
// silently drop).
//
// Reference templates (templates/system/*.md) don't use tmplData's per-turn
// fields (Focus/Iteration/RunFolder/WorkshopMode) -- they're static
// background material, not per-invocation guided flows -- so rendering once
// here with a zero-value tmplData is safe and produces the same content
// AttachReferenceSurface's bundle would have shown under this name.
func MaterializeReferenceKindsAsSkills(mode string, names []string) ([]*llmtypes.Skill, error) {
	out := make([]*llmtypes.Skill, 0, len(names))
	for _, name := range names {
		meta, ok := referenceKinds[name]
		if !ok {
			return nil, fmt.Errorf("unknown reference kind %q", name)
		}
		if !modeAllowedIn(name, mode, referenceKinds) {
			return nil, fmt.Errorf("reference kind %q is not allowed in mode %q", name, mode)
		}
		content, err := renderReferenceKind(name, tmplData{})
		if err != nil {
			return nil, fmt.Errorf("render reference kind %q: %w", name, err)
		}
		out = append(out, &llmtypes.Skill{
			Name:        name,
			Description: meta.Description,
			Content:     content,
			Source:      llmtypes.SkillSource{Origin: "builtin"},
		})
	}
	return out, nil
}

func AttachReferenceSurface(mode string, attach func(*llmtypes.Skill) error) error {
	if attach == nil {
		return fmt.Errorf("attach reference surface: nil attach function")
	}
	if meta := BuildSystemToolsSkill(mode); meta != nil {
		if err := attach(meta); err != nil {
			return fmt.Errorf("attach %s: %w", meta.Name, err)
		}
	}
	if refs := MaterializeReferenceSkill(mode); refs != nil {
		if err := attach(refs); err != nil {
			return fmt.Errorf("attach %s: %w", refs.Name, err)
		}
	}
	if cmds := MaterializeGuidanceSkill(mode); cmds != nil {
		if err := attach(cmds); err != nil {
			return fmt.Errorf("attach %s: %w", cmds.Name, err)
		}
	}
	return nil
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

	// Select overrides mode filtering when non-nil. Step execution selects by
	// the tools the agent actually holds rather than by mode — see PLAT-124 and
	// MaterializeStepExecutionReferenceSkill.
	Select func(kind string, meta kindMeta) bool
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
		if spec.Select != nil {
			if spec.Select(k, spec.Registry[k]) {
				allowed = append(allowed, k)
			}
			continue
		}
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
			// will be absent from read_skill's file list, making the
			// programming error visible without crashing the session.
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
