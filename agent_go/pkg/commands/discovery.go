package commands

import (
	"fmt"
	"path"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/skills"
)

// DiscoverCommands discovers all user-defined commands in the workspace
func DiscoverCommands(workspaceAPIURL string) ([]Command, error) {
	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	// List all folders in commands/custom/
	entries, err := client.ListFiles(CustomCommandsSubPath)
	if err != nil {
		// If folder doesn't exist, return empty list
		return []Command{}, nil
	}

	var cmds []Command

	for _, entry := range entries {
		if entry.Type != "folder" {
			continue
		}

		folderName := path.Base(entry.Filepath)
		cmdFilePath := path.Join(entry.Filepath, CommandFileName)

		content, err := client.ReadFile(cmdFilePath)
		if err != nil {
			// Skip folders without COMMAND.md
			continue
		}

		cmd, err := ParseCommandFromContent(content, folderName, cmdFilePath)
		if err != nil {
			// Skip invalid commands
			continue
		}

		cmds = append(cmds, *cmd)
	}

	return cmds, nil
}

// GetCommand retrieves a specific command by folder name
func GetCommand(workspaceAPIURL, folderName string) (*Command, error) {
	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	cmdFilePath := path.Join(CustomCommandsSubPath, folderName, CommandFileName)
	content, err := client.ReadFile(cmdFilePath)
	if err != nil {
		return nil, fmt.Errorf("command not found: %w", err)
	}

	cmd, err := ParseCommandFromContent(content, folderName, cmdFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse command: %w", err)
	}

	return cmd, nil
}

// CreateCommand creates a new command folder and writes COMMAND.md
func CreateCommand(workspaceAPIURL, folderName, content string) (*Command, error) {
	// Validate content first
	frontmatter, body, err := ValidateCommandContent(content)
	if err != nil {
		return nil, fmt.Errorf("invalid command content: %w", err)
	}

	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	// Create the folder
	folderPath := path.Join(CustomCommandsSubPath, folderName)
	if err := client.CreateFolder(folderPath); err != nil {
		return nil, fmt.Errorf("failed to create command folder: %w", err)
	}

	// Write COMMAND.md
	cmdFilePath := path.Join(folderPath, CommandFileName)
	if err := client.WriteFile(cmdFilePath, content); err != nil {
		return nil, fmt.Errorf("failed to write command file: %w", err)
	}

	return &Command{
		Frontmatter: *frontmatter,
		Content:     body,
		FolderName:  folderName,
		FilePath:    cmdFilePath,
	}, nil
}

// UpdateCommand updates a command's COMMAND.md content
func UpdateCommand(workspaceAPIURL, folderName, content string) (*Command, error) {
	frontmatter, body, err := ValidateCommandContent(content)
	if err != nil {
		return nil, fmt.Errorf("invalid command content: %w", err)
	}

	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	cmdFilePath := path.Join(CustomCommandsSubPath, folderName, CommandFileName)
	if err := client.WriteFile(cmdFilePath, content); err != nil {
		return nil, fmt.Errorf("failed to write command: %w", err)
	}

	return &Command{
		Frontmatter: *frontmatter,
		Content:     body,
		FolderName:  folderName,
		FilePath:    cmdFilePath,
	}, nil
}

// DeleteCommand deletes a command folder
func DeleteCommand(workspaceAPIURL, folderName string) error {
	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	folderPath := path.Join(CustomCommandsSubPath, folderName)
	if err := client.DeleteFolder(folderPath); err != nil {
		return fmt.Errorf("failed to delete command: %w", err)
	}

	return nil
}

// ValidateCommandContent validates the complete COMMAND.md content
func ValidateCommandContent(content string) (*CommandFrontmatter, string, error) {
	frontmatter, body, err := ParseCommandFile(content)
	if err != nil {
		return nil, "", err
	}

	if strings.TrimSpace(frontmatter.Name) == "" {
		return nil, "", fmt.Errorf("name is required")
	}

	if strings.TrimSpace(frontmatter.Description) == "" {
		return nil, "", fmt.Errorf("description is required")
	}

	// Validate name format
	for _, c := range frontmatter.Name {
		if !isValidNameChar(c) {
			return nil, "", fmt.Errorf("name contains invalid character '%c' (only alphanumeric, hyphens, and underscores allowed)", c)
		}
	}

	return frontmatter, body, nil
}

func isValidNameChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_'
}
