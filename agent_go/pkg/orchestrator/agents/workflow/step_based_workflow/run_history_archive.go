package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// scheduleRunsFilename is the run-history file the scheduler maintains beside
// the workflow. It is read and rewritten here rather than through the server's
// typed accessor because this package cannot import cmd/server.
const scheduleRunsFilename = "schedule-runs.json"

// scheduleRunStatusesStillOwningIterationZero are the statuses of a run that has
// not finished with the live slot.
//
// A running run is obviously still in it. A capacity-suspended run is the
// subtler case: it is mid-flight and will resume into the SAME folder when the
// provider window reopens (PLAT-101), so repointing its history entry at an
// archived name would send the resume looking for the wrong evidence.
var scheduleRunStatusesStillOwningIterationZero = map[string]bool{
	"running":              true,
	"waiting_for_capacity": true,
}

// ArchiveScheduleRunFolder repoints the run-history entry that owned the live
// iteration-0 slot at the permanent name that slot has just been rotated to.
//
// Every run executes in iteration-0 and is recorded as such, but iteration-0 is
// only the live slot — the NEXT run rotates it to a permanent iteration-N.
// Nothing updated the history entry when that happened, so every historical run
// claimed a folder it no longer occupied. On hetznerssh that left 24 entries all
// reading "iteration-0" while the folders on disk were iteration-21..25, and one
// entry's own error text named iteration-25 while the entry said iteration-0.
//
// The visible damage is in the schedule popup. Cost and token totals are looked
// up per run folder, and rotation already archives cost records
// (ArchiveRunCostPaths) to the new name — correctly. So
// every history row resolved to the one folder still called iteration-0 and
// displayed the CURRENT run's spend, identically, on every row. The rows were
// not stale, they were all reading the same live cell.
//
// Only the most recent terminal entry is repointed. That is the run that
// actually produced this folder; older entries claiming iteration-0 are from
// runs whose folders were rotated and pruned long ago, and their true names are
// not recoverable. Rewriting them would replace a visible wrong answer with an
// invisible one.
func (hcpo *StepBasedWorkflowOrchestrator) ArchiveScheduleRunFolder(ctx context.Context, from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if hcpo == nil || from == "" || to == "" || from == to {
		return nil
	}
	path := fmt.Sprintf("%s/%s", hcpo.GetWorkspacePath(), scheduleRunsFilename)
	content, err := hcpo.ReadWorkspaceFile(ctx, path)
	if err != nil || strings.TrimSpace(content) == "" {
		// No run history is normal: a workflow run from chat rather than from a
		// schedule never creates one.
		return nil
	}

	// Decoded as generic maps on purpose. The typed entry lives in cmd/server
	// and gains fields over time; a struct here would silently drop every field
	// this package does not know about when it writes the file back.
	var runs []map[string]interface{}
	if err := json.Unmarshal([]byte(content), &runs); err != nil {
		return fmt.Errorf("parse %s: %w", scheduleRunsFilename, err)
	}

	newest := scheduleRunIndexOwningFolder(runs, from)
	if newest < 0 {
		return nil
	}
	runs[newest]["run_folder"] = to
	body, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", scheduleRunsFilename, err)
	}
	if err := hcpo.WriteWorkspaceFile(ctx, path, string(body)); err != nil {
		return fmt.Errorf("write %s: %w", scheduleRunsFilename, err)
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📁 Run history: repointed the run that owned %s to %s", from, to))
	return nil
}

// scheduleRunIndexOwningFolder returns the index of the run-history entry that
// actually produced a folder, or -1 when none does.
//
// "Produced it" means: recorded that folder, and has finished with it. A run
// still holding the live slot is skipped — a running run is obviously in it, and
// a capacity-suspended run will resume into the SAME folder when the provider
// window reopens (PLAT-101), so repointing it would send the resume looking for
// evidence under a name it never wrote to.
//
// Only the newest match is returned. Older entries naming the same folder are
// from runs whose folders rotated and were pruned long before; their true names
// are gone, and inventing one would turn a visibly wrong answer into an
// invisible one.
func scheduleRunIndexOwningFolder(runs []map[string]interface{}, folder string) int {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return -1
	}
	newest, newestStart := -1, ""
	for i, run := range runs {
		recorded, _ := run["run_folder"].(string)
		if strings.TrimSpace(recorded) != folder {
			continue
		}
		status, _ := run["status"].(string)
		if scheduleRunStatusesStillOwningIterationZero[strings.TrimSpace(status)] {
			continue
		}
		startedAt, _ := run["started_at"].(string)
		if newest < 0 || startedAt > newestStart {
			newest, newestStart = i, startedAt
		}
	}
	return newest
}
