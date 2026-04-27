package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

// =====================================================================
// experiment_runtime.go — state machine + persistence for the experiment
// loop. Owns: experiments/active.json, experiments/history.jsonl,
// experiments/config.json, experiments/diffs/<id>.patch.
//
// LLM-callable proposers/concluders live in:
//   experiment_propose.go   — propose_experiment, propose_metric
//   experiment_conclude.go  — conclude_experiment
//   experiment_verdict.go   — compute_verdict + record_measurement hook
//
// Schemas: schemas/auto-improvement.schema.json
// =====================================================================

func experimentsDir(workspacePath string) string {
	return path.Join(strings.Trim(workspacePath, "/"), "experiments")
}

func experimentsActivePath(workspacePath string) string {
	return path.Join(experimentsDir(workspacePath), "active.json")
}

func experimentsHistoryPath(workspacePath string) string {
	return path.Join(experimentsDir(workspacePath), "history.jsonl")
}

func experimentsConfigPath(workspacePath string) string {
	return path.Join(experimentsDir(workspacePath), "config.json")
}

func experimentsDiffsDir(workspacePath string) string {
	return path.Join(experimentsDir(workspacePath), "diffs")
}

func experimentDiffPath(workspacePath, experimentID string) string {
	return path.Join(experimentsDiffsDir(workspacePath), experimentID+".patch")
}

// ----------------------------------------------------------------------
// experiments/active.json
// ----------------------------------------------------------------------

// ReadActiveFile loads experiments/active.json. (nil, false, nil) if absent.
func ReadActiveFile(ctx context.Context, workspacePath string) (*ExperimentsActiveFile, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, experimentsActivePath(workspacePath))
	if err != nil {
		return nil, false, err
	}
	if !exists || strings.TrimSpace(content) == "" {
		return nil, false, nil
	}
	var file ExperimentsActiveFile
	if err := json.Unmarshal([]byte(content), &file); err != nil {
		return nil, true, fmt.Errorf("parse experiments/active.json: %w", err)
	}
	if file.Experiments == nil {
		file.Experiments = []ExperimentRecord{}
	}
	return &file, true, nil
}

// WriteActiveFile persists experiments/active.json atomically.
func WriteActiveFile(ctx context.Context, workspacePath string, file *ExperimentsActiveFile) error {
	if file == nil {
		file = &ExperimentsActiveFile{Experiments: []ExperimentRecord{}}
	}
	if file.Experiments == nil {
		file.Experiments = []ExperimentRecord{}
	}
	file.UpdatedAt = nowUTC()
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal active.json: %w", err)
	}
	return writeFileToWorkspace(ctx, experimentsActivePath(workspacePath), string(body))
}

