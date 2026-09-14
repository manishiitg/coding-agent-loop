package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAllocateScheduledRunFolderIsImmutableAndSharesNumericNamespace(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	runs := filepath.Join(docs, "Workflow", "demo", "runs")
	for _, name := range []string{"iteration-4", "iteration-8-hook", "iteration-6-sched"} {
		if err := os.MkdirAll(filepath.Join(runs, name), 0700); err != nil {
			t.Fatal(err)
		}
	}

	first, err := allocateScheduledRunFolder("Workflow/demo", "run-one")
	if err != nil {
		t.Fatal(err)
	}
	if first != "iteration-9-sched" {
		t.Fatalf("first folder = %q, want iteration-9-sched", first)
	}
	again, err := allocateScheduledRunFolder("Workflow/demo", "run-one")
	if err != nil || again != first {
		t.Fatalf("idempotent allocation = (%q, %v), want (%q, nil)", again, err, first)
	}
	second, err := allocateScheduledRunFolder("Workflow/demo", "run-two")
	if err != nil {
		t.Fatal(err)
	}
	if second != "iteration-10-sched" {
		t.Fatalf("second folder = %q, want iteration-10-sched", second)
	}
}

func TestAllocateScheduledRunFolderConcurrentOccurrencesDoNotCollide(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	if err := os.MkdirAll(filepath.Join(docs, "Workflow", "demo"), 0700); err != nil {
		t.Fatal(err)
	}

	const count = 12
	results := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			folder, err := allocateScheduledRunFolder("Workflow/demo", fmt.Sprintf("run-%d", index))
			if err != nil {
				errs <- err
				return
			}
			results <- folder
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for folder := range results {
		if seen[folder] {
			t.Fatalf("duplicate folder allocation: %s", folder)
		}
		seen[folder] = true
	}
	if len(seen) != count {
		t.Fatalf("allocated %d folders, want %d", len(seen), count)
	}
}
