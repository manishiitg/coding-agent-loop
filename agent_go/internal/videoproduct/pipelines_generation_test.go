package videoproduct

import (
	"strings"
	"testing"
)

// The generation pipelines are the only ones that spend the user's money —
// the infographic route's assets stage makes ffmpeg placeholders and costs
// nothing. The gate is the authorization paragraph in the stage's own prompt,
// so this pins exactly which stages carry it. Naming them here is the point:
// a new stage that starts calling a paid API without the gate fails this
// test, and removing the gate from one of these fails it too.
func TestEverySpendingStageCarriesTheApprovalGate(t *testing.T) {
	const gateMarker = "ONLY from the human input for THIS stage"

	wantGated := map[string]bool{
		"longform-characters":          true,
		"longform-visual-development":  true,
		"longform-narration":           true,
		"longform-anchor-shot":         true,
		"longform-next-shot":           true,
		"shortform-characters":         true,
		"shortform-visual-development": true,
		"shortform-narration":          true,
		"shortform-anchor-shot":        true,
		"shortform-next-shot":          true,
	}

	for _, pipeline := range []*Pipeline{longformPipeline, shortformPipeline} {
		for _, stage := range pipeline.Stages {
			hasGate := strings.Contains(stage.Description, gateMarker)
			switch {
			case wantGated[stage.ID] && !hasGate:
				t.Fatalf("%s spends money but its prompt carries no approval gate, so nothing stops it billing the user", stage.ID)
			case !wantGated[stage.ID] && hasGate:
				t.Fatalf("%s carries the approval gate but is not listed as a spending stage; add it to wantGated so the paid surface stays written down in one place", stage.ID)
			}
			delete(wantGated, stage.ID)
		}
	}
	for id := range wantGated {
		t.Fatalf("%s is listed as a spending stage but no longer exists in either pipeline", id)
	}

	// The gate is worthless if it accepts approval granted anywhere else --
	// that is exactly how an approved storyboard becomes an unapproved bill.
	if !strings.Contains(generationSpendGate("x"), "Approval of an earlier stage") {
		t.Fatal("the spend gate no longer rejects approval inherited from an earlier stage")
	}
}

func TestCinematicPipelinesUseDirectCutReviewAndDeterministicAssembly(t *testing.T) {
	for _, pipeline := range []*Pipeline{longformPipeline, shortformPipeline} {
		stages := map[string]PipelineStage{}
		for _, stage := range pipeline.Stages {
			if strings.Contains(stage.ID, "seam-bridge") || strings.Contains(stage.ID, "stitch-plan") {
				t.Fatalf("%s still exposes retired bridge/planning stage %q", pipeline.ID, stage.ID)
			}
			stages[stage.ID] = stage
		}

		seam := stages[pipeline.ID+"-seam-proof"].Description
		for _, required := range []string{"lightweight receipt", "Do not create a third bridge clip", "minimax/h3-max/reference-to-video"} {
			if !strings.Contains(seam, required) {
				t.Fatalf("%s direct-cut review is missing %q", pipeline.ID, required)
			}
		}
		assembly := stages[pipeline.ID+"-assemble"]
		for _, required := range []string{pipeline.ID + "-assembly-manifest.json", "accepted clip ledger", "Resolve every input"} {
			if !strings.Contains(assembly.Description, required) {
				t.Fatalf("%s deterministic assembly is missing %q", pipeline.ID, required)
			}
		}
		if !containsSkill(assembly.Skills, "video-stitching") || !containsSkill(assembly.Skills, "video-editing") {
			t.Fatalf("%s assembly lacks deterministic media skills: %v", pipeline.ID, assembly.Skills)
		}
	}
}

