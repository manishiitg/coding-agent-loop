package handlers

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workspace/models"
	"workspace/parsers"
	"workspace/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// Global lock manager
var lockManager = utils.NewLockManager()

// isImageFile checks if the file is an image
func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	imageExts := []string{".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg", ".ico"}
	for _, imgExt := range imageExts {
		if ext == imgExt {
			return true
		}
	}
	return false
}

// getImageMimeType returns the MIME type for image files
func getImageMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	default:
		return "image/png"
	}
}

// formatImageContent returns base64 encoded image data
func formatImageContent(filename string, content []byte) string {
	mimeType := getImageMimeType(filename)
	base64Data := base64.StdEncoding.EncodeToString(content)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
}

// CreateDocument handles POST /api/documents
func CreateDocument(c *gin.Context) {
	var req models.CreateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	// Sanitize input filepath to ensure it's relative
	docsDir := viper.GetString("docs-dir")
	req.FilePath = utils.SanitizeInputPath(req.FilePath, docsDir)

	// Validate filepath
	if err := validateFilepath(req.FilePath); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid filepath",
			Error:   err.Error(),
		})
		return
	}

	// Create full file path
	fullPath := filepath.Join(docsDir, req.FilePath)

	// Acquire file lock
	lock, err := lockManager.AcquireLock(fullPath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "File is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(lock)

	// Create directory if it doesn't exist
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create directory",
			Error:   err.Error(),
		})
		return
	}

	// Check if file already exists
	if _, err := os.Stat(fullPath); err == nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Document already exists",
			Error:   "File already exists: " + req.FilePath,
		})
		return
	}

	// Write file
	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create document",
			Error:   err.Error(),
		})
		return
	}

	// File created successfully

	// Queue file for semantic processing
	if fileProcessor := GetFileProcessor(); fileProcessor != nil {
		go fileProcessor.QueueJob(req.FilePath, req.Content, "create")
	}

	// Parse markdown structure
	structure := parsers.ParseMarkdown(req.Content)

	// Extract folder from filepath
	folder := filepath.Dir(req.FilePath)
	if folder == "." {
		folder = ""
	}

	// Convert full path to relative path for API response
	relativePath, err := utils.GetRelativePath(fullPath, docsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to convert file path",
			Error:   err.Error(),
		})
		return
	}

	// Get file info for metadata
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to get file info",
			Error:   err.Error(),
		})
		return
	}

	// Create response
	doc := models.Document{
		FilePath:    relativePath,
		Folder:      folder,
		Size:        fileInfo.Size(),
		ModifiedAt:  fileInfo.ModTime(),
		IsDirectory: fileInfo.IsDir(),
	}

	// Handle git operations if commit message provided
	if req.CommitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", req.CommitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	responseData := models.CreateDocumentResponse{
		Document:  doc,
		Structure: *structure,
	}

	c.JSON(http.StatusCreated, models.APIResponse[models.CreateDocumentResponse]{
		Success: true,
		Message: "Document created successfully",
		Data:    responseData,
	})
}

// getAllDocumentsRecursively recursively reads all files and folders from a directory
func getAllDocumentsRecursively(searchPath, docsDir string, maxDepth int) ([]models.Document, error) {
	var documents []models.Document

	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory entirely
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		// Skip the root search path itself only if it's not a folder we want to include
		if path == searchPath {
			// If the search path is a directory and we're querying a specific folder,
			// we need to include it in the results
			if info.IsDir() && searchPath != docsDir {
				// This is a specific folder query - include the folder itself
				relPathFromDocs, err := filepath.Rel(docsDir, path)
				if err != nil {
					return nil
				}

				folder := ""
				if dir := filepath.Dir(relPathFromDocs); dir != "." {
					folder = dir
				}

				doc := models.Document{
					FilePath:    relPathFromDocs,
					Folder:      folder,
					Type:        "folder", // Mark as folder
					IsDirectory: true,
					ModifiedAt:  info.ModTime(),
				}
				documents = append(documents, doc)
			}
			return nil
		}

		// Calculate current depth relative to search path
		relPath, err := filepath.Rel(searchPath, path)
		if err != nil {
			return nil // Skip files/folders that can't be relativized
		}

		// Calculate depth by counting path separators
		currentDepth := 0
		if relPath != "." {
			currentDepth = strings.Count(relPath, string(filepath.Separator))
		}

		// Skip if max depth exceeded
		if maxDepth >= 0 && currentDepth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir // Skip this directory and its contents
			}
			return nil // Skip this file
		}

		// Determine folder relative to docs directory
		relPathFromDocs, err := filepath.Rel(docsDir, path)
		if err != nil {
			return nil // Skip files/folders that can't be relativized
		}

		folder := ""
		if dir := filepath.Dir(relPathFromDocs); dir != "." {
			folder = dir
		}

		if info.IsDir() {
			// This is a folder - add it to the list
			doc := models.Document{
				FilePath:    relPathFromDocs,
				Folder:      folder,
				Type:        "folder", // Mark as folder
				IsDirectory: true,
				ModifiedAt:  info.ModTime(),
			}
			documents = append(documents, doc)
		} else {
			// This is a file - include all files regardless of type
			// For listing, we don't read file content to improve performance
			// Content is only read when using read_workspace_file tool

			// Check if it's an image
			isImage := isImageFile(info.Name())

			doc := models.Document{
				FilePath:    relPathFromDocs,
				Folder:      folder,
				Type:        "file", // Mark as file
				IsImage:     isImage,
				Size:        info.Size(),
				ModifiedAt:  info.ModTime(),
				IsDirectory: false,
			}
			documents = append(documents, doc)
		}

		return nil
	})
	return documents, err
}

// buildHierarchicalStructure builds a tree structure from flat document list
func buildHierarchicalStructure(documents []models.Document, queryFolder string) []models.Document {
	// Create a map to store folders by their path - using pointers
	folderMap := make(map[string]*models.Document)
	var rootItems []*models.Document // Use pointers for root items too

	// First pass: create all folder nodes and store pointers
	for i := range documents {
		doc := &documents[i]
		if doc.Type == "folder" {
			folderMap[doc.FilePath] = doc
		}
	}

	// Second pass: organize files and folders into hierarchy
	for i := range documents {
		doc := &documents[i]

		if doc.Type == "folder" {
			// This is a folder - add to root if it's a top-level folder, the queried folder itself, or a direct child of the queried folder
			if doc.Folder == "" || (queryFolder != "" && (doc.FilePath == queryFolder || doc.Folder == queryFolder)) {
				// Add the folder pointer to root items
				rootItems = append(rootItems, folderMap[doc.FilePath])
			}
			// Note: We don't add subfolders to their parents here because
			// we need to process all files first to populate the folders
		} else {
			// This is a file - add to its parent folder
			if doc.Folder == "" || (queryFolder != "" && doc.Folder == queryFolder) {
				// File is in root or in the queried folder - create a copy for root items
				fileCopy := *doc
				rootItems = append(rootItems, &fileCopy)
			} else {
				// File is in a folder - add to parent folder
				if parent, exists := folderMap[doc.Folder]; exists {
					if parent.Children == nil {
						parent.Children = []models.Document{}
					}
					parent.Children = append(parent.Children, *doc)
				}
			}
		}
	}

	// Third pass: add subfolders to their parents (process in reverse order for deep nesting)
	for i := len(documents) - 1; i >= 0; i-- {
		doc := &documents[i]
		if doc.Type == "folder" && doc.Folder != "" {
			// This is a subfolder - add to its parent
			if parent, exists := folderMap[doc.Folder]; exists {
				if parent.Children == nil {
					parent.Children = []models.Document{}
				}
				// Add a reference to the folder from folderMap
				parent.Children = append(parent.Children, *folderMap[doc.FilePath])
			}
		}
	}

	// Convert root items from pointers to values for return
	var result []models.Document
	for _, item := range rootItems {
		result = append(result, *item)
	}

	// Sort the root items (folders first, then files)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Type == result[j].Type {
			return result[i].FilePath < result[j].FilePath
		}
		return result[i].Type == "folder"
	})

	return result
}

