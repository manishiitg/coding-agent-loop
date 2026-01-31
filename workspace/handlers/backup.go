package handlers

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workspace/models"
	"workspace/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// ExportWorkspaceRequest represents the request to export a workspace
type ExportWorkspaceRequest struct {
	WorkspacePath string `json:"workspace_path" binding:"required"`
}

// ImportWorkspaceRequest represents the request to import a workspace backup
type ImportWorkspaceRequest struct {
	WorkspacePath string `form:"workspace_path" binding:"required"`
	Overwrite     bool   `form:"overwrite"` // Whether to overwrite existing files
}

// ExportWorkspace handles POST /api/workspace/export
// Creates a ZIP archive of the entire workspace folder
func ExportWorkspace(c *gin.Context) {
	var req ExportWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	docsDir := viper.GetString("docs-dir")
	
	// Sanitize and validate workspace path
	workspacePath := utils.SanitizeInputPath(req.WorkspacePath, docsDir)
	fullWorkspacePath := filepath.Join(docsDir, workspacePath)

	// Check if workspace directory exists
	if info, err := os.Stat(fullWorkspacePath); err != nil || !info.IsDir() {
		c.JSON(http.StatusNotFound, models.APIResponse[any]{
			Success: false,
			Message: "Workspace not found",
			Error:   fmt.Sprintf("Workspace path does not exist: %s", workspacePath),
		})
		return
	}

	// Create a temporary ZIP file
	tempZipPath := filepath.Join(os.TempDir(), fmt.Sprintf("workspace-backup-%d.zip", time.Now().Unix()))
	defer os.Remove(tempZipPath) // Clean up temp file

	// Create ZIP file
	zipFile, err := os.Create(tempZipPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create backup file",
			Error:   err.Error(),
		})
		return
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Walk through the workspace directory and add all files to ZIP
	err = filepath.Walk(fullWorkspacePath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories (we'll add files only)
		if info.IsDir() {
			return nil
		}

		// Skip symlinks (they may point to directories or files outside the workspace)
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Calculate relative path from workspace root
		relPath, err := filepath.Rel(fullWorkspacePath, filePath)
		if err != nil {
			return err
		}

		// Open the file
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer file.Close()

		// Create a file in the ZIP
		zipEntry, err := zipWriter.Create(relPath)
		if err != nil {
			return err
		}

		// Copy file contents to ZIP
		_, err = io.Copy(zipEntry, file)
		return err
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create backup archive",
			Error:   err.Error(),
		})
		return
	}

	// Close ZIP writer to finalize the archive
	if err := zipWriter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to finalize backup archive",
			Error:   err.Error(),
		})
		return
	}

	// Generate filename for download
	workspaceName := filepath.Base(workspacePath)
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-backup-%s.zip", workspaceName, timestamp)

	// Set headers for file download
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Header("Content-Transfer-Encoding", "binary")

	// Open the ZIP file for reading
	zipFileReader, err := os.Open(tempZipPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to read backup file",
			Error:   err.Error(),
		})
		return
	}
	defer zipFileReader.Close()

	// Get file info for Content-Length
	zipFileInfo, err := zipFileReader.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to get backup file info",
			Error:   err.Error(),
		})
		return
	}

	c.Header("Content-Length", fmt.Sprintf("%d", zipFileInfo.Size()))

	// Stream the ZIP file to the client
	c.DataFromReader(http.StatusOK, zipFileInfo.Size(), "application/zip", zipFileReader, map[string]string{
		"Content-Disposition": fmt.Sprintf("attachment; filename=\"%s\"", filename),
	})
}

// ImportWorkspace handles POST /api/workspace/import
// Extracts a ZIP archive to restore a workspace folder
func ImportWorkspace(c *gin.Context) {
	// Parse form data
	var req ImportWorkspaceRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request",
			Error:   err.Error(),
		})
		return
	}

	// Get the uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "No file uploaded",
			Error:   err.Error(),
		})
		return
	}

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".zip") {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file type",
			Error:   "Only ZIP files are supported",
		})
		return
	}

	docsDir := viper.GetString("docs-dir")
	
	// Sanitize and validate workspace path
	workspacePath := utils.SanitizeInputPath(req.WorkspacePath, docsDir)
	fullWorkspacePath := filepath.Join(docsDir, workspacePath)

	// Check if workspace directory exists (if not overwriting, we might want to create it)
	if !req.Overwrite {
		if _, err := os.Stat(fullWorkspacePath); err == nil {
			c.JSON(http.StatusConflict, models.APIResponse[any]{
				Success: false,
				Message: "Workspace already exists",
				Error:   fmt.Sprintf("Workspace path already exists: %s. Use overwrite=true to replace it.", workspacePath),
			})
			return
		}
	}

	// Create workspace directory if it doesn't exist
	if err := os.MkdirAll(fullWorkspacePath, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to create workspace directory",
			Error:   err.Error(),
		})
		return
	}

	// Save uploaded file to temporary location
	tempZipPath := filepath.Join(os.TempDir(), fmt.Sprintf("workspace-import-%d.zip", time.Now().Unix()))
	if err := c.SaveUploadedFile(file, tempZipPath); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to save uploaded file",
			Error:   err.Error(),
		})
		return
	}
	defer os.Remove(tempZipPath) // Clean up temp file

	// Open the ZIP file
	zipReader, err := zip.OpenReader(tempZipPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid ZIP file",
			Error:   err.Error(),
		})
		return
	}
	defer zipReader.Close()

	// Extract all files from the ZIP
	var extractedFiles []string
	for _, zipFile := range zipReader.File {
		// Sanitize the file path to prevent directory traversal
		filePath := filepath.Join(fullWorkspacePath, zipFile.Name)
		
		// Ensure the extracted path is within the workspace directory
		if !strings.HasPrefix(filePath, fullWorkspacePath) {
			c.JSON(http.StatusBadRequest, models.APIResponse[any]{
				Success: false,
				Message: "Invalid file path in archive",
				Error:   fmt.Sprintf("Path traversal detected: %s", zipFile.Name),
			})
			return
		}

		// Create directory structure if needed
		if zipFile.FileInfo().IsDir() {
			if err := os.MkdirAll(filePath, 0755); err != nil {
				c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
					Success: false,
					Message: "Failed to create directory",
					Error:   err.Error(),
				})
				return
			}
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
				Success: false,
				Message: "Failed to create directory structure",
				Error:   err.Error(),
			})
			return
		}

		// Open file from ZIP
		zipFileReader, err := zipFile.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
				Success: false,
				Message: "Failed to read file from archive",
				Error:   err.Error(),
			})
			return
		}

		// Create destination file
		destFile, err := os.Create(filePath)
		if err != nil {
			zipFileReader.Close()
			c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
				Success: false,
				Message: "Failed to create destination file",
				Error:   err.Error(),
			})
			return
		}

		// Copy file contents
		_, err = io.Copy(destFile, zipFileReader)
		destFile.Close()
		zipFileReader.Close()

		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
				Success: false,
				Message: "Failed to extract file",
				Error:   err.Error(),
			})
			return
		}

		extractedFiles = append(extractedFiles, zipFile.Name)
	}

	c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
		Success: true,
		Message: "Workspace backup imported successfully",
		Data: map[string]interface{}{
			"workspace_path":   workspacePath,
			"files_extracted":  len(extractedFiles),
			"extracted_files": extractedFiles,
		},
	})
}