func TestShortformStagesPutDirectionAndMeasuredNarrationBeforeShots(t *testing.T) {
	index := map[string]int{}
	for i, stage := range shortformPipeline.Stages {
		index[stage.ID] = i
	}
	for _, ordered := range [][2]string{
		{"shortform-script", "shortform-characters"},
		{"shortform-characters", "shortform-look-sound"},
		{"shortform-look-sound", "shortform-narration"},
		{"shortform-narration", "shortform-shotlist"},
		{"shortform-shotlist", "shortform-visual-development"},
		{"shortform-visual-development", "shortform-anchor-shot"},
		{"shortform-shotlist", "shortform-anchor-shot"},
		{"shortform-anchor-shot", "shortform-next-shot"},
		{"shortform-next-shot", "shortform-seam-proof"},
		{"shortform-seam-proof", "shortform-assemble"},
		{"shortform-assemble", "shortform-check"},
	} {
		before, after := ordered[0], ordered[1]
		if index[before] >= index[after] {
			t.Fatalf("%s must run before %s", before, after)
		}
	}
	characters := shortformPipeline.Stages[index["shortform-characters"]].Description
	for _, required := range []string{"minimum sufficient approved identity reference", "face-only", "representative complete-scene test", "separately from video cost", "FLUX.2 Max", "human input approves spending on those images", "explicitly approved"} {
		if !strings.Contains(characters, required) {
			t.Fatalf("short-form character step is missing %q", required)
		}
	}
	anchor := shortformPipeline.Stages[index["shortform-anchor-shot"]].Description
	next := shortformPipeline.Stages[index["shortform-next-shot"]].Description
	for _, required := range []string{"exactly one approved anchor clip", "Do not batch the remaining shots", "HyperFrames insert", "user-approved visual style"} {
		if !strings.Contains(anchor, required) {
			t.Fatalf("short-form anchor generation is missing rule %q", required)
		}
	}

	lookSound := shortformPipeline.Stages[index["shortform-look-sound"]].Description
	for _, required := range []string{"Locations and backgrounds", "Wardrobe, props, and continuity", "Lighting and visual palette", "Speech and voices", "Music", "Ambience and sound effects", "Captions", "BEFORE committing the video model", "visible lip-synced dialogue", "off-camera TTS voiceover", "hybrid with native dialogue", "cost", "edit-complexity tradeoff", "explicit choice", "audio-incapable model silently decide", "synchronized native audio", "Separate TTS is for off-camera voiceover", "Never silently turn"} {
		if !strings.Contains(lookSound, required) {
			t.Fatalf("short-form look/sound step is missing %q", required)
		}
	}

	narration := shortformPipeline.Stages[index["shortform-narration"]].Description
	for _, required := range []string{"ffprobe", "REAL measured duration", "Missing, unreadable, silent, or unmeasured", "visible native dialogue", "Off-camera instructional voiceover is not optional"} {
		if !strings.Contains(narration, required) {
			t.Fatalf("short-form narration step is missing %q", required)
		}
	}

	shotlist := shortformPipeline.Stages[index["shortform-shotlist"]].Description
	for _, required := range []string{"shortform-look-sound.md", "shortform-narration.md", "MEASURED", "Never fit narration", "multi-clip-cinematic-generation", "live-verified continuity controls", "seam-generation route", "last usable stable frame", "motivated camera-angle cut"} {
		if !strings.Contains(shotlist, required) {
			t.Fatalf("short-form shot list is missing %q", required)
		}
	}
	for _, required := range []string{"exactly one next clip", "multi-clip-cinematic-generation", "durable history from prior runs", "seam-generation route", "minimax/h3-max/reference-to-video", "reference_video_urls", "last usable stable frame", "orientation/aspect-ratio", "camera-angle change", "cumulatively"} {
		if !strings.Contains(next, required) {
			t.Fatalf("short-form next-shot generation is missing follow-up guidance %q", required)
		}
	}
	visualDevelopment := shortformPipeline.Stages[index["shortform-visual-development"]]
	for _, required := range []string{"minimum sufficient references", "show_reference", "start reference", "exit/end-state reference", "shortform-reference-manifest.json"} {
		if !strings.Contains(visualDevelopment.Description, required) {
			t.Fatalf("short-form visual-development step is missing %q", required)
		}
	}
	for _, id := range []string{"shortform-characters", "shortform-visual-development", "shortform-anchor-shot", "shortform-next-shot"} {
		if !containsSkill(shortformPipeline.Stages[index[id]].Skills, "cinematic-visual-development") {
			t.Fatalf("%s must attach cinematic-visual-development", id)
		}
	}
	seamProof := shortformPipeline.Stages[index["shortform-seam-proof"]]
	for _, required := range []string{"lightweight receipt", "ffprobe", "Do not create a third bridge clip", "minimax/h3-max/reference-to-video", "does not block"} {
		if !strings.Contains(seamProof.Description, required) {
			t.Fatalf("short-form seam-proof step is missing %q", required)
		}
	}
	assemble := shortformPipeline.Stages[index["shortform-assemble"]].Description
	for _, required := range []string{"shortform-assembly-manifest.json", "accepted clip ledger", "video-stitching", "required narration segment", "Cut visuals to the measured narration timeline", "music, ambience, sound effects, and captions", "selected background/look decisions", "native-dialogue source per beat"} {
		if !strings.Contains(assemble, required) {
			t.Fatalf("short-form assembly is missing %q", required)
		}
	}

	check := shortformPipeline.Stages[index["shortform-check"]].Description
	for _, required := range []string{"missing or silent narration is a deterministic failure", "may not be marked not_applicable", "narration_alignment", "same quality-report.json", "Record join evidence"} {
		if !strings.Contains(check, required) {
			t.Fatalf("short-form QA is missing %q", required)
		}
	}
}