// normalizeFolderPath removes trailing slashes from folder path (except for root "/")
func normalizeFolderPath(folder string) string {
	if folder == "" || folder == "/" {
		return folder
	}
	return strings.TrimRight(folder, "/")
}

// ListDocuments handles GET /api/documents
func ListDocuments(c *gin.Context) {
	var req models.ListDocumentsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid query parameters",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Normalize folder path by removing trailing slashes
	// This fixes issues where trailing slashes cause comparison failures in buildHierarchicalStructure
	normalizedFolder := normalizeFolderPath(req.Folder)

	// Build search path
	var searchPath string
	// Special handling for Downloads folder - use Downloads folder relative to docs-dir
	if normalizedFolder == "Downloads" || strings.HasPrefix(normalizedFolder, "Downloads/") {
		// Use Downloads folder relative to docs-dir (e.g., /app/workspace-docs/Downloads)
		if normalizedFolder == "Downloads" {
			searchPath = filepath.Join(docsDir, "Downloads")
		} else {
			// Handle Downloads/subfolder case
			subfolder := strings.TrimPrefix(normalizedFolder, "Downloads/")
			searchPath = filepath.Join(docsDir, "Downloads", subfolder)
		}
	} else if normalizedFolder != "" {
		searchPath = filepath.Join(docsDir, normalizedFolder)
	} else {
		searchPath = docsDir
	}

	// Debug logging for Downloads folder access
	if normalizedFolder == "Downloads" || strings.HasPrefix(normalizedFolder, "Downloads/") {
		log.Printf("[DEBUG Downloads] docsDir=%s, normalizedFolder=%s, searchPath=%s", docsDir, normalizedFolder, searchPath)
		// Check if searchPath exists
		if info, err := os.Stat(searchPath); err == nil {
			log.Printf("[DEBUG Downloads] Path exists: %s (isDir=%v)", searchPath, info.IsDir())
		} else {
			log.Printf("[DEBUG Downloads] Path does not exist: %s (error: %v)", searchPath, err)
		}
	}

	// Check if the folder exists before attempting to read it
	if _, err := os.Stat(searchPath); os.IsNotExist(err) {
		// Folder doesn't exist - return 200 with empty list and message
		c.JSON(http.StatusOK, models.APIResponse[[]models.Document]{
			Success: true,
			Message: "Folder does not exist",
			Data:    []models.Document{},
			Error:   fmt.Sprintf("Folder not found: %s", normalizedFolder),
		})
		return
	}

	// Use recursive function to get all documents with max depth
	documents, err := getAllDocumentsRecursively(searchPath, docsDir, req.MaxDepth)
	if err != nil {
		// Check if error is due to directory not existing
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, models.APIResponse[[]models.Document]{
				Success: true,
				Message: "Folder does not exist",
				Data:    []models.Document{},
				Error:   fmt.Sprintf("Folder not found: %s", normalizedFolder),
			})
			return
		}
		// For other errors, return 500
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to read documents directory",
			Error:   err.Error(),
		})
		return
	}

	// Build hierarchical structure from flat list using normalized folder path
	hierarchicalDocuments := buildHierarchicalStructure(documents, normalizedFolder)

	c.JSON(http.StatusOK, models.APIResponse[[]models.Document]{
		Success: true,
		Message: "Documents retrieved successfully",
		Data:    hierarchicalDocuments,
	})
}

// GlobDocuments handles GET /api/documents/glob
// Discovers files matching a glob pattern (supports ** for recursive matching)
func GlobDocuments(c *gin.Context) {
	var req models.GlobDocumentsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid query parameters",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Normalize folder path
	normalizedFolder := normalizeFolderPath(req.Folder)

	// Build search path
	var searchPath string
	if normalizedFolder == "Downloads" || strings.HasPrefix(normalizedFolder, "Downloads/") {
		if normalizedFolder == "Downloads" {
			searchPath = filepath.Join(docsDir, "Downloads")
		} else {
			subfolder := strings.TrimPrefix(normalizedFolder, "Downloads/")
			searchPath = filepath.Join(docsDir, "Downloads", subfolder)
		}
	} else if normalizedFolder != "" {
		searchPath = filepath.Join(docsDir, normalizedFolder)
	} else {
		searchPath = docsDir
	}

	// Check if the folder exists
	if _, err := os.Stat(searchPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse[[]models.Document]{
			Success: false,
			Message: "Folder not found",
			Error:   fmt.Sprintf("Folder does not exist: %s", normalizedFolder),
			Data:    []models.Document{},
		})
		return
	}

	// Set max depth (default: unlimited if -1)
	maxDepth := req.MaxDepth
	if maxDepth < 0 {
		maxDepth = -1 // Unlimited
	}

	// Get all documents recursively
	allDocuments, err := getAllDocumentsRecursively(searchPath, docsDir, maxDepth)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to read documents directory",
			Error:   err.Error(),
		})
		return
	}

	// Match files against glob pattern
	matchedDocuments := matchGlobPattern(allDocuments, req.Pattern, req.IncludeDirs, docsDir)

	c.JSON(http.StatusOK, models.APIResponse[[]models.Document]{
		Success: true,
		Message: fmt.Sprintf("Found %d files matching pattern '%s'", len(matchedDocuments), req.Pattern),
		Data:    matchedDocuments,
	})
}

