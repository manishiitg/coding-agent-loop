package step_based_workflow

import (
	"strings"
	"testing"
)

func TestScriptedValidationFailureRetainsDiagnostics(t *testing.T) {
	result := &ScriptedFastPathResult{RanScript: true, Error: "validation failed", Output: "missing nav-login on /intro"}
	d := decideScriptedFastPath(result)
	if !strings.Contains(d.PriorError, result.Error) || !strings.Contains(d.PriorError, result.Output) {
		t.Fatalf("lost validation or output evidence: %q", d.PriorError)
	}
	result.Error += "\n" + result.Output
	d = decideScriptedFastPath(result)
	if strings.Count(d.PriorError, result.Output) != 1 {
		t.Fatalf("duplicated output evidence: %q", d.PriorError)
	}
}

// The scripted-step fallback is the feature: when a step's saved main.py fails,
// the run must NOT fail. It falls back to the LLM carrying the broken script AND
// its error so the model repairs the script instead of rewriting blind. These
// pin that contract — it had no coverage at all, and the three cases are easy to
// transpose (the difference between "failed" and "never ran" decides whether the
// model is shown an error it must fix).
func TestScriptedFastPathDecision(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     *ScriptedFastPathResult
		wantDone   bool
		wantScript string
		wantError  string
	}{
		{
			name:     "script ran and validated: skip the LLM entirely",
			result:   &ScriptedFastPathResult{RanScript: true, Success: true, Output: "done", ExistingScript: "print(1)"},
			wantDone: true,
			// No repair context: nothing to fix, and the LLM never runs.
			wantScript: "",
			wantError:  "",
		},
		{
			name: "script ran and FAILED: hand the LLM the script and the error",
			result: &ScriptedFastPathResult{
				RanScript:      true,
				Success:        false,
				ExistingScript: "print(broken",
				Error:          "SyntaxError: unexpected EOF",
			},
			wantDone:   false,
			wantScript: "print(broken",
			wantError:  "SyntaxError: unexpected EOF",
		},
		{
			name:       "saved script never ran: reuse/update, NOT a failure",
			result:     &ScriptedFastPathResult{RanScript: false, ExistingScript: "print(1)"},
			wantDone:   false,
			wantScript: "print(1)",
			// Must stay empty: presenting a phantom error would make the model
			// "fix" a script that never failed.
			wantError: "",
		},
		{
			name:       "no saved script: LLM writes one from scratch",
			result:     &ScriptedFastPathResult{},
			wantDone:   false,
			wantScript: "",
			wantError:  "",
		},
		{
			name:     "nil result is not a fast path",
			result:   nil,
			wantDone: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decideScriptedFastPath(tc.result)
			if got.FastPathDone != tc.wantDone {
				t.Errorf("FastPathDone = %v, want %v", got.FastPathDone, tc.wantDone)
			}
			if got.PriorScript != tc.wantScript {
				t.Errorf("PriorScript = %q, want %q", got.PriorScript, tc.wantScript)
			}
			if got.PriorError != tc.wantError {
				t.Errorf("PriorError = %q, want %q", got.PriorError, tc.wantError)
			}
		})
	}
}

// A failing script must reach the model as relearn context. IsRelearnMode is
// driven by PriorScript being non-empty, so a fallback that dropped the script
// would silently downgrade the repair turn into a from-scratch rewrite.
func TestScriptedFailureProducesRelearnContext(t *testing.T) {
	decision := decideScriptedFastPath(&ScriptedFastPathResult{
		RanScript:      true,
		Success:        false,
		ExistingScript: "print(broken",
		Error:          "boom",
	})
	if decision.FastPathDone {
		t.Fatal("a failed script must not short-circuit the LLM")
	}
	if decision.PriorScript == "" {
		t.Fatal("relearn requires the prior script; without it the model rewrites from scratch")
	}
	if decision.PriorError == "" {
		t.Fatal("relearn requires the error; without it the model cannot know what to fix")
	}
}