func TestCharacterReferencesUseProductPolicyAndRequireSpendApproval(t *testing.T) {
	for _, pipeline := range []*Pipeline{longformPipeline, shortformPipeline} {
		var characters, shotlist string
		for _, stage := range pipeline.Stages {
			switch stage.ID {
			case pipeline.ID + "-characters":
				characters = stage.Description
			case pipeline.ID + "-shotlist":
				shotlist = stage.Description
			}
		}
		for _, required := range []string{
			"Reuse approved source imagery or reviewed local crops",
			"FLUX.2 Max",
			"Do not substitute another model without explicit approval",
			"human input approves spending on those images",
			"show_character",
		} {
			if !strings.Contains(characters, required) {
				t.Fatalf("%s character stage is missing %q", pipeline.ID, required)
			}
		}
		if !strings.Contains(shotlist, "explicitly approves its displayed reference") {
			t.Fatalf("%s shot list can proceed without explicit approval of the displayed character reference", pipeline.ID)
		}
	}
}

// Stage outputs are resolved by filename against prior steps and disambiguated
// by which file exists on disk. That is safe while exactly one pipeline runs,
// but a workshop or partial re-run reuses the run folder, so a stale same-named
// file from a different pipeline can win on array order and be read silently.
// Keeping every context_output distinct across pipelines removes the hazard
// rather than relying on the folder always being clean.
func TestPipelineOutputsDoNotCollideAcrossPipelines(t *testing.T) {
	owner := map[string]string{}
	for _, pipeline := range pipelineRegistry {
		for _, stage := range pipeline.Stages {
			if stage.Output == "" {
				continue
			}
			if prior, seen := owner[stage.Output]; seen {
				t.Fatalf("%q is the context_output of both %s and %s/%s; a re-run can resolve it to the wrong pipeline's file", stage.Output, prior, pipeline.ID, stage.ID)
			}
			owner[stage.Output] = pipeline.ID + "/" + stage.ID
		}
	}
}

