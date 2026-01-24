package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("WORKSPACE_API_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
}

// WorkspaceFile represents a file from the workspace API
type WorkspaceFile struct {
	FilePath    string `json:"filepath"`
	Folder      string `json:"folder,omitempty"`
	Type        string `json:"type"` // "file" or "folder"
	IsDirectory bool   `json:"is_directory,omitempty"`
	Size        int64  `json:"size,omitempty"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	Content     string `json:"content,omitempty"`
	Name        string `json:"-"` // Computed from filepath
	IsDir       bool   `json:"-"` // Computed from type
}

// WorkspaceListResponse represents the response from listing files
type WorkspaceListResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    []WorkspaceFile `json:"data"`
}

// encodePath encodes a file path for use in URLs
func encodePath(path string) string {
	segments := strings.Split(path, "/")
	encodedSegments := make([]string, len(segments))
	for i, segment := range segments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	return strings.Join(encodedSegments, "/")
}

// CreateFolder creates a folder via the workspace API
func CreateFolder(folderPath string) error {
	apiURL := getWorkspaceAPIURL() + "/api/folders"

	requestBody := map[string]string{
		"folder_path": folderPath,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create folder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create folder (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// WriteFile writes a file via the workspace API
func WriteFile(filePath, content string) error {
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodePath(filePath)

	requestBody := map[string]string{
		"content": content,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("PUT", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to write file (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// ReadFile reads a file via the workspace API
func ReadFile(filePath string) (string, error) {
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodePath(filePath)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("file not found: %s", filePath)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to read file (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response - structure is {"success": bool, "data": {"content": "..."}}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract data object
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid response format: missing data field")
	}

	content, ok := data["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid response format: missing content field in data")
	}

	return content, nil
}

// ListFiles lists files in a folder via the workspace API
func ListFiles(folderPath string) ([]WorkspaceFile, error) {
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	if folderPath != "" {
		apiURL += "?folder=" + url.QueryEscape(folderPath)
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list files (status %d): %s", resp.StatusCode, string(body))
	}

	var result WorkspaceListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Process the files to extract name and set IsDir
	files := make([]WorkspaceFile, 0)
	for _, f := range result.Data {
		// Extract name from filepath
		parts := strings.Split(f.FilePath, "/")
		if len(parts) > 0 {
			f.Name = parts[len(parts)-1]
		}
		// Set IsDir based on type
		f.IsDir = f.Type == "folder" || f.IsDirectory

		// Only include direct children of the requested folder
		if f.Folder == folderPath || (folderPath == "" && f.Folder == "") {
			files = append(files, f)
		}
	}

	return files, nil
}

// DeleteFile deletes a file via the workspace API
func DeleteFile(filePath string) error {
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodePath(filePath) + "?confirm=true"

	req, err := http.NewRequest("DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete file (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteFolder deletes a folder via the workspace API
func DeleteFolder(folderPath string) error {
	apiURL := getWorkspaceAPIURL() + "/api/folders/" + encodePath(folderPath) + "?confirm=true"

	req, err := http.NewRequest("DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete folder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete folder (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// FileExists checks if a file exists via the workspace API
func FileExists(filePath string) bool {
	_, err := ReadFile(filePath)
	return err == nil
}