// matchGlobPattern matches documents against a glob pattern
// Supports standard glob patterns: *, ?, [chars], and ** for recursive matching
func matchGlobPattern(documents []models.Document, pattern string, includeDirs bool, docsDir string) []models.Document {
	var matched []models.Document

	// Check if pattern contains ** (recursive matching)
	hasDoubleStar := strings.Contains(pattern, "**")

	for _, doc := range documents {
		// Skip directories if not included
		if doc.IsDirectory && !includeDirs {
			continue
		}

		// Get relative path from docs directory for matching
		relPath := doc.FilePath

		// Handle recursive patterns with **
		if hasDoubleStar {
			if matchRecursiveGlob(relPath, pattern) {
				matched = append(matched, doc)
			}
		} else {
			// Use standard filepath.Match for non-recursive patterns
			// For patterns like "*.go", match against filename only
			// For patterns like "docs/*.md", match against full relative path
			matchedPath := relPath
			if !strings.Contains(pattern, "/") && !strings.Contains(pattern, string(filepath.Separator)) {
				// Pattern doesn't contain path separators - match against filename only
				matchedPath = filepath.Base(relPath)
			}

			if isMatched, err := filepath.Match(pattern, matchedPath); err == nil && isMatched {
				matched = append(matched, doc)
			}
		}
	}

	return matched
}

// matchRecursiveGlob matches a path against a glob pattern with ** support
// ** matches zero or more directories
func matchRecursiveGlob(path, pattern string) bool {
	// Convert path separators to forward slashes for consistent matching
	normalizedPath := strings.ReplaceAll(path, string(filepath.Separator), "/")
	normalizedPattern := strings.ReplaceAll(pattern, string(filepath.Separator), "/")

	// If pattern doesn't contain **, use standard matching
	if !strings.Contains(normalizedPattern, "**") {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// Split pattern by ** to handle recursive matching
	// Example: "docs/**/*.md" -> ["docs/", "/*.md"]
	// Example: "**/*.go" -> ["", "/*.go"]
	patternParts := strings.Split(normalizedPattern, "**")
	if len(patternParts) < 2 {
		// Invalid pattern or no ** found
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// Start pattern (before first **)
	startPattern := patternParts[0]
	// End pattern (after last **)
	endPattern := patternParts[len(patternParts)-1]

	// Check if path starts with start pattern
	if startPattern != "" {
		if !strings.HasPrefix(normalizedPath, startPattern) {
			return false
		}
		normalizedPath = normalizedPath[len(startPattern):]
	}

	// Check if path ends with end pattern
	if endPattern != "" {
		// For end patterns like "/*.md" or "*.md", we need to match the suffix
		if strings.HasPrefix(endPattern, "/") {
			// Pattern like "/*.md" - match from any directory boundary
			endPattern = endPattern[1:] // Remove leading /
			// Try matching from each directory boundary
			pathParts := strings.Split(normalizedPath, "/")
			for i := 0; i < len(pathParts); i++ {
				suffix := strings.Join(pathParts[i:], "/")
				if matched, _ := filepath.Match(endPattern, suffix); matched {
					return true
				}
			}
			return false
		} else {
			// Pattern like "*.md" - match filename only
			// Extract filename from path
			lastSlash := strings.LastIndex(normalizedPath, "/")
			if lastSlash >= 0 {
				filename := normalizedPath[lastSlash+1:]
				matched, _ := filepath.Match(endPattern, filename)
				return matched
			} else {
				// No directory separator, match entire path
				matched, _ := filepath.Match(endPattern, normalizedPath)
				return matched
			}
		}
	}

	// If no end pattern, pattern matches if start pattern matched
	return true
}

// GetDocument handles GET /api/documents/*filepath
func GetDocument(c *gin.Context) {
	filePathParam := c.Param("filepath")
	docsDir := viper.GetString("docs-dir")

	// Sanitize input path to ensure it's relative
	filePathParam = utils.SanitizeInputPath(filePathParam, docsDir)

	// Convert relative path to full path internally
	filePath := filepath.Join(docsDir, filePathParam)

	// Validate file path for security
	if !utils.IsValidFilePath(filePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file path",
			Error:   "File path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Read operations don't need locks - os.ReadFile is atomic and safe
	// Only write operations need locks to prevent concurrent modifications

	// Check if file exists and get file info
	fileInfo, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// File doesn't exist - return 200 with message indicating file doesn't exist
		c.JSON(http.StatusOK, models.APIResponse[models.Document]{
			Success: true,
			Message: "File does not exist",
			Data:    models.Document{},
			Error:   fmt.Sprintf("File not found: %s", filePathParam),
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to get file info",
			Error:   err.Error(),
		})
		return
	}

	// Read file
	content, err := os.ReadFile(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to read document",
			Error:   err.Error(),
		})
		return
	}

	// Determine folder
	folder := ""
	relPath, _ := filepath.Rel(docsDir, filePath)
	if dir := filepath.Dir(relPath); dir != "." {
		folder = dir
	}

	// Convert full path to relative path for API response
	relativePath, err := utils.GetRelativePath(filePath, docsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to convert file path",
			Error:   err.Error(),
		})
		return
	}

	// Check if file is an image
	var contentStr string
	isImage := isImageFile(filepath.Base(filePath))

	if isImage {
		// Image file - return base64 encoded data
		contentStr = formatImageContent(filepath.Base(filePath), content)
	} else if isTextBasedFile(filepath.Base(filePath), "") {
		// Text file - include content
		contentStr = string(content)
	} else {
		// Other binary files - return metadata
		contentStr = fmt.Sprintf("[Binary file: %d bytes]", len(content))
	}

	doc := models.Document{
		FilePath:    relativePath,
		Folder:      folder,
		Content:     contentStr, // Include content for read_workspace_file tool
		IsImage:     isImage,
		Size:        fileInfo.Size(),
		ModifiedAt:  fileInfo.ModTime(),
		IsDirectory: fileInfo.IsDir(),
	}

	// Check if this is a download request (query parameter or Accept header)
	downloadParam := c.Query("download")
	acceptHeader := c.GetHeader("Accept")
	downloadRequested := downloadParam == "true" || acceptHeader == "application/octet-stream"

	// Debug logging
	if !isImage && !isTextBasedFile(filepath.Base(filePath), "") {
		log.Printf("[GetDocument] Binary file detected: %s, download param: %s, accept header: %s, downloadRequested: %v",
			filepath.Base(filePath), downloadParam, acceptHeader, downloadRequested)
	}

	// For binary files when download is requested, return raw file
	if downloadRequested && !isImage && !isTextBasedFile(filepath.Base(filePath), "") {
		// Return raw binary file
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(filePath)))
		c.Data(http.StatusOK, "application/octet-stream", content)
		return
	}

	c.JSON(http.StatusOK, models.APIResponse[models.Document]{
		Success: true,
		Message: "Document retrieved successfully",
		Data:    doc,
	})
}