// The long-form pipeline encodes an ordering the skills argue for and that is
// expensive to get wrong: narration is generated and MEASURED before the shot
// list exists, because visuals are cut to real audio durations rather than
// estimates. Characters are defined before any shot of them is generated, for
// the same reason in the visual dimension. Reordering these stages silently
// reintroduces the two defects the pipeline exists to prevent.
func TestLongformStagesKeepTheirLoadBearingOrder(t *testing.T) {
	index := map[string]int{}
	for i, stage := range longformPipeline.Stages {
		index[stage.ID] = i
	}

	for _, ordered := range [][2]string{
		{"longform-script", "longform-characters"},
		{"longform-characters", "longform-look-sound"},
		{"longform-look-sound", "longform-narration"},
		{"longform-script", "longform-narration"},
		{"longform-narration", "longform-shotlist"},
		{"longform-shotlist", "longform-visual-development"},
		{"longform-visual-development", "longform-anchor-shot"},
		{"longform-shotlist", "longform-anchor-shot"},
		{"longform-anchor-shot", "longform-next-shot"},
		{"longform-next-shot", "longform-seam-proof"},
		{"longform-seam-proof", "longform-assemble"},
		{"longform-assemble", "longform-check"},
	} {
		before, after := ordered[0], ordered[1]
		if index[before] >= index[after] {
			t.Fatalf("%s must run before %s", before, after)
		}
	}

	shotlist := longformPipeline.Stages[index["longform-shotlist"]].Description
	for _, required := range []string{"MEASURED", "multi-clip-cinematic-generation", "live-verified continuity controls", "seam-generation route", "last usable stable frame", "camera handoff", "reference manifest"} {
		if !strings.Contains(shotlist, required) {
			t.Fatalf("long-form shot list is missing %q", required)
		}
	}
	next := longformPipeline.Stages[index["longform-next-shot"]].Description
	for _, required := range []string{"exactly one next clip", "multi-clip-cinematic-generation", "durable history from prior runs", "recorded seam-generation route", "minimax/h3-max/reference-to-video", "reference_video_urls", "last usable stable frame", "orientation/aspect-ratio", "camera-angle cut", "cumulatively"} {
		if !strings.Contains(next, required) {
			t.Fatalf("long-form next-shot generation is missing follow-up-shot guidance %q", required)
		}
	}
	visualDevelopment := longformPipeline.Stages[index["longform-visual-development"]]
	for _, required := range []string{"minimum sufficient references", "show_reference", "start reference", "exit/end-state reference", "longform-reference-manifest.json"} {
		if !strings.Contains(visualDevelopment.Description, required) {
			t.Fatalf("long-form visual-development step is missing %q", required)
		}
	}
	for _, id := range []string{"longform-characters", "longform-visual-development", "longform-anchor-shot", "longform-next-shot"} {
		if !containsSkill(longformPipeline.Stages[index[id]].Skills, "cinematic-visual-development") {
			t.Fatalf("%s must attach cinematic-visual-development", id)
		}
	}
	seamProof := longformPipeline.Stages[index["longform-seam-proof"]]
	for _, required := range []string{"lightweight receipt", "ffprobe", "Do not create a third bridge clip", "minimax/h3-max/reference-to-video", "does not block"} {
		if !strings.Contains(seamProof.Description, required) {
			t.Fatalf("long-form seam-proof step is missing %q", required)
		}
	}
}

