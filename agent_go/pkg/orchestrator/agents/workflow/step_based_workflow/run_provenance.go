package step_based_workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	runIndexVersion       = 2
	runIndexRelativePath  = "runs/run_index.json"
	planRevisionDirectory = "planning/revisions"
)

type workflowRunIndex struct {
	Version                   int      `json:"version"`
	ActiveIteration           string   `json:"active_iteration"`
	RetainedIterations        []string `json:"retained_iterations"`
	BuilderActiveIteration    string   `json:"builder_active_iteration,omitempty"`
	BuilderRetainedIterations []string `json:"builder_retained_iterations,omitempty"`
	ScheduledIterations       []string `json:"scheduled_iterations,omitempty"`
	WebhookIterations         []string `json:"webhook_iterations,omitempty"`
	SlackIterations           []string `json:"slack_iterations,omitempty"`
	LastTransition            string   `json:"last_transition"`
	FullRunPolicy             string   `json:"full_run_policy"`
	PartialGroupRunPolicy     string   `json:"partial_group_policy"`
	UpdatedAt                 string   `json:"updated_at"`
}

var runIndexWriteLocks sync.Map
var indexedIterationFolderPattern = regexp.MustCompile(`^iteration-([0-9]+)(?:-(hook|sched|slack-[a-f0-9]+))?$`)

func runIndexWriteLock(workspacePath string) *sync.Mutex {
	lock, _ := runIndexWriteLocks.LoadOrStore(filepath.Clean(workspacePath), &sync.Mutex{})
	return lock.(*sync.Mutex)
}

type executablePlanRevision struct {
	RevisionID string                 `json:"revision_id"`
	Files      map[string]interface{} `json:"files"`
}

var executablePlanRevisionFiles = []string{
	"workflow.json",
	"planning/plan.json",
	"planning/step_config.json",
}

func canonicalJSONDocument(raw string) (interface{}, error) {
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func planRevisionForFiles(files map[string]interface{}) (string, []byte, error) {
	payload, err := json.Marshal(files)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("plan-%x", sum[:]), payload, nil
}

// ensureExecutablePlanRevision persists one content-addressed copy of the
// exact executable configuration used by a run. Missing optional files are
// represented as null, so adding or removing one changes the revision.
func (hcpo *StepBasedWorkflowOrchestrator) ensureExecutablePlanRevision(ctx context.Context) (string, error) {
	files := make(map[string]interface{}, len(executablePlanRevisionFiles))
	for _, path := range executablePlanRevisionFiles {
		// Persist the plan this execution actually loaded, even if a builder
		// changed the current file between dispatch and run-metadata creation.
		snapshotPath := "planning/plan.json"
		if snapshot := executionPlanFromContext(ctx); snapshot != nil && path == snapshotPath {
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				return "", fmt.Errorf("encode execution plan snapshot: %w", err)
			}
			value, err := canonicalJSONDocument(string(encoded))
			if err != nil {
				return "", err
			}
			files[path] = value
			continue
		}
		raw, err := hcpo.ReadWorkspaceFile(ctx, path)
		if err != nil {
			if path == "planning/plan.json" {
				return "", fmt.Errorf("read executable plan %s: %w", path, err)
			}
			files[path] = nil
			continue
		}
		value, err := canonicalJSONDocument(raw)
		if err != nil {
			return "", fmt.Errorf("canonicalize executable plan file %s: %w", path, err)
		}
		files[path] = value
	}

	revisionID, _, err := planRevisionForFiles(files)
	if err != nil {
		return "", fmt.Errorf("hash executable plan revision: %w", err)
	}
	revisionPath := filepath.ToSlash(filepath.Join(planRevisionDirectory, revisionID+".json"))
	if existing, readErr := hcpo.ReadWorkspaceFile(ctx, revisionPath); readErr == nil && strings.TrimSpace(existing) != "" {
		var persisted executablePlanRevision
		if json.Unmarshal([]byte(existing), &persisted) == nil && persisted.RevisionID == revisionID {
			return revisionID, nil
		}
		return "", fmt.Errorf("plan revision %s already exists with contradictory content", revisionID)
	}

	if err := createFolderViaAPI(ctx, planRevisionDirectory, hcpo.GetWorkspacePath()); err != nil {
		return "", fmt.Errorf("create plan revision directory: %w", err)
	}
	encoded, err := json.MarshalIndent(executablePlanRevision{RevisionID: revisionID, Files: files}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode executable plan revision: %w", err)
	}
	writeCtx := withPlanningFileMutationWriteAccess(ctx, hcpo.GetWorkspacePath(), filepath.Join("revisions", revisionID+".json"))
	if err := hcpo.WriteWorkspaceFile(writeCtx, revisionPath, string(encoded)); err != nil {
		return "", fmt.Errorf("write executable plan revision: %w", err)
	}
	return revisionID, nil
}

