package models

import "time"

// Document represents a markdown document
type Document struct {
	FilePath    string     `json:"filepath"`
	Content     string     `json:"content,omitempty"` // Only included when reading specific files
	Folder      string     `json:"folder,omitempty"`
	Type        string     `json:"type,omitempty"`         // "file" or "folder"
	Children    []Document `json:"children,omitempty"`     // For hierarchical structure
	IsImage     bool       `json:"is_image,omitempty"`     // Whether file is an image
	Size        int64      `json:"size,omitempty"`         // File size in bytes
	ModifiedAt  time.Time  `json:"modified_at,omitempty"`  // File modification time
	IsDirectory bool       `json:"is_directory,omitempty"` // Whether this is a directory
}

// CreateDocumentRequest represents the request to create a document
type CreateDocumentRequest struct {
	FilePath      string `json:"filepath" binding:"required"`
	Content       string `json:"content" binding:"required"`
	CommitMessage string `json:"commit_message,omitempty"`
}

// UpdateDocumentRequest represents the request to update a document
type UpdateDocumentRequest struct {
	Content       string `json:"content" binding:"required"`
	CommitMessage string `json:"commit_message,omitempty"`
}

// APIResponse represents a standard API response
type APIResponse[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    T      `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

// CreateDocumentResponse represents the response after creating a document
type CreateDocumentResponse struct {
	Document  Document          `json:"document"`
	Structure MarkdownStructure `json:"structure"`
}

// ListDocumentsRequest represents the request to list documents
type ListDocumentsRequest struct {
	Folder   string `form:"folder"`               // Base directory
	MaxDepth int    `form:"max_depth,default=-1"` // Max directory depth (-1 = unlimited)
}

// DeleteDocumentRequest represents the request to delete a document
type DeleteDocumentRequest struct {
	Confirm       bool   `form:"confirm"`
	CommitMessage string `form:"commit_message"`
}

// MoveDocumentRequest represents the request to move a document
type MoveDocumentRequest struct {
	DestinationPath string `json:"destination_path" binding:"required"`
	CommitMessage   string `json:"commit_message,omitempty"`
}

// PatchDocumentRequest represents the request to patch a document
type PatchDocumentRequest struct {
	TargetType     string `json:"target_type" binding:"required"`
	TargetSelector string `json:"target_selector" binding:"required"`
	Operation      string `json:"operation" binding:"required"`
	Content        string `json:"content" binding:"required"`
	CommitMessage  string `json:"commit_message,omitempty"`
}

// DiffPatchRequest represents the request to apply a diff patch
type DiffPatchRequest struct {
	Diff          string `json:"diff" binding:"required"`
	CommitMessage string `json:"commit_message,omitempty"`
}

// DiffPatchResponse represents the response after applying a diff patch
type DiffPatchResponse struct {
	Applied      bool                   `json:"applied"`
	Suggestions  []string               `json:"suggestions,omitempty"`
	ErrorDetails map[string]interface{} `json:"error_details,omitempty"`
}

// FileVersion represents a version of a file
type FileVersion struct {
	CommitHash    string    `json:"commit_hash"`
	CommitMessage string    `json:"commit_message"`
	Author        string    `json:"author"`
	Date          time.Time `json:"date"`
	Content       string    `json:"content,omitempty"`
	Diff          string    `json:"diff,omitempty"`
}

// FileVersionHistoryRequest represents the request to get file version history
type FileVersionHistoryRequest struct {
	Limit int `form:"limit,default=10"`
}

// RestoreVersionRequest represents the request to restore a file version
type RestoreVersionRequest struct {
	CommitHash    string `json:"commit_hash" binding:"required"`
	CommitMessage string `json:"commit_message,omitempty"`
}

// FileUploadRequest represents the request to upload a file
type FileUploadRequest struct {
	FolderPath    string `form:"folder_path" binding:"required"`
	CommitMessage string `form:"commit_message,omitempty"`
}

// FileUploadResponse represents the response after file upload
type FileUploadResponse struct {
	FilePath    string `json:"filepath"`
	FileName    string `json:"filename"`
	FileSize    int64  `json:"file_size"`
	ContentType string `json:"content_type"`
	Folder      string `json:"folder"`
}

// CreateFolderRequest represents the request to create a folder
type CreateFolderRequest struct {
	FolderPath    string `json:"folder_path" binding:"required"`
	CommitMessage string `json:"commit_message,omitempty"`
}

// CreateFolderResponse represents the response after folder creation
type CreateFolderResponse struct {
	FolderPath string `json:"folder_path"`
	Created    bool   `json:"created"`
}