func TestShotCreationUsesAnchorAndReusableNextShotRecipes(t *testing.T) {
	plan := planForAll([]*Pipeline{longformPipeline, shortformPipeline})
	steps := map[string]map[string]interface{}{}
	for _, raw := range plan["steps"].([]map[string]interface{}) {
		steps[raw["id"].(string)] = raw
	}

	for _, pipeline := range []*Pipeline{longformPipeline, shortformPipeline} {
		anchorID := pipeline.ID + "-anchor-shot"
		nextID := pipeline.ID + "-next-shot"
		var anchor, next PipelineStage
		for _, stage := range pipeline.Stages {
			switch stage.ID {
			case anchorID:
				anchor = stage
			case nextID:
				next = stage
			}
		}
		if anchor.ID == "" || next.ID == "" {
			t.Fatalf("%s must expose both anchor and reusable next-shot recipes", pipeline.ID)
		}
		for _, required := range []string{"exactly one approved anchor clip", "Do not batch", "show_video"} {
			if !strings.Contains(anchor.Description, required) {
				t.Fatalf("%s is missing %q", anchorID, required)
			}
		}
		for _, required := range []string{"exactly one next clip", "human input", "durable history from prior runs", "blocked result", "cumulatively", "show_video"} {
			if !strings.Contains(next.Description, required) {
				t.Fatalf("%s is missing %q", nextID, required)
			}
		}
		var shotlist PipelineStage
		for _, stage := range pipeline.Stages {
			if stage.ID == pipeline.ID+"-shotlist" {
				shotlist = stage
			}
		}
		for _, stage := range []PipelineStage{shotlist, anchor, next} {
			if !containsSkill(stage.Skills, "multi-clip-cinematic-generation") {
				t.Fatalf("%s must attach multi-clip-cinematic-generation: %v", stage.ID, stage.Skills)
			}
		}

		deps := map[string]bool{}
		for _, name := range steps[nextID]["context_dependencies"].([]string) {
			deps[name] = true
		}
		for _, required := range append([]string{anchor.Output}, anchor.Artifacts...) {
			if !deps[required] {
				t.Fatalf("%s cannot read anchor evidence %q: %v", nextID, required, deps)
			}
		}
	}
}

// Both cinematic pipelines retain real footage generation while permitting
// HyperFrames only in the planning, creation, assembly, and QA steps where an
// explicitly planned deterministic insert can be made and verified.
func TestProductionPipelinesSupportApprovedAnimatedSequences(t *testing.T) {
	generationSkills := map[string]bool{}
	for _, name := range []string{"fal-ai", "google-ai", "seeddance-api", "longform-cinematic-video", "video-model-selection", "video-cinematography", "video-storytelling"} {
		generationSkills[name] = true
	}

	for _, pipeline := range []*Pipeline{longformPipeline, shortformPipeline} {
		sawGenerationSkill := false
		for _, stage := range pipeline.Stages {
			allowsHyperFrames := strings.HasSuffix(stage.ID, "-shotlist") || strings.HasSuffix(stage.ID, "-anchor-shot") || strings.HasSuffix(stage.ID, "-next-shot") || strings.HasSuffix(stage.ID, "-assemble") || strings.HasSuffix(stage.ID, "-check")
			for _, skill := range stage.Skills {
				if strings.HasPrefix(skill, "hyperframes") && !allowsHyperFrames {
					t.Fatalf("%s/%s attaches %q outside the planned insert lifecycle", pipeline.ID, stage.ID, skill)
				}
				if generationSkills[skill] {
					sawGenerationSkill = true
				}
			}
			if stage.Description == "" {
				t.Fatalf("%s/%s has no description; its agent would run with no instruction", pipeline.ID, stage.ID)
			}
			if stage.Output == "" {
				t.Fatalf("%s/%s declares no output, so nothing downstream can depend on it", pipeline.ID, stage.ID)
			}
		}
		if !sawGenerationSkill {
			t.Fatalf("%s attaches none of the generation skills it exists to run", pipeline.ID)
		}
		var shotlist string
		for _, stage := range pipeline.Stages {
			if stage.ID == pipeline.ID+"-shotlist" {
				shotlist = stage.Description
			}
		}
		for _, marker := range []string{"HyperFrames insert", "approved illustrated or animated sequences", "Real people and realistic characters are not required"} {
			if !strings.Contains(shotlist, marker) {
				t.Fatalf("%s shot list is missing %q", pipeline.ID, marker)
			}
		}
	}
}

