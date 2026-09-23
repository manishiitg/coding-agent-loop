package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestArchiveRunCostPathsUpdatesOnlyExecutionDisplayPath(t *testing.T) {
	const workspace = "/workspace/Workflow/test"
	const path = "/workspace/Workflow/test/costs/execution/default/2026-08-06.json"
	daily := DailyGroupTokenUsageFile{
		Executions: map[string]*ExecutionTokenUsage{
			"run-A": {RunFolder: "iteration-0/default", TokenUsage: &TokenUsageFile{}},
			"run-B": {RunFolder: "iteration-0/default", TokenUsage: &TokenUsageFile{}},
		},
		// A v1 projection may remain for old readers, but it is not modified or
		// used as identity by the archive operation.
		RunFolders: map[string]*TokenUsageFile{"iteration-0/default": {}},
	}
	encoded, err := json.Marshal(daily)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{path: string(encoded)}
	store := &tokenUsageFileStore{
		workspacePath: workspace,
		readFile: func(_ context.Context, p string) (string, error) {
			if content, ok := files[p]; ok {
				return content, nil
			}
			return "", fmt.Errorf("not found")
		},
		listFiles: func(_ context.Context, p string) ([]string, error) {
			switch p {
			case filepath.Join(workspace, "costs", "execution"):
				return []string{"default"}, nil
			case filepath.Join(workspace, "costs", "execution", "default"):
				return []string{"2026-08-06.json"}, nil
			default:
				return nil, fmt.Errorf("not found")
			}
		},
		writeFile: func(_ context.Context, p, content string) error {
			files[p] = content
			return nil
		},
		warnf: func(string) {},
	}

	if err := store.archiveRunCostPaths(context.Background(), "iteration-0", "iteration-22"); err != nil {
		t.Fatalf("archiveRunCostPaths: %v", err)
	}
	var got DailyGroupTokenUsageFile
	if err := json.Unmarshal([]byte(files[path]), &got); err != nil {
		t.Fatal(err)
	}
	for id, record := range got.Executions {
		if record.RunFolder != "iteration-0/default" {
			t.Fatalf("%s run_folder changed to %q; immutable source path must remain", id, record.RunFolder)
		}
		if record.ArchivedRunFolder != "iteration-22/default" {
			t.Fatalf("%s archived_run_folder = %q", id, record.ArchivedRunFolder)
		}
	}
	if got.RunFolders["iteration-0/default"] == nil {
		t.Fatal("archive changed the legacy v1 projection")
	}
}

func TestReadRunAcrossDatesUsesExecutionKeyedRecordsNotLegacyProjection(t *testing.T) {
	const workspace = "/workspace/Workflow/test"
	const run = "iteration-0/default"
	dir := filepath.Join(workspace, "costs", "execution", "default")
	day := func(date string, tokens int) string {
		encoded, err := json.Marshal(DailyGroupTokenUsageFile{
			Date: date,
			Executions: map[string]*ExecutionTokenUsage{
				"execution-1": {
					RunFolder: run,
					TokenUsage: &TokenUsageFile{ByModel: map[string]*ModelTokenUsage{
						"gpt-6-sol": {InputTokens: tokens},
					}},
				},
			},
			// This is the stale v1 projection from before the schema migration.
			// It must not be added a second time.
			RunFolders: map[string]*TokenUsageFile{run: {ByModel: map[string]*ModelTokenUsage{
				"gpt-6-sol": {InputTokens: 999},
			}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	files := map[string]string{
		filepath.Join(dir, "2026-08-05.json"): day("2026-08-05", 100),
		filepath.Join(dir, "2026-08-06.json"): day("2026-08-06", 250),
	}
	store := &tokenUsageFileStore{
		workspacePath: workspace,
		readFile: func(_ context.Context, p string) (string, error) {
			if content, ok := files[p]; ok {
				return content, nil
			}
			return "", fmt.Errorf("not found")
		},
		listFiles: func(_ context.Context, p string) ([]string, error) {
			if p == dir {
				return []string{"2026-08-05.json", "2026-08-06.json"}, nil
			}
			return nil, fmt.Errorf("not found")
		},
		writeFile:  func(context.Context, string, string) error { return nil },
		deleteFile: func(context.Context, string) error { return nil },
		warnf:      func(string) {},
	}

	got := store.readRunAcrossDates(context.Background(), run)
	if got.ByModel["gpt-6-sol"].InputTokens != 350 {
		t.Fatalf("execution-keyed total = %d, want 350 without stale v1 projection", got.ByModel["gpt-6-sol"].InputTokens)
	}
}