// ReadActiveExperiments returns just the in-flight experiments.
func ReadActiveExperiments(ctx context.Context, workspacePath string) ([]ExperimentRecord, error) {
	file, exists, err := ReadActiveFile(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	if !exists || file == nil {
		return []ExperimentRecord{}, nil
	}
	return file.Experiments, nil
}

// FindActiveExperiment returns a pointer to the in-flight experiment with the
// given id, or nil. Returned pointer is into the file's slice; mutations
// require WriteActiveFile to persist.
func FindActiveExperiment(file *ExperimentsActiveFile, id string) *ExperimentRecord {
	if file == nil {
		return nil
	}
	for i := range file.Experiments {
		if file.Experiments[i].ID == id {
			return &file.Experiments[i]
		}
	}
	return nil
}

// UpsertActiveExperiment inserts or replaces an experiment in active.json
// based on id. Persists atomically.
func UpsertActiveExperiment(ctx context.Context, workspacePath string, rec ExperimentRecord) error {
	file, _, err := ReadActiveFile(ctx, workspacePath)
	if err != nil {
		return err
	}
	if file == nil {
		file = &ExperimentsActiveFile{Experiments: []ExperimentRecord{}}
	}
	replaced := false
	for i := range file.Experiments {
		if file.Experiments[i].ID == rec.ID {
			file.Experiments[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		file.Experiments = append(file.Experiments, rec)
	}
	return WriteActiveFile(ctx, workspacePath, file)
}

// RemoveActiveExperiment removes the experiment with the given id from
// active.json. No-op if absent.
func RemoveActiveExperiment(ctx context.Context, workspacePath, experimentID string) error {
	file, exists, err := ReadActiveFile(ctx, workspacePath)
	if err != nil {
		return err
	}
	if !exists || file == nil {
		return nil
	}
	out := make([]ExperimentRecord, 0, len(file.Experiments))
	for _, e := range file.Experiments {
		if e.ID == experimentID {
			continue
		}
		out = append(out, e)
	}
	file.Experiments = out
	return WriteActiveFile(ctx, workspacePath, file)
}

// ----------------------------------------------------------------------
// experiments/history.jsonl
// ----------------------------------------------------------------------

// AppendHistoryRecord appends a concluded experiment to history.jsonl.
func AppendHistoryRecord(ctx context.Context, workspacePath string, rec ExperimentRecord) error {
	_, err := appendJSONLRecord(ctx, experimentsHistoryPath(workspacePath), rec)
	return err
}

// ReadHistoryExperiments returns all concluded experiments.
func ReadHistoryExperiments(ctx context.Context, workspacePath string) ([]ExperimentRecord, error) {
	return readJSONLRecords[ExperimentRecord](ctx, experimentsHistoryPath(workspacePath))
}

// ----------------------------------------------------------------------
// experiments/config.json
// ----------------------------------------------------------------------

// ReadExperimentsConfig loads experiments/config.json, falling back to
// DefaultExperimentsConfig() when the file is missing or unparseable.
func ReadExperimentsConfig(ctx context.Context, workspacePath string) (ExperimentsConfig, error) {
	def := DefaultExperimentsConfig()
	content, exists, err := readFileFromWorkspace(ctx, experimentsConfigPath(workspacePath))
	if err != nil {
		return def, err
	}
	if !exists || strings.TrimSpace(content) == "" {
		return def, nil
	}
	var cfg ExperimentsConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return def, fmt.Errorf("parse experiments/config.json: %w", err)
	}
	mergeConfigDefaults(&cfg, def)
	return cfg, nil
}

func mergeConfigDefaults(cfg *ExperimentsConfig, def ExperimentsConfig) {
	if cfg.DefaultMeasurementRuns == 0 {
		cfg.DefaultMeasurementRuns = def.DefaultMeasurementRuns
	}
	if cfg.MinRuns == 0 {
		cfg.MinRuns = def.MinRuns
	}
	if cfg.MaxRuns == 0 {
		cfg.MaxRuns = def.MaxRuns
	}
	if cfg.BaselineWindow == 0 {
		cfg.BaselineWindow = def.BaselineWindow
	}
	if len(cfg.AllowedInterventionPaths) == 0 {
		cfg.AllowedInterventionPaths = def.AllowedInterventionPaths
	}
	if len(cfg.ForbiddenInterventionPaths) == 0 {
		cfg.ForbiddenInterventionPaths = def.ForbiddenInterventionPaths
	}
	if cfg.VerdictThresholds == nil {
		cfg.VerdictThresholds = def.VerdictThresholds
	} else {
		t := cfg.VerdictThresholds
		d := def.VerdictThresholds
		if t.KeptMagnitudePct == 0 {
			t.KeptMagnitudePct = d.KeptMagnitudePct
		}
		if t.KeptPerRunBeatPct == 0 {
			t.KeptPerRunBeatPct = d.KeptPerRunBeatPct
		}
		if t.RevertedPerRunBeatPct == 0 {
			t.RevertedPerRunBeatPct = d.RevertedPerRunBeatPct
		}
		if t.NoiseBandStdMultiplier == 0 {
			t.NoiseBandStdMultiplier = d.NoiseBandStdMultiplier
		}
	}
	if cfg.MaxConcurrentExperiments == 0 {
		cfg.MaxConcurrentExperiments = def.MaxConcurrentExperiments
	}
}

// EnsureExperimentsConfig writes the default config to disk if no config
// exists yet. Used when bootstrapping a workflow's experiments/ dir.
func EnsureExperimentsConfig(ctx context.Context, workspacePath string) error {
	_, exists, err := readFileFromWorkspace(ctx, experimentsConfigPath(workspacePath))
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	body, err := json.MarshalIndent(DefaultExperimentsConfig(), "", "  ")
	if err != nil {
		return err
	}
	return writeFileToWorkspace(ctx, experimentsConfigPath(workspacePath), string(body))
}

// ----------------------------------------------------------------------
// id allocator
// ----------------------------------------------------------------------

// nextExperimentID generates a new experiment id. Format:
//   exp-<workflowSlug>-<YYYYMMDD>-<sequence>
func nextExperimentID(ctx context.Context, workspacePath string) (string, error) {
	slug := workflowSlugFromPath(workspacePath)
	today := time.Now().UTC().Format("20060102")
	prefix := fmt.Sprintf("exp-%s-%s-", slug, today)

	maxSeq := 0
	scan := func(candidates []ExperimentRecord) {
		for _, e := range candidates {
			if !strings.HasPrefix(e.ID, prefix) {
				continue
			}
			tail := e.ID[len(prefix):]
			seq := 0
			for _, r := range tail {
				if r < '0' || r > '9' {
					seq = 0
					break
				}
				seq = seq*10 + int(r-'0')
			}
			if seq > maxSeq {
				maxSeq = seq
			}
		}
	}

	active, err := ReadActiveExperiments(ctx, workspacePath)
	if err != nil {
		return "", err
	}
	scan(active)
	hist, err := ReadHistoryExperiments(ctx, workspacePath)
	if err != nil {
		return "", err
	}
	scan(hist)

	return fmt.Sprintf("%s%03d", prefix, maxSeq+1), nil
}