// UpdateDocument handles PUT /api/documents/*filepath
func UpdateDocument(c *gin.Context) {
	filePathParam := c.Param("filepath")
	var req models.UpdateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input path to ensure it's relative
	filePathParam = utils.SanitizeInputPath(filePathParam, docsDir)

	filePath := filepath.Join(docsDir, filePathParam)

	// Validate file path for security
	if !utils.IsValidFilePath(filePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file path",
			Error:   "File path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Acquire file lock
	lock, err := lockManager.AcquireLock(filePath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "File is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(lock)

	// Check if file exists and create directory if needed
	fileExists := true
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fileExists = false
		// Create directory if it doesn't exist
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
				Success: false,
				Message: "Failed to create directory",
				Error:   err.Error(),
			})
			return
		}
	}

	// Write content (create or update)
	if err := os.WriteFile(filePath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to write document",
			Error:   err.Error(),
		})
		return
	}

	// Queue file for semantic processing
	if fileProcessor := GetFileProcessor(); fileProcessor != nil {
		action := "update"
		if !fileExists {
			action = "create"
		}
		go fileProcessor.QueueJob(filePathParam, req.Content, action)
	}

	// Determine folder
	folder := ""
	relPath, _ := filepath.Rel(docsDir, filePath)
	if dir := filepath.Dir(relPath); dir != "." {
		folder = dir
	}

	// Handle git operations if commit message provided
	if req.CommitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", req.CommitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	// Convert full path to relative path for API response
	relativePath, err := utils.GetRelativePath(filePath, docsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to convert file path",
			Error:   err.Error(),
		})
		return
	}

	// Get file info for metadata
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to get file info",
			Error:   err.Error(),
		})
		return
	}

	doc := models.Document{
		FilePath:    relativePath,
		Folder:      folder,
		Size:        fileInfo.Size(),
		ModifiedAt:  fileInfo.ModTime(),
		IsDirectory: fileInfo.IsDir(),
	}

	// Determine appropriate message based on whether file was created or updated
	message := "Document updated successfully"
	if !fileExists {
		message = "Document created successfully"
	}

	c.JSON(http.StatusOK, models.APIResponse[models.Document]{
		Success: true,
		Message: message,
		Data:    doc,
	})
}

// DeleteDocument handles DELETE /api/documents/*filepath
func DeleteDocument(c *gin.Context) {
	filePathParam := c.Param("filepath")
	confirm := c.Query("confirm")
	commitMessage := c.Query("commit_message")

	if confirm != "true" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Deletion requires confirmation",
			Error:   "Add ?confirm=true to confirm deletion",
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input path to ensure it's relative
	filePathParam = utils.SanitizeInputPath(filePathParam, docsDir)

	filePath := filepath.Join(docsDir, filePathParam)

	// Validate file path for security
	if !utils.IsValidFilePath(filePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file path",
			Error:   "File path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Acquire file lock
	lock, err := lockManager.AcquireLock(filePath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "File is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(lock)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse[any]{
			Success: false,
			Message: "Document not found",
			Error:   "Document not found: " + filePathParam,
		})
		return
	}

	// Delete file
	if err := os.Remove(filePath); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to delete document",
			Error:   err.Error(),
		})
		return
	}

	// Queue file for semantic processing (delete embeddings)
	if fileProcessor := GetFileProcessor(); fileProcessor != nil {
		go fileProcessor.QueueJob(filePathParam, "", "delete")
	}

	// Handle git operations if commit message provided
	if commitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", commitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, models.APIResponse[any]{
		Success: true,
		Message: "Document deleted successfully",
	})
}

// MoveDocument handles POST /api/documents/*filepath/move
func MoveDocument(c *gin.Context) {
	filePathParam := c.Param("filepath")
	var req models.MoveDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input paths to ensure they're relative
	sourcePath := utils.SanitizeInputPath(filePathParam, docsDir)
	destinationPath := utils.SanitizeInputPath(req.DestinationPath, docsDir)

	// Validate source filepath (use lenient validation for move - supports all file types)
	if err := validateFilePathLenient(sourcePath); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid source filepath",
			Error:   err.Error(),
		})
		return
	}

	// Validate destination filepath (use lenient validation for move - supports all file types)
	if err := validateFilePathLenient(destinationPath); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid destination filepath",
			Error:   err.Error(),
		})
		return
	}

	// Check if source and destination are the same
	if sourcePath == destinationPath {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Source and destination paths are the same",
			Error:   "Cannot move file to the same location",
		})
		return
	}

	sourceFilePath := filepath.Join(docsDir, sourcePath)
	destinationFilePath := filepath.Join(docsDir, destinationPath)

	// Validate file paths for security
	if !utils.IsValidFilePath(sourceFilePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid source file path",
			Error:   "Source file path contains invalid characters or attempts directory traversal",
		})
		return
	}

	if !utils.IsValidFilePath(destinationFilePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid destination file path",
			Error:   "Destination file path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Check if source file exists
	if _, err := os.Stat(sourceFilePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse[any]{
			Success: false,
			Message: "Source file not found",
			Error:   "The file to move does not exist",
		})
		return
	}

	// Check if destination file already exists
	// Note: We check this before creating the directory, so if the directory
	// doesn't exist, os.Stat will fail and we'll continue to create it.
	if _, err := os.Stat(destinationFilePath); err == nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Destination file already exists",
			Error:   "File already exists: " + req.DestinationPath,
		})
		return
	}

	// Acquire locks for both source and destination
	sourceLock, err := lockManager.AcquireLock(sourcePath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Source file is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(sourceLock)

	destinationLock, err := lockManager.AcquireLock(destinationFilePath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Destination file is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(destinationLock)

	// Create destination directory if it doesn't exist
	destDir := filepath.Dir(destinationFilePath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create destination directory",
			Error:   err.Error(),
		})
		return
	}

	// Move file
	if err := os.Rename(sourceFilePath, destinationFilePath); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to move document",
			Error:   err.Error(),
		})
		return
	}

	// Handle git operations if commit message provided
	if req.CommitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", req.CommitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	// Create response data
	relativePath, err := utils.GetRelativePath(destinationFilePath, docsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to get relative path",
			Error:   err.Error(),
		})
		return
	}

	// Get new file info
	fileInfo, err := os.Stat(destinationFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to get new file info",
			Error:   err.Error(),
		})
		return
	}

	responseData := models.Document{
		FilePath:    relativePath,
		Folder:      filepath.Dir(relativePath),
		Type:        "file",
		Size:        fileInfo.Size(),
		ModifiedAt:  fileInfo.ModTime(),
		IsDirectory: fileInfo.IsDir(),
	}

	c.JSON(http.StatusOK, models.APIResponse[models.Document]{
		Success: true,
		Message: "Document moved successfully",
		Data:    responseData,
	})
}

// GetFileVersionHistory handles GET /api/documents/*filepath/versions
func GetFileVersionHistory(c *gin.Context) {
	filePathParam := c.Param("filepath")
	var req models.FileVersionHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid query parameters",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input path to ensure it's relative
	filePathParam = utils.SanitizeInputPath(filePathParam, docsDir)

	filePath := filepath.Join(docsDir, filePathParam)

	// Validate file path for security
	if !utils.IsValidFilePath(filePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file path",
			Error:   "File path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse[any]{
			Success: false,
			Message: "Document not found",
			Error:   "Document not found: " + filePathParam,
		})
		return
	}

	// Get version history
	versionManager := utils.NewGitVersionManager(docsDir)
	versions, err := versionManager.GetFileVersionHistory(filePath, req.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to get version history",
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse[any]{
		Success: true,
		Message: "Version history retrieved successfully",
		Data:    versions,
	})
}

