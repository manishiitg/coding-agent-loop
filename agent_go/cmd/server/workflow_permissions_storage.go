package server

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

// Persists per-user workflow access grants to config/workflow-user-permissions.json.
// File shape: { "user-key": "read|write|owner", ... }. Keys are normalized (lowercase
// trimmed) and matched against UserID, Username, or Email at lookup time. File-backed
// grants override the env-var grants in workflow_permissions.go.

var workflowUserPermissionsMu sync.Mutex

func workflowUserPermissionsFilePath() string {
	return "config/workflow-user-permissions.json"
}

func readWorkflowUserPermissionsFile() (map[string]WorkflowAccessLevel, error) {
	data, exists, err := readFileFromWorkspace(context.Background(), workflowUserPermissionsFilePath())
	if err != nil {
		return nil, err
	}
	if !exists {
		return map[string]WorkflowAccessLevel{}, nil
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil, err
	}
	out := make(map[string]WorkflowAccessLevel, len(raw))
	for k, v := range raw {
		key := normalizeWorkflowPermissionKey(k)
		if key == "" {
			continue
		}
		level, ok := parseWorkflowAccessLevel(v)
		if !ok {
			continue
		}
		out[key] = level
	}
	return out, nil
}

func writeWorkflowUserPermissionsFile(entries map[string]WorkflowAccessLevel) error {
	workflowUserPermissionsMu.Lock()
	defer workflowUserPermissionsMu.Unlock()
	out := make(map[string]string, len(entries))
	for k, v := range entries {
		key := normalizeWorkflowPermissionKey(k)
		if key == "" || v == "" {
			continue
		}
		out[key] = string(v)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return writeFileToWorkspace(context.Background(), workflowUserPermissionsFilePath(), string(data))
}

func upsertWorkflowUserPermission(userKey string, level WorkflowAccessLevel) error {
	entries, err := readWorkflowUserPermissionsFile()
	if err != nil {
		return err
	}
	key := normalizeWorkflowPermissionKey(userKey)
	if key == "" {
		return errInvalidPermissionKey
	}
	entries[key] = level
	return writeWorkflowUserPermissionsFile(entries)
}

func deleteWorkflowUserPermission(userKey string) error {
	entries, err := readWorkflowUserPermissionsFile()
	if err != nil {
		return err
	}
	key := normalizeWorkflowPermissionKey(userKey)
	if key == "" {
		return nil
	}
	if _, ok := entries[key]; !ok {
		return nil
	}
	delete(entries, key)
	return writeWorkflowUserPermissionsFile(entries)
}

type permissionStorageError string

func (e permissionStorageError) Error() string { return string(e) }

const (
	errInvalidPermissionKey   permissionStorageError = "invalid user key"
	errInvalidPermissionLevel permissionStorageError = "invalid access level"
)

// listWorkflowUserPermissions returns the persisted grants in stable order
// (sorted by key) for handler output.
func listWorkflowUserPermissions() ([]workflowUserPermissionEntry, error) {
	entries, err := readWorkflowUserPermissionsFile()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	// Deterministic ordering for UI rendering.
	sortStrings(keys)
	out := make([]workflowUserPermissionEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, workflowUserPermissionEntry{
			UserKey: k,
			Access:  entries[k],
		})
	}
	return out, nil
}

type workflowUserPermissionEntry struct {
	UserKey string              `json:"user_key"`
	Access  WorkflowAccessLevel `json:"workflow_access"`
}

func sortStrings(in []string) {
	// tiny insertion sort — keeps this file dep-free
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && strings.Compare(in[j-1], in[j]) > 0; j-- {
			in[j-1], in[j] = in[j], in[j-1]
		}
	}
}