func TestLongformPipelineOwnsCinematicContinuityAndSeamEvidence(t *testing.T) {
	requiredSkillStages := map[string]bool{
		"longform-brief":       true,
		"longform-script":      true,
		"longform-shotlist":    true,
		"longform-anchor-shot": true,
		"longform-next-shot":   true,
		"longform-stitch-plan": true,
		"longform-assemble":    true,
		"longform-check":       true,
	}
	requiredArtifacts := map[string][]string{
		"longform-shotlist":    {"longform-sequence-plan.json"},
		"longform-anchor-shot": {"longform-continuity-anchor.json"},
		"longform-next-shot":   {"longform-continuity-ledger.json"},
		"longform-stitch-plan": {"longform-stitch-plan.json"},
		"longform-assemble":    {"longform-final.mp4", "longform-edit-decision-list.json"},
		"longform-check":       {"quality-report.json"},
	}

	for _, stage := range longformPipeline.Stages {
		if requiredSkillStages[stage.ID] {
			attached := false
			for _, skill := range stage.Skills {
				if skill == "longform-cinematic-video" {
					attached = true
					break
				}
			}
			if !attached {
				t.Fatalf("%s must attach longform-cinematic-video: %v", stage.ID, stage.Skills)
			}
		}

		for _, required := range requiredArtifacts[stage.ID] {
			found := false
			for _, artifact := range stage.Artifacts {
				if artifact == required {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s must require %s: %v", stage.ID, required, stage.Artifacts)
			}
		}
	}
}

func containsSkill(skills []string, want string) bool {
	for _, skill := range skills {
		if skill == want {
			return true
		}
	}
	return false
}

// A user with only one provider's key must still be able to finish a whole
// production. Narration is where that nearly broke: the stage used to attach
// only fal-ai and tell the agent to use it for TTS, which left a Google-only
// user stuck halfway through a long-form run with a script and no voiceover.
// Both providers generate speech, so both are attached and the prompt defers
// to whichever the brief committed to.
func TestNarrationIsNotLockedToOneProvider(t *testing.T) {
	var narration PipelineStage
	for _, stage := range longformPipeline.Stages {
		if stage.ID == "longform-narration" {
			narration = stage
		}
	}
	if narration.ID == "" {
		t.Fatal("longform-narration stage is missing")
	}

	attached := map[string]bool{}
	for _, skill := range narration.Skills {
		attached[skill] = true
	}
	for _, provider := range []string{"fal-ai", "google-ai"} {
		if !attached[provider] {
			t.Fatalf("longform-narration does not attach %s, so an agent with only that provider's key cannot generate narration: %v", provider, narration.Skills)
		}
	}
	if strings.Contains(narration.Description, "Use fal-ai for TTS") {
		t.Fatal("longform-narration hard-defaults to fal-ai again, which blocks a Google-only production")
	}
}

// Long-form is the default; visual styles share the same production routes.
func TestLongformIsTheDefaultProductionRoute(t *testing.T) {
	plan := planForAll(pipelineRegistry)
	steps := plan["steps"].([]map[string]interface{})
	if len(steps) == 0 || steps[0]["type"] != "routing" {
		t.Fatalf("plan does not open with a routing step: %+v", steps[0])
	}

	routes := steps[0]["routes"].([]map[string]interface{})
	routed := map[string]string{}
	for _, route := range routes {
		routed[route["route_id"].(string)] = route["next_step_id"].(string)
	}
	for _, want := range []string{"longform", "shortform", "quality"} {
		if routed[want] == "" {
			t.Fatalf("no route reaches the %s pipeline: %+v", want, routed)
		}
	}
	if routed["longform"] != "longform-brief" || routed["shortform"] != "shortform-brief" {
		t.Fatalf("generation routes must enter at their brief stage, got %+v", routed)
	}
	if routed["infographic"] != "" {
		t.Fatalf("product infographic remains exposed as a route: %+v", routed)
	}
	if got := steps[0]["default_route_id"]; got != "longform" {
		t.Fatalf("default route = %v, want longform", got)
	}
	if DefaultPipeline().ID != "longform" {
		t.Fatalf("DefaultPipeline() = %s, want longform", DefaultPipeline().ID)
	}
}