// RestoreFileVersion handles POST /api/documents/*filepath/restore
func RestoreFileVersion(c *gin.Context) {
	filePathParam := c.Param("filepath")
	var req models.RestoreVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input path to ensure it's relative
	filePathParam = utils.SanitizeInputPath(filePathParam, docsDir)

	filePath := filepath.Join(docsDir, filePathParam)

	// Validate file path for security
	if !utils.IsValidFilePath(filePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file path",
			Error:   "File path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Acquire file lock
	lock, err := lockManager.AcquireLock(filePath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "File is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(lock)

	// Restore the version
	versionManager := utils.NewGitVersionManager(docsDir)
	if err := versionManager.RestoreFileVersion(filePath, req.CommitHash, req.CommitMessage); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to restore version",
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, models.APIResponse[any]{
		Success: true,
		Message: "File version restored successfully",
		Data: map[string]interface{}{
			"commit_hash": req.CommitHash,
			"filepath":    filePathParam,
		},
	})
}

// CreateFolder handles POST /api/folders
func CreateFolder(c *gin.Context) {
	var req models.CreateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	// Sanitize input folder path to ensure it's relative
	docsDir := viper.GetString("docs-dir")
	req.FolderPath = utils.SanitizeInputPath(req.FolderPath, docsDir)

	// Validate folder path
	if err := validateFolderPath(req.FolderPath); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid folder path",
			Error:   err.Error(),
		})
		return
	}

	folderPath := filepath.Join(docsDir, req.FolderPath)

	// Validate folder path for security
	if !utils.IsValidFilePath(folderPath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid folder path",
			Error:   "Folder path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Check if folder already exists or if a file exists at that path
	if info, err := os.Stat(folderPath); err == nil {
		if info.IsDir() {
			c.JSON(http.StatusConflict, models.APIResponse[any]{
				Success: false,
				Message: "Folder already exists",
				Error:   "Folder already exists: " + req.FolderPath,
			})
		} else {
			c.JSON(http.StatusConflict, models.APIResponse[any]{
				Success: false,
				Message: "File exists at folder path",
				Error:   "A file already exists at this path. Cannot create folder: " + req.FolderPath,
			})
		}
		return
	}

	// Acquire folder lock
	lock, err := lockManager.AcquireLock(folderPath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Folder is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(lock)

	// Create the folder
	if err := os.MkdirAll(folderPath, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create folder",
			Error:   err.Error(),
		})
		return
	}

	// Handle git operations if commit message provided
	if req.CommitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", req.CommitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	response := models.CreateFolderResponse{
		FolderPath: req.FolderPath,
		Created:    true,
	}

	c.JSON(http.StatusCreated, models.APIResponse[any]{
		Success: true,
		Message: "Folder created successfully",
		Data:    response,
	})
}

// CopyFolder handles POST /api/folders/copy
func CopyFolder(c *gin.Context) {
	var req models.CopyFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input paths to ensure they're relative
	req.SourcePath = utils.SanitizeInputPath(req.SourcePath, docsDir)
	req.DestinationPath = utils.SanitizeInputPath(req.DestinationPath, docsDir)

	// Validate folder paths
	if err := validateFolderPath(req.SourcePath); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid source folder path",
			Error:   err.Error(),
		})
		return
	}

	if err := validateFolderPath(req.DestinationPath); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid destination folder path",
			Error:   err.Error(),
		})
		return
	}

	sourcePath := filepath.Join(docsDir, req.SourcePath)
	destinationPath := filepath.Join(docsDir, req.DestinationPath)

	// Validate paths for security
	if !utils.IsValidFilePath(sourcePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid source folder path",
			Error:   "Source path contains invalid characters or attempts directory traversal",
		})
		return
	}

	if !utils.IsValidFilePath(destinationPath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid destination folder path",
			Error:   "Destination path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Check if source folder exists
	sourceInfo, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse[any]{
			Success: false,
			Message: "Source folder not found",
			Error:   "Source folder does not exist: " + req.SourcePath,
		})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to check source folder",
			Error:   err.Error(),
		})
		return
	}

	// Check if source is actually a directory
	if !sourceInfo.IsDir() {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Source path is not a folder",
			Error:   "Source path is not a directory: " + req.SourcePath,
		})
		return
	}

	// Check if destination already exists
	if _, err := os.Stat(destinationPath); err == nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Destination folder already exists",
			Error:   "Destination folder already exists: " + req.DestinationPath,
		})
		return
	}

	// Acquire locks for both source and destination
	sourceLock, err := lockManager.AcquireLock(sourcePath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Source folder is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(sourceLock)

	destinationLock, err := lockManager.AcquireLock(destinationPath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Destination folder is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(destinationLock)

	// Counters for response
	var filesCopied int
	var dirsCreated int

	// Use filepath.Walk to recursively copy all files and directories
	err = filepath.Walk(sourcePath, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory entirely
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		// Calculate relative path from source
		relPath, err := filepath.Rel(sourcePath, srcPath)
		if err != nil {
			return err
		}

		// Build destination path
		dstPath := filepath.Join(destinationPath, relPath)

		if info.IsDir() {
			// Create directory
			if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstPath, err)
			}
			dirsCreated++
		} else {
			// Create parent directory if it doesn't exist
			if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", dstPath, err)
			}

			// Copy file
			srcFile, err := os.Open(srcPath)
			if err != nil {
				return fmt.Errorf("failed to open source file %s: %w", srcPath, err)
			}
			defer srcFile.Close()

			dstFile, err := os.Create(dstPath)
			if err != nil {
				return fmt.Errorf("failed to create destination file %s: %w", dstPath, err)
			}
			defer dstFile.Close()

			// Copy file contents
			_, err = io.Copy(dstFile, srcFile)
			if err != nil {
				return fmt.Errorf("failed to copy file from %s to %s: %w", srcPath, dstPath, err)
			}

			// Preserve file permissions
			if err := os.Chmod(dstPath, info.Mode()); err != nil {
				return fmt.Errorf("failed to set permissions for %s: %w", dstPath, err)
			}

			filesCopied++
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to copy folder",
			Error:   err.Error(),
		})
		return
	}

	// Handle git operations if commit message provided
	if req.CommitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", req.CommitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	response := models.CopyFolderResponse{
		SourcePath:      req.SourcePath,
		DestinationPath: req.DestinationPath,
		FilesCopied:     filesCopied,
		DirsCreated:     dirsCreated,
	}

	c.JSON(http.StatusOK, models.APIResponse[any]{
		Success: true,
		Message: fmt.Sprintf("Folder copied successfully: %d files, %d directories", filesCopied, dirsCreated),
		Data:    response,
	})
}

