package models

import "time"

// SyncStatus represents the GitHub sync status
type SyncStatus struct {
	IsConnected      bool         `json:"is_connected"`
	LastSync         time.Time    `json:"last_sync,omitempty"`
	PendingChanges   int          `json:"pending_changes"`
	UnpushedCommits  int          `json:"unpushed_commits"`
	PendingFiles     []string     `json:"pending_files,omitempty"`
	FileStatuses     []FileStatus `json:"file_statuses,omitempty"`
	Conflicts        []Conflict   `json:"conflicts,omitempty"`
	Repository       string       `json:"repository"`
	Branch           string       `json:"branch"`
}

// FileStatus represents the status of a file in Git
type FileStatus struct {
	File   string `json:"file"`
	Status string `json:"status"` // M, A, D, R, C, U, ?, !, etc.
	Staged bool   `json:"staged"` // true if staged, false if unstaged
}

// Conflict represents a merge conflict
type Conflict struct {
	File    string `json:"file"`
	Message string `json:"message"`
	Type    string `json:"type"` // "merge", "push", "pull"
}

// Change represents a file change
type Change struct {
	Filepath      string    `json:"filepath"`
	Operation     string    `json:"operation"` // "create", "update", "delete"
	Content       string    `json:"content,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	ClientID      string    `json:"client_id,omitempty"`
	CommitMessage string    `json:"commit_message"`
}

// SyncRequest represents the request to sync with GitHub
type SyncRequest struct {
	Force         bool   `json:"force"`          // Force sync even with conflicts (not recommended)
	CommitMessage string `json:"commit_message"` // Custom commit message (optional)
	Operation     string `json:"operation"`      // Operation type: "sync", "force_push_local", "force_pull_remote"
}

// SyncStatusRequest represents the request to get sync status
type SyncStatusRequest struct {
	ShowPending   bool `form:"show_pending,default=true"`
	ShowConflicts bool `form:"show_conflicts,default=true"`
}

// SyncResponse represents the response from a sync operation
type SyncResponse struct {
	Status          string    `json:"status"`
	Operation       string    `json:"operation"`
	CommitMessage   string    `json:"commit_message"`
	Repository      string    `json:"repository"`
	Branch          string    `json:"branch"`
	Timestamp       time.Time `json:"timestamp"`
	Message         string    `json:"message,omitempty"`
	ConflictOptions []string  `json:"conflict_resolution_options,omitempty"`
}
