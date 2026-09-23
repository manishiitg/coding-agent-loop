package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
)

// Steps raise concerns as plain `CONCERNS:` lines in their completion summary
// (scripted steps print them to stdout, which becomes the step result). This
// collects the lines written since the previous Pulse started and hands them
// to Pulse as a compact list, so no reviewer has to search run folders. It
// only reads: Pulse decides whether a concern is a real issue, and nothing here
// files a finding.

const (
	stepConcernLookbackWithoutPulse = 7 * 24 * time.Hour
	stepConcernMaxItems             = 50
	stepConcernMaxText              = 400
)

// StepConcern is one distinct concern text from one step.
type StepConcern struct {
	StepID      string   `json:"step_id"`
	Text        string   `json:"text"`
	SeenCount   int      `json:"seen_count"`
	RunFolders  []string `json:"run_folders"`
	LastSeenAt  string   `json:"last_seen_at"`
	SourceFiles []string `json:"source_files"`
}

// StepConcernsView is what Pulse reads.
type StepConcernsView struct {
	Since     string        `json:"since"`
	Concerns  []StepConcern `json:"concerns"`
	Total     int           `json:"total"`
	Truncated bool          `json:"truncated,omitempty"`
	Note      string        `json:"note"`
}

// parseStepConcernLines returns the payloads of `CONCERNS:` lines. A line may
// be wrapped in backticks. Empty payloads and instruction placeholders such as
// "<what happened ...>" are ignored.
func parseStepConcernLines(text string) []string {
	var out []string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimSpace(strings.Trim(line, "`"))
		line = strings.TrimLeft(line, "-* ")
		if len(line) < len("CONCERNS:") || !strings.EqualFold(line[:len("CONCERNS:")], "CONCERNS:") {
			continue
		}
		payload := strings.TrimSpace(line[len("CONCERNS:"):])
		if payload == "" || strings.HasPrefix(payload, "<") || strings.EqualFold(payload, "none") {
			continue
		}
		if len(payload) > stepConcernMaxText {
			payload = payload[:stepConcernMaxText] + "…"
		}
		out = append(out, payload)
	}
	return out
}

type stepConcernSource struct {
	stepID, runFolder, path string
	modTime                 time.Time
	texts                   []string
}

func readStepConcernSource(path string, modTime time.Time) (stepConcernSource, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return stepConcernSource{}, false
	}
	src := stepConcernSource{path: path, modTime: modTime}
	switch filepath.Base(path) {
	case "execution-final-summary.json":
		var summary struct {
			StepID          string `json:"step_id"`
			RunFolder       string `json:"run_folder"`
			ExecutionResult string `json:"execution_result"`
		}
		if json.Unmarshal(raw, &summary) != nil {
			return src, false
		}
		src.stepID, src.runFolder = summary.StepID, summary.RunFolder
		src.texts = []string{summary.ExecutionResult}
	case "session.json":
		var session struct {
			StepID    string `json:"step_id"`
			RunFolder string `json:"run_folder"`
			Entries   []struct {
				Summary string `json:"summary"`
			} `json:"entries"`
		}
		if json.Unmarshal(raw, &session) != nil {
			return src, false
		}
		src.stepID, src.runFolder = session.StepID, session.RunFolder
		for _, entry := range session.Entries {
			src.texts = append(src.texts, entry.Summary)
		}
	default:
		return src, false
	}
	return src, true
}

// collectStepConcerns scans the workflow's retained runs for step summaries
// written after since.
func collectStepConcerns(workspacePath string, since time.Time) StepConcernsView {
	view := StepConcernsView{
		Since:    since.UTC().Format(time.RFC3339),
		Concerns: []StepConcern{},
		Note:     "CONCERNS: lines steps wrote since the previous Pulse started. Leads, not findings: judge each against the evidence, reuse an existing issue for the same root cause, and treat outcome concerns as Goal Work evidence.",
	}
	runsRoot := filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(strings.Trim(workspacePath, "/")), "runs")
	type key struct{ step, text string }
	grouped := map[key]*StepConcern{}
	lastSeen := map[key]time.Time{}
	_ = filepath.WalkDir(runsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Pulse's own run folders are not step evidence.
			if d.Name() == "pulse" && filepath.Dir(path) == runsRoot {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if name != "execution-final-summary.json" && name != "session.json" {
			return nil
		}
		if name == "session.json" && filepath.Base(filepath.Dir(filepath.Dir(path))) != "execution" {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.ModTime().After(since) {
			return nil
		}
		src, ok := readStepConcernSource(path, info.ModTime())
		if !ok {
			return nil
		}
		stepID := strings.TrimSpace(src.stepID)
		if stepID == "" {
			stepID = filepath.Base(filepath.Dir(filepath.Dir(path)))
		}
		rel, _ := filepath.Rel(filepath.Dir(runsRoot), path)
		for _, text := range src.texts {
			for _, concern := range parseStepConcernLines(text) {
				k := key{stepID, concern}
				item := grouped[k]
				if item == nil {
					item = &StepConcern{StepID: stepID, Text: concern, RunFolders: []string{}, SourceFiles: []string{}}
					grouped[k] = item
				}
				item.SeenCount++
				if src.runFolder != "" && !slices.Contains(item.RunFolders, src.runFolder) {
					item.RunFolders = append(item.RunFolders, src.runFolder)
				}
				if !slices.Contains(item.SourceFiles, filepath.ToSlash(rel)) && len(item.SourceFiles) < 3 {
					item.SourceFiles = append(item.SourceFiles, filepath.ToSlash(rel))
				}
				if src.modTime.After(lastSeen[k]) {
					lastSeen[k] = src.modTime
					item.LastSeenAt = src.modTime.UTC().Format(time.RFC3339)
				}
			}
		}
		return nil
	})
	for _, item := range grouped {
		view.Concerns = append(view.Concerns, *item)
	}
	sort.Slice(view.Concerns, func(i, j int) bool {
		if view.Concerns[i].LastSeenAt != view.Concerns[j].LastSeenAt {
			return view.Concerns[i].LastSeenAt > view.Concerns[j].LastSeenAt
		}
		return view.Concerns[i].StepID < view.Concerns[j].StepID
	})
	view.Total = len(view.Concerns)
	if len(view.Concerns) > stepConcernMaxItems {
		view.Concerns = view.Concerns[:stepConcernMaxItems]
		view.Truncated = true
	}
	return view
}

// stepConcernWindowStart is when the previous Pulse started; without one, the
// last week.
func stepConcernWindowStart(ctx context.Context, workspacePath string) time.Time {
	state, err := readPulseScheduleState(ctx, workspacePath)
	if err == nil && state != nil && !state.PreviousStartedAt.IsZero() {
		return state.PreviousStartedAt
	}
	return time.Now().UTC().Add(-stepConcernLookbackWithoutPulse)
}

func readStepConcernsView(ctx context.Context, workspacePath string) (string, error) {
	out, err := json.MarshalIndent(collectStepConcerns(workspacePath, stepConcernWindowStart(ctx, workspacePath)), "", "  ")
	return string(out), err
}