// DeleteFolder handles DELETE /api/folders/*folderpath
func DeleteFolder(c *gin.Context) {
	folderPathParam := c.Param("folderpath")
	commitMessage := c.Query("commit_message")
	confirm := c.Query("confirm") == "true"

	// Check if this is a request to delete all files in folder
	if strings.HasSuffix(c.Request.URL.Path, "/files") {
		// Remove "/files" from the folderpath
		folderPathParam = strings.TrimSuffix(folderPathParam, "/files")
		DeleteAllFilesInFolder(c, folderPathParam, commitMessage, confirm)
		return
	}

	if !confirm {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Folder deletion requires confirmation",
			Error:   "Add ?confirm=true to confirm folder deletion",
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input folder path to ensure it's relative
	folderPathParam = utils.SanitizeInputPath(folderPathParam, docsDir)

	folderPath := filepath.Join(docsDir, folderPathParam)

	// Validate folder path for security
	if !utils.IsValidFilePath(folderPath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid folder path",
			Error:   "Folder path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Check if folder exists
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse[any]{
			Success: false,
			Message: "Folder not found",
			Error:   "Folder not found: " + folderPathParam,
		})
		return
	}

	// Check if it's actually a directory
	if info, err := os.Stat(folderPath); err == nil && !info.IsDir() {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Path is not a folder",
			Error:   "Path is not a directory: " + folderPathParam,
		})
		return
	}

	// Acquire folder lock (using folder path as lock key)
	lock, err := lockManager.AcquireLock(folderPath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Folder is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(lock)

	// Remove the folder and all its contents
	if err := os.RemoveAll(folderPath); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to delete folder",
			Error:   err.Error(),
		})
		return
	}

	// If commit message provided, commit the deletion
	if commitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", commitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, models.APIResponse[any]{
		Success: true,
		Message: "Folder deleted successfully",
		Data: map[string]interface{}{
			"folder_path": folderPathParam,
		},
	})
}

// DeleteAllFilesInFolder handles DELETE /api/folders/*folderpath/files
func DeleteAllFilesInFolder(c *gin.Context, folderPathParam, commitMessage string, confirm bool) {
	if !confirm {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "File deletion requires confirmation",
			Error:   "Add ?confirm=true to confirm deletion of all files in folder",
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input folder path to ensure it's relative
	folderPathParam = utils.SanitizeInputPath(folderPathParam, docsDir)

	folderPath := filepath.Join(docsDir, folderPathParam)

	// Validate folder path for security
	if !utils.IsValidFilePath(folderPath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid folder path",
			Error:   "Folder path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Check if folder exists
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, models.APIResponse[any]{
			Success: false,
			Message: "Folder not found",
			Error:   "Folder not found: " + folderPathParam,
		})
		return
	}

	// Check if it's actually a directory
	if info, err := os.Stat(folderPath); err == nil && !info.IsDir() {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Path is not a folder",
			Error:   "Path is not a directory: " + folderPathParam,
		})
		return
	}

	// Acquire folder lock (using folder path as lock key)
	lock, err := lockManager.AcquireLock(folderPath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "Folder is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(lock)

	// Count and collect all files and folders to be deleted
	var itemsToDelete []string
	var deletedCount int

	err = filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the root folder itself
		if path == folderPath {
			return nil
		}

		// Get relative path for processing
		relPath, err := filepath.Rel(docsDir, path)
		if err != nil {
			return err
		}
		itemsToDelete = append(itemsToDelete, relPath)

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to scan folder contents",
			Error:   err.Error(),
		})
		return
	}

	// Delete all files and folders
	for _, itemPath := range itemsToDelete {
		fullPath := filepath.Join(docsDir, itemPath)

		// Acquire lock for each item
		itemLock, err := lockManager.AcquireLock(fullPath, 10*time.Second)
		if err != nil {
			// Log warning but continue with other items
			fmt.Printf("Warning: Could not acquire lock for item %s: %v\n", itemPath, err)
			continue
		}

		// Check if it's a file or directory
		info, err := os.Stat(fullPath)
		if err != nil {
			lockManager.ReleaseLock(itemLock)
			fmt.Printf("Warning: Could not stat item %s: %v\n", itemPath, err)
			continue
		}

		// Delete the item (file or directory)
		if info.IsDir() {
			// Delete directory and all its contents
			if err := os.RemoveAll(fullPath); err != nil {
				lockManager.ReleaseLock(itemLock)
				fmt.Printf("Warning: Could not delete directory %s: %v\n", itemPath, err)
				continue
			}
		} else {
			// Delete file
			if err := os.Remove(fullPath); err != nil {
				lockManager.ReleaseLock(itemLock)
				fmt.Printf("Warning: Could not delete file %s: %v\n", itemPath, err)
				continue
			}

			// Queue file for semantic processing (delete embeddings)
			if fileProcessor := GetFileProcessor(); fileProcessor != nil {
				go fileProcessor.QueueJob(itemPath, "", "delete")
			}
		}

		lockManager.ReleaseLock(itemLock)
		deletedCount++
	}

	// Handle git operations if commit message provided
	if commitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", commitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, models.APIResponse[any]{
		Success: true,
		Message: fmt.Sprintf("Successfully deleted %d items (files and folders) from folder", deletedCount),
		Data: map[string]interface{}{
			"folder_path":   folderPathParam,
			"items_deleted": deletedCount,
			"total_found":   len(itemsToDelete),
		},
	})
}