func retainedIterationNames(folders []string) []string {
	all, _, _, _ := categorizedIterationNames(folders)
	return all
}

func categorizedIterationNames(folders []string) (all, builder, scheduled, webhook []string) {
	for _, folder := range folders {
		folder = strings.TrimSpace(folder)
		if folder == "" || folder == currentWorkflowRunFolder {
			continue
		}
		match := indexedIterationFolderPattern.FindStringSubmatch(folder)
		if len(match) == 3 {
			var number int
			_, _ = fmt.Sscanf(match[1], "%d", &number)
			if number <= 0 {
				continue
			}
			all = append(all, folder)
			switch match[2] {
			case "sched":
				scheduled = append(scheduled, folder)
			case "hook":
				webhook = append(webhook, folder)
			default:
				if !strings.HasPrefix(match[2], "slack-") {
					builder = append(builder, folder)
				}
			}
		}
	}
	sortIterations := func(values []string) {
		sort.Slice(values, func(i, j int) bool {
			var left, right int
			_, _ = fmt.Sscanf(values[i], "iteration-%d", &left)
			_, _ = fmt.Sscanf(values[j], "iteration-%d", &right)
			return left < right
		})
	}
	sortIterations(all)
	sortIterations(builder)
	sortIterations(scheduled)
	sortIterations(webhook)
	return all, builder, scheduled, webhook
}

func (hcpo *StepBasedWorkflowOrchestrator) writeRunIndex(ctx context.Context, transition string) error {
	lock := runIndexWriteLock(hcpo.GetWorkspacePath())
	lock.Lock()
	defer lock.Unlock()
	runsPath := filepath.ToSlash(filepath.Join(hcpo.GetWorkspacePath(), "runs"))
	folders, err := hcpo.listRunFolders(ctx, runsPath)
	if err != nil {
		return fmt.Errorf("list run folders for provenance index: %w", err)
	}
	retained, builderRetained, scheduled, webhooks := categorizedIterationNames(folders)
	var slack []string
	for _, folder := range retained {
		if strings.Contains(folder, "-slack-") {
			slack = append(slack, folder)
		}
	}
	index := workflowRunIndex{
		Version:                   runIndexVersion,
		ActiveIteration:           currentWorkflowRunFolder,
		RetainedIterations:        retained,
		BuilderActiveIteration:    currentWorkflowRunFolder,
		BuilderRetainedIterations: builderRetained,
		ScheduledIterations:       scheduled,
		WebhookIterations:         webhooks,
		SlackIterations:           slack,
		LastTransition:            strings.TrimSpace(transition),
		FullRunPolicy:             "rotate_iteration_0_to_next_available_iteration",
		PartialGroupRunPolicy:     "reuse_iteration_0",
		UpdatedAt:                 time.Now().UTC().Format(time.RFC3339Nano),
	}
	encoded, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run provenance index: %w", err)
	}
	if err := hcpo.WriteWorkspaceFile(ctx, runIndexRelativePath, string(encoded)); err != nil {
		return fmt.Errorf("write run provenance index: %w", err)
	}
	return nil
}
