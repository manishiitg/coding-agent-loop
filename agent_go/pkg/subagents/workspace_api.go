package subagents

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// WorkspaceAPIClient provides methods to interact with the workspace API
type WorkspaceAPIClient struct {
	BaseURL string
	Client  *http.Client
}

// NewWorkspaceAPIClient creates a new workspace API client
func NewWorkspaceAPIClient(baseURL string) *WorkspaceAPIClient {
	return &WorkspaceAPIClient{
		BaseURL: baseURL,
		Client:  &http.Client{},
	}
}

// DocumentsResponse represents the response from listing documents
type DocumentsResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    []DocumentEntry `json:"data"`
}

// DocumentEntry represents a file or folder in the workspace
type DocumentEntry struct {
	Filepath     string          `json:"filepath"`
	Type         string          `json:"type"`
	IsBinary     bool            `json:"is_binary,omitempty"`
	Size         int64           `json:"size,omitempty"`
	MimeType     string          `json:"mime_type,omitempty"`
	LastModified string          `json:"last_modified,omitempty"`
	Children     []DocumentEntry `json:"children,omitempty"`
}

// DocumentContentResponse represents the response from reading a document
type DocumentContentResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Content      string `json:"content"`
		IsBinary     bool   `json:"is_binary,omitempty"`
		Size         int64  `json:"size,omitempty"`
		MimeType     string `json:"mime_type,omitempty"`
		LastModified string `json:"last_modified"`
	} `json:"data"`
	Message string `json:"message"`
}

// ListFiles lists files in a folder via workspace API
func (c *WorkspaceAPIClient) ListFiles(folderPath string) ([]DocumentEntry, error) {
	reqURL := fmt.Sprintf("%s/api/documents?folder=%s", c.BaseURL, url.QueryEscape(folderPath))

	resp, err := c.Client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list files: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result DocumentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("API returned error: %s", result.Message)
	}

	// The workspace API wraps the folder listing: data=[{filepath:"subagents", type:"folder", children:[...]}]
	// We need to unwrap and return the children of the requested folder.
	// If the response is a single root entry matching our requested folder, return its children.
	if len(result.Data) == 1 && result.Data[0].Type == "folder" && result.Data[0].Filepath == folderPath {
		return result.Data[0].Children, nil
	}

	// Also handle the case where the root entry's children contain folders with nested children —
	// flatten one level so discovery works with the workspace API's nested response format.
	var flattened []DocumentEntry
	for _, entry := range result.Data {
		if entry.Type == "folder" && len(entry.Children) > 0 && entry.Filepath == folderPath {
			flattened = append(flattened, entry.Children...)
		} else {
			flattened = append(flattened, entry)
		}
	}
	if len(flattened) > 0 {
		return flattened, nil
	}

	return result.Data, nil
}

// ReadFile reads a file's content via workspace API
func (c *WorkspaceAPIClient) ReadFile(filePath string) (string, error) {
	reqURL := fmt.Sprintf("%s/api/documents/%s", c.BaseURL, url.PathEscape(filePath))

	resp, err := c.Client.Get(reqURL)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to read file: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result DocumentContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("API returned error: %s", result.Message)
	}
	if result.Data.IsBinary {
		return "", fmt.Errorf("cannot read binary file as text: %s", filePath)
	}

	return result.Data.Content, nil
}

// WriteFile writes content to a file via workspace API
func (c *WorkspaceAPIClient) WriteFile(filePath, content string) error {
	reqURL := fmt.Sprintf("%s/api/documents/%s", c.BaseURL, url.PathEscape(filePath))

	body := map[string]string{"content": content}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, reqURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to write file: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteFolder deletes a folder via workspace API
func (c *WorkspaceAPIClient) DeleteFolder(folderPath string) error {
	reqURL := fmt.Sprintf("%s/api/folders/%s?confirm=true", c.BaseURL, url.PathEscape(folderPath))

	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete folder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete folder: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CreateFolder creates a folder via workspace API
func (c *WorkspaceAPIClient) CreateFolder(folderPath string) error {
	reqURL := fmt.Sprintf("%s/api/folders", c.BaseURL)

	body := map[string]string{
		"folder_path": folderPath,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, reqURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create folder: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create folder: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