// UploadFile handles POST /api/upload
func UploadFile(c *gin.Context) {
	var req models.FileUploadRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request parameters",
			Error:   err.Error(),
		})
		return
	}

	// Get the uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "No file uploaded",
			Error:   "Please provide a file to upload",
		})
		return
	}
	defer file.Close()

	// Validate file size (max 10MB)
	const maxFileSize = 10 * 1024 * 1024 // 10MB
	if header.Size > maxFileSize {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "File too large",
			Error:   fmt.Sprintf("File size exceeds maximum allowed size of %d bytes", maxFileSize),
		})
		return
	}

	// Validate file type - only allow text-based files
	if !isTextBasedFile(header.Filename, header.Header.Get("Content-Type")) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file type",
			Error:   "Only text-based files are allowed (txt, md, json, csv, yaml, xml, etc.). Binary files like images, videos, and executables are not permitted.",
		})
		return
	}

	// Validate file name
	if header.Filename == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file name",
			Error:   "File name cannot be empty",
		})
		return
	}

	// Sanitize file name
	fileName := sanitizeFilename(header.Filename)
	if fileName == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file name",
			Error:   "File name contains only invalid characters",
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Sanitize input folder path to ensure it's relative
	req.FolderPath = utils.SanitizeInputPath(req.FolderPath, docsDir)

	// Create folder path
	folderPath := filepath.Join(docsDir, req.FolderPath)
	if err := os.MkdirAll(folderPath, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create folder",
			Error:   err.Error(),
		})
		return
	}

	// Validate folder path for security
	if !utils.IsValidFilePath(folderPath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid folder path",
			Error:   "Folder path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Create full file path
	fullFilePath := filepath.Join(folderPath, fileName)

	// Validate full file path for security
	if !utils.IsValidFilePath(fullFilePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file path",
			Error:   "File path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Acquire file lock
	lock, err := lockManager.AcquireLock(fullFilePath, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusConflict, models.APIResponse[any]{
			Success: false,
			Message: "File is currently being modified",
			Error:   err.Error(),
		})
		return
	}
	defer lockManager.ReleaseLock(lock)

	// Create the file
	dst, err := os.Create(fullFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create file",
			Error:   err.Error(),
		})
		return
	}
	defer dst.Close()

	// Copy file content
	fileSize, err := io.Copy(dst, file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to save file",
			Error:   err.Error(),
		})
		return
	}

	// File uploaded successfully

	// Get content type
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// If commit message provided, commit the upload
	if req.CommitMessage != "" {
		if err := utils.SyncWithGitHub(docsDir, "main", req.CommitMessage); err != nil {
			// Log error but don't fail the request
			fmt.Printf("Warning: Git operation failed: %v\n", err)
		}
	}

	// Convert full path to relative path for API response
	relativePath, err := utils.GetRelativePath(fullFilePath, docsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to convert file path",
			Error:   err.Error(),
		})
		return
	}

	// Prepare response
	response := models.FileUploadResponse{
		FilePath:    relativePath,
		FileName:    fileName,
		FileSize:    fileSize,
		ContentType: contentType,
		Folder:      req.FolderPath,
	}

	c.JSON(http.StatusOK, models.APIResponse[any]{
		Success: true,
		Message: "File uploaded successfully",
		Data:    response,
	})
}

// sanitizeFilename converts a title to a safe filename
func sanitizeFilename(title string) string {
	// Replace spaces with hyphens and remove special characters
	filename := strings.ReplaceAll(title, " ", "-")
	filename = strings.ReplaceAll(filename, "/", "-")
	filename = strings.ReplaceAll(filename, "\\", "-")
	filename = strings.ReplaceAll(filename, ":", "-")
	filename = strings.ReplaceAll(filename, "*", "-")
	filename = strings.ReplaceAll(filename, "?", "-")
	filename = strings.ReplaceAll(filename, "\"", "-")
	filename = strings.ReplaceAll(filename, "<", "-")
	filename = strings.ReplaceAll(filename, ">", "-")
	filename = strings.ReplaceAll(filename, "|", "-")

	// Convert to lowercase
	filename = strings.ToLower(filename)

	// Remove multiple consecutive hyphens
	for strings.Contains(filename, "--") {
		filename = strings.ReplaceAll(filename, "--", "-")
	}

	// Remove leading/trailing hyphens
	filename = strings.Trim(filename, "-")

	// Ensure it's not empty
	if filename == "" {
		filename = "untitled"
	}

	return filename
}

// HandleDocumentRequest routes document requests to appropriate handlers based on method and path
func HandleDocumentRequest(c *gin.Context) {
	filePathParam := c.Param("filepath")
	method := c.Request.Method
	path := c.Request.URL.Path

	// Remove leading slash from filepath
	filePathParam = strings.TrimPrefix(filePathParam, "/")

	// Route based on HTTP method and path
	switch method {
	case "GET":
		// Basic document retrieval
		c.Params = []gin.Param{{Key: "filepath", Value: filePathParam}}
		GetDocument(c)
	case "PUT":
		// Basic document update
		c.Params = []gin.Param{{Key: "filepath", Value: filePathParam}}
		UpdateDocument(c)
	case "PATCH":
		if strings.HasSuffix(path, "/diff") {
			// Remove /diff from filepath for diff patch operations
			filePathParam = strings.TrimSuffix(filePathParam, "/diff")
			c.Params = []gin.Param{{Key: "filepath", Value: filePathParam}}
			DiffPatchDocument(c)
		} else {
			c.JSON(http.StatusMethodNotAllowed, models.APIResponse[any]{
				Success: false,
				Message: "Method not allowed",
				Error:   "PATCH method only supports /diff endpoint",
			})
		}
	case "POST":
		if strings.HasSuffix(path, "/move") {
			// Remove /move from filepath
			filePathParam = strings.TrimSuffix(filePathParam, "/move")
			c.Params = []gin.Param{{Key: "filepath", Value: filePathParam}}
			MoveDocument(c)
		} else {
			c.JSON(http.StatusMethodNotAllowed, models.APIResponse[any]{
				Success: false,
				Message: "Method not allowed",
				Error:   "POST method not supported for this endpoint",
			})
		}
	case "DELETE":
		// Basic document deletion
		c.Params = []gin.Param{{Key: "filepath", Value: filePathParam}}
		DeleteDocument(c)
	default:
		c.JSON(http.StatusMethodNotAllowed, models.APIResponse[any]{
			Success: false,
			Message: "Method not allowed",
			Error:   "Unsupported HTTP method: " + method,
		})
	}
}

// validateFilepath validates the filepath for security and format
func validateFilepath(filepath string) error {
	// Check if filepath is empty
	if filepath == "" {
		return fmt.Errorf("filepath cannot be empty")
	}

	// Check for directory traversal attacks
	if strings.Contains(filepath, "..") || strings.HasPrefix(filepath, "/") {
		return fmt.Errorf("filepath contains invalid characters or path traversal")
	}

	// Check if it's a markdown file
	if !strings.HasSuffix(strings.ToLower(filepath), ".md") {
		return fmt.Errorf("filepath must end with .md extension")
	}

	// Check for invalid characters
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	for _, char := range invalidChars {
		if strings.Contains(filepath, char) {
			return fmt.Errorf("filepath contains invalid character: %s", char)
		}
	}

	return nil
}

// validateFilePathLenient validates filepath for move operations - supports all file types, not just .md
func validateFilePathLenient(filepath string) error {
	// Check if filepath is empty
	if filepath == "" {
		return fmt.Errorf("filepath cannot be empty")
	}

	// Check for directory traversal attacks
	if strings.Contains(filepath, "..") || strings.HasPrefix(filepath, "/") {
		return fmt.Errorf("filepath contains invalid characters or path traversal")
	}

	// Check for invalid characters
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	for _, char := range invalidChars {
		if strings.Contains(filepath, char) {
			return fmt.Errorf("filepath contains invalid character: %s", char)
		}
	}

	return nil
}

// validateFolderPath validates the folder path for security and format
func validateFolderPath(folderPath string) error {
	// Check if folder path is empty
	if folderPath == "" {
		return fmt.Errorf("folder path cannot be empty")
	}

	// Check for directory traversal attacks
	if strings.Contains(folderPath, "..") || strings.HasPrefix(folderPath, "/") {
		return fmt.Errorf("folder path contains invalid characters or path traversal")
	}

	// Check for invalid characters
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	for _, char := range invalidChars {
		if strings.Contains(folderPath, char) {
			return fmt.Errorf("folder path contains invalid character: %s", char)
		}
	}

	// Check for reserved names
	reservedNames := []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}
	upperFolderPath := strings.ToUpper(folderPath)
	for _, reserved := range reservedNames {
		if upperFolderPath == reserved || strings.HasPrefix(upperFolderPath, reserved+".") {
			return fmt.Errorf("folder path uses reserved name: %s", reserved)
		}
	}

	return nil
}

