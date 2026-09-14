package server

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var scheduledRunFolderPattern = regexp.MustCompile(`^iteration-([0-9]+)-sched$`)
var numberedRunFolderPattern = regexp.MustCompile(`^iteration-([0-9]+)(?:-(?:hook|sched))?$`)

// allocateScheduledRunFolder binds one saved-schedule occurrence to an
// immutable folder. Exclusive mkdir is the cross-process arbiter. The marker
// makes restart/capacity resume idempotent for the same durable run ID.
func allocateScheduledRunFolder(workspace, runID string) (string, error) {
	if strings.TrimSpace(runID) == "" {
		return "", fmt.Errorf("schedule run id is required")
	}
	lock := scheduleRunFileLock(workspace + "/run-folder-allocation")
	lock.Lock()
	defer lock.Unlock()

	root, err := webhookWorkspaceRoot(workspace)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err = root.MkdirAll("runs", 0700); err != nil {
		return "", err
	}
	entries, err := fs.ReadDir(root.FS(), "runs")
	if err != nil {
		return "", err
	}
	n := 0
	if raw, readErr := root.ReadFile(".schedule-sequence"); readErr == nil {
		if previous, parseErr := strconv.Atoi(strings.TrimSpace(string(raw))); parseErr == nil && previous > n {
			n = previous
		}
	}
	for _, entry := range entries {
		if match := scheduledRunFolderPattern.FindStringSubmatch(entry.Name()); len(match) > 0 {
			raw, readErr := root.ReadFile("runs/" + entry.Name() + "/.schedule-run-id")
			if readErr == nil && string(raw) == runID {
				return entry.Name(), nil
			}
		}
		if match := numberedRunFolderPattern.FindStringSubmatch(entry.Name()); len(match) > 0 {
			if value, parseErr := strconv.Atoi(match[1]); parseErr == nil && value > n {
				n = value
			}
		}
	}
	for {
		n++
		folder := fmt.Sprintf("iteration-%d-sched", n)
		err = root.Mkdir("runs/"+folder, 0700)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		marker, openErr := root.OpenFile("runs/"+folder+"/.schedule-run-id", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if openErr != nil {
			return "", openErr
		}
		_, writeErr := marker.WriteString(runID)
		closeErr := marker.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if err := root.WriteFile(".schedule-sequence", []byte(strconv.Itoa(n)), 0600); err != nil {
			return "", err
		}
		return folder, nil
	}
}
