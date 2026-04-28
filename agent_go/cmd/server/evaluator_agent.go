package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

// =====================================================================
// evaluator_agent.go — auto-spawn the experiment evaluator.
//
// The evaluator is a single-purpose narration step that fires when an
// experiment transitions to status=evaluating. It writes the rationale
// paragraph that explains the system-computed verdict, then commits the
// conclusion (which archives the experiment).
//
// Architecturally distinct from the proposer (the workflow-builder agent
// in optimizer mode) so an LLM can't self-justify its own experiments.
// The evaluator only sees the experiment record; it has no chat history,
// no proposer reasoning, and no other tools.
//
// Implementation: a one-shot LLM call (via virtualtools.GenerateTextOneShot)
// constrained to producing only a rationale string. The verdict was already
// computed deterministically by ComputeVerdict; the evaluator does not get
// to change it. ConcludeExperiment commits the rationale to the record and
// archives the experiment.
// =====================================================================

// SpawnEvaluatorAgent narrates the system-computed verdict for an
// experiment in evaluating status. Async-safe: errors are logged but not
// propagated. The framework state machine moves to "concluded" on success
// or stays at "evaluating" on failure (manual_conclude can take over).
func SpawnEvaluatorAgent(ctx context.Context, workspacePath, experimentID string) {
	go func() {
		bgCtx := context.Background()
		if err := runEvaluator(bgCtx, workspacePath, experimentID); err != nil {
			log.Printf("[EVALUATOR_AGENT] failed for %s / %s: %v", workspacePath, experimentID, err)
		}
	}()
}

func runEvaluator(ctx context.Context, workspacePath, experimentID string) error {
	// 1. Read the experiment record.
	file, exists, err := ReadActiveFile(ctx, workspacePath)
	if err != nil {
		return fmt.Errorf("read active.json: %w", err)
	}
	if !exists || file == nil {
		return fmt.Errorf("active.json missing")
	}
	rec := FindActiveExperiment(file, experimentID)
	if rec == nil {
		return fmt.Errorf("experiment %s not in active.json", experimentID)
	}
	if rec.Status != ExpStatusEvaluating {
		return fmt.Errorf("experiment %s is in status %q, not evaluating — skipping evaluator", experimentID, rec.Status)
	}
	if rec.Conclusion == nil || rec.Conclusion.Verdict == "" {
		return fmt.Errorf("experiment %s has no system-computed verdict yet — should not have spawned evaluator", experimentID)
	}

	// 2. Build the system + user messages. The system prompt is the entire
	// evaluator role definition; the user message is the experiment record.
	systemMessage := evaluatorSystemPrompt
	userMessage, err := buildEvaluatorUserMessage(rec)
	if err != nil {
		return fmt.Errorf("build user message: %w", err)
	}

	// 3. One-shot LLM call. Tier=low because narration is straightforward.
	rationale, err := virtualtools.GenerateTextOneShot(ctx, "low", systemMessage, userMessage)
	if err != nil {
		return fmt.Errorf("LLM call: %w", err)
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" {
		rationale = fmt.Sprintf("(empty rationale from evaluator) verdict=%s; system-computed", rec.Conclusion.Verdict)
	}
	if len(rationale) > 500 {
		rationale = rationale[:497] + "…"
	}

	// 4. Commit via ConcludeExperiment. No override — the evaluator's job
	// is to narrate, not to change the verdict. If the LLM wrote something
	// that disagrees with the verdict, that's caught when an operator reads
	// the rationale; the audit trail still shows the heuristic verdict.
	in := ConcludeExperimentInput{
		ExperimentID: experimentID,
		Rationale:    rationale,
	}
	out, err := ConcludeExperiment(ctx, workspacePath, "evaluator-agent", in)
	if err != nil {
		return fmt.Errorf("ConcludeExperiment: %w", err)
	}
	log.Printf("[EVALUATOR_AGENT] concluded %s: verdict=%s archived=%v", experimentID, out.FinalVerdict, out.Archived)
	return nil
}

// evaluatorSystemPrompt is the entire role definition for the evaluator.
// Intentionally narrow — it only narrates a verdict the system already
// computed.
const evaluatorSystemPrompt = `You are an experiment evaluator for the auto-improvement framework.

Your job: narrate WHY the system-computed verdict on an experiment is honest given the data. The verdict is already decided by a deterministic heuristic (kept / reverted / inconclusive / extend) — you do not change it. You write a short rationale paragraph (≤500 characters) that an operator can read to understand the call.

Rules:
- Stick to the data shown. Do not invent baselines, target values, or causal stories.
- Reference the actual numbers: post-mean vs baseline-mean, magnitude observed vs expected, per-run beat percentage.
- If world drift is flagged, mention it as a confound that downgrades confidence.
- Keep it tight — operator reads this as a quick sanity check, not a research paper.
- Format: 1–3 sentences of plain English. No bullet points, no headings, no markdown.

Example outputs:

verdict=kept:
"Post-mean 0.10 vs baseline 0.19 (≈9pp decrease), expected ≥10pp; close enough. 5/5 post-runs individually beat baseline. Direction matches expected_direction=decrease — kept."

verdict=reverted:
"Post-mean rose to 0.27 from baseline 0.19 — opposite of expected_direction=decrease. 1/5 post-runs beat baseline; magnitude exceeds the noise band. Reverted, intervention rolled back."

verdict=inconclusive:
"Post-mean 0.18 vs baseline 0.19 — within the noise band (1× std). No clear movement; ≥70% beat-rate not met. Inconclusive — neither kept nor reverted; user should decide whether to extend the window or revert."

Do NOT write headings, bullets, or commentary about the framework itself. Just the rationale.`

// buildEvaluatorUserMessage formats the experiment record as the LLM input.
// Includes the pre-registered hypothesis, baseline, measurement values, and
// the verdict + evidence the heuristic computed. Excludes anything the
// proposer wrote about its reasoning (we only have the hypothesis text).
func buildEvaluatorUserMessage(rec *ExperimentRecord) (string, error) {
	type evaluatorPayload struct {
		Hypothesis        string                  `json:"hypothesis"`
		TargetMetrics     []string                `json:"target_metrics"`
		ExpectedDirection ExpectedDirection       `json:"expected_direction"`
		ExpectedMagnitude float64                 `json:"expected_magnitude"`
		Baseline          ExperimentBaseline      `json:"baseline"`
		Measurement       ExperimentMeasurement   `json:"measurement"`
		Conclusion        *ExperimentConclusion   `json:"conclusion"`
		WorldState        ExperimentWorldState    `json:"world_state"`
	}
	payload := evaluatorPayload{
		Hypothesis:        rec.Hypothesis,
		TargetMetrics:     rec.TargetMetrics,
		ExpectedDirection: rec.ExpectedDirection,
		ExpectedMagnitude: rec.ExpectedMagnitude,
		Baseline:          rec.Baseline,
		Measurement:       rec.Measurement,
		Conclusion:        rec.Conclusion,
		WorldState:        rec.WorldState,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"Experiment record (system-computed verdict already populated):\n\n```json\n%s\n```\n\nWrite the rationale paragraph for this verdict.",
		string(body),
	), nil
}