// isTextBasedFile checks if a file is text-based based on extension and MIME type
func isTextBasedFile(filename, contentType string) bool {
	// Get file extension
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" {
		ext = ext[1:] // Remove the dot
	}

	// Allowed text-based file extensions
	allowedExtensions := map[string]bool{
		"txt":        true,
		"md":         true,
		"markdown":   true,
		"json":       true,
		"csv":        true,
		"yaml":       true,
		"yml":        true,
		"xml":        true,
		"html":       true,
		"htm":        true,
		"css":        true,
		"js":         true,
		"ts":         true,
		"py":         true,
		"go":         true,
		"java":       true,
		"cpp":        true,
		"c":          true,
		"h":          true,
		"hpp":        true,
		"php":        true,
		"rb":         true,
		"sh":         true,
		"bash":       true,
		"zsh":        true,
		"fish":       true,
		"sql":        true,
		"log":        true,
		"conf":       true,
		"config":     true,
		"ini":        true,
		"toml":       true,
		"env":        true,
		"gitignore":  true,
		"dockerfile": true,
		"makefile":   true,
		"cmake":      true,
		"gradle":     true,
		"maven":      true,
		"pom":        true,
		"sbt":        true,
		"scala":      true,
		"kt":         true,
		"swift":      true,
		"rs":         true,
		"dart":       true,
		"r":          true,
		"m":          true,
		"pl":         true,
		"lua":        true,
		"vim":        true,
		"emacs":      true,
		"tex":        true,
		"latex":      true,
		"rst":        true,
		"adoc":       true,
		"asciidoc":   true,
		"org":        true,
		"wiki":       true,
		"svg":        true,  // SVG is text-based
		"pdf":        false, // PDF is binary
		"doc":        false, // Word docs are binary
		"docx":       false,
		"xls":        false, // Excel files are binary
		"xlsx":       false,
		"ppt":        false, // PowerPoint files are binary
		"pptx":       false,
		"zip":        false, // Archives are binary
		"rar":        false,
		"7z":         false,
		"tar":        false,
		"gz":         false,
		"bz2":        false,
		"xz":         false,
		"jpg":        false, // Images are binary
		"jpeg":       false,
		"png":        false,
		"gif":        false,
		"bmp":        false,
		"tiff":       false,
		"webp":       false,
		"ico":        false,
		"mp4":        false, // Videos are binary
		"avi":        false,
		"mov":        false,
		"wmv":        false,
		"flv":        false,
		"webm":       false,
		"mp3":        false, // Audio files are binary
		"wav":        false,
		"flac":       false,
		"aac":        false,
		"ogg":        false,
		"exe":        false, // Executables are binary
		"dll":        false,
		"so":         false,
		"dylib":      false,
		"bin":        false,
		"app":        false,
		"deb":        false,
		"rpm":        false,
		"msi":        false,
		"dmg":        false,
		"iso":        false,
	}

	// Check extension first
	if allowed, exists := allowedExtensions[ext]; exists {
		return allowed
	}

	// If extension not found, check MIME type
	allowedMimeTypes := map[string]bool{
		"text/plain":                true,
		"text/markdown":             true,
		"text/html":                 true,
		"text/css":                  true,
		"text/javascript":           true,
		"text/x-javascript":         true,
		"text/typescript":           true,
		"application/json":          true,
		"application/xml":           true,
		"text/xml":                  true,
		"text/csv":                  true,
		"application/csv":           true,
		"text/yaml":                 true,
		"application/x-yaml":        true,
		"text/x-yaml":               true,
		"application/x-python-code": true,
		"text/x-python":             true,
		"text/x-go":                 true,
		"text/x-java":               true,
		"text/x-c":                  true,
		"text/x-c++":                true,
		"text/x-php":                true,
		"text/x-ruby":               true,
		"text/x-shellscript":        true,
		"text/x-sql":                true,
		"text/x-log":                true,
		"text/x-ini":                true,
		"text/x-toml":               true,
		"text/x-dockerfile":         true,
		"text/x-makefile":           true,
		"text/x-cmake":              true,
		"text/x-gradle":             true,
		"text/x-maven":              true,
		"text/x-sbt":                true,
		"text/x-scala":              true,
		"text/x-kotlin":             true,
		"text/x-swift":              true,
		"text/x-rust":               true,
		"text/x-dart":               true,
		"text/x-r":                  true,
		"text/x-matlab":             true,
		"text/x-perl":               true,
		"text/x-lua":                true,
		"text/x-vim":                true,
		"text/x-emacs":              true,
		"text/x-tex":                true,
		"text/x-latex":              true,
		"text/x-rst":                true,
		"text/x-asciidoc":           true,
		"text/x-org":                true,
		"text/x-wiki":               true,
		"image/svg+xml":             true,  // SVG is text-based
		"application/pdf":           false, // PDF is binary
		"application/msword":        false, // Word docs are binary
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": false,
		"application/vnd.ms-excel": false, // Excel files are binary
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         false,
		"application/vnd.ms-powerpoint":                                             false, // PowerPoint files are binary
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": false,
		"application/zip":               false, // Archives are binary
		"application/x-rar-compressed":  false,
		"application/x-7z-compressed":   false,
		"application/x-tar":             false,
		"application/gzip":              false,
		"application/x-bzip2":           false,
		"application/x-xz":              false,
		"image/jpeg":                    false, // Images are binary
		"image/png":                     false,
		"image/gif":                     false,
		"image/bmp":                     false,
		"image/tiff":                    false,
		"image/webp":                    false,
		"image/x-icon":                  false,
		"video/mp4":                     false, // Videos are binary
		"video/avi":                     false,
		"video/quicktime":               false,
		"video/x-msvideo":               false,
		"video/x-flv":                   false,
		"video/webm":                    false,
		"audio/mpeg":                    false, // Audio files are binary
		"audio/wav":                     false,
		"audio/flac":                    false,
		"audio/aac":                     false,
		"audio/ogg":                     false,
		"application/x-executable":      false, // Executables are binary
		"application/x-msdownload":      false,
		"application/x-sharedlib":       false,
		"application/x-archive":         false,
		"application/x-debian-package":  false,
		"application/x-rpm":             false,
		"application/x-msi":             false,
		"application/x-apple-diskimage": false,
		"application/x-iso9660-image":   false,
	}

	// Check MIME type
	if allowed, exists := allowedMimeTypes[contentType]; exists {
		return allowed
	}

	// If neither extension nor MIME type is recognized, default to false (reject)
	return false
}
