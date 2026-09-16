package commands

import (
	"fmt"
	"path"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/skills"
)

// DiscoverCommands discovers all user-defined commands in the workspace
func DiscoverCommands(workspaceAPIURL string) ([]Command, error) {
	return DiscoverCommandsAt(workspaceAPIURL, CustomCommandsSubPath)
}

// DiscoverCommandsAt discovers commands below a caller-resolved scope. The
// server resolves that scope from the signed-in user/current workspace; command
// storage itself never accepts a free-form root from the browser or model.
func DiscoverCommandsAt(workspaceAPIURL, commandsPath string) ([]Command, error) {
	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	// List all folders in commands/custom/
	entries, err := client.ListFiles(commandsPath)
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
	return GetCommandAt(workspaceAPIURL, CustomCommandsSubPath, folderName)
}

func GetCommandAt(workspaceAPIURL, commandsPath, folderName string) (*Command, error) {
	if err := validateFolderName(folderName); err != nil {
		return nil, err
	}
	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	cmdFilePath := path.Join(commandsPath, folderName, CommandFileName)
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
	return CreateCommandAt(workspaceAPIURL, CustomCommandsSubPath, folderName, content)
}

func CreateCommandAt(workspaceAPIURL, commandsPath, folderName, content string) (*Command, error) {
	if err := validateFolderName(folderName); err != nil {
		return nil, err
	}
	// Validate content first
	frontmatter, body, err := ValidateCommandContent(content)
	if err != nil {
		return nil, fmt.Errorf("invalid command content: %w", err)
	}

	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	// Create the folder
	folderPath := path.Join(commandsPath, folderName)
	if _, err := GetCommandAt(workspaceAPIURL, commandsPath, folderName); err == nil {
		return nil, fmt.Errorf("command %q already exists", folderName)
	}
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
	return UpdateCommandAt(workspaceAPIURL, CustomCommandsSubPath, folderName, content)
}

func UpdateCommandAt(workspaceAPIURL, commandsPath, folderName, content string) (*Command, error) {
	if err := validateFolderName(folderName); err != nil {
		return nil, err
	}
	frontmatter, body, err := ValidateCommandContent(content)
	if err != nil {
		return nil, fmt.Errorf("invalid command content: %w", err)
	}

	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	cmdFilePath := path.Join(commandsPath, folderName, CommandFileName)
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
	return DeleteCommandAt(workspaceAPIURL, CustomCommandsSubPath, folderName)
}

func DeleteCommandAt(workspaceAPIURL, commandsPath, folderName string) error {
	if err := validateFolderName(folderName); err != nil {
		return err
	}
	client := skills.NewWorkspaceAPIClient(workspaceAPIURL)

	folderPath := path.Join(commandsPath, folderName)
	if err := client.DeleteFolder(folderPath); err != nil {
		return fmt.Errorf("failed to delete command: %w", err)
	}

	return nil
}

func validateFolderName(folderName string) error {
	folderName = strings.TrimSpace(folderName)
	if folderName == "" || folderName == "." || folderName == ".." || path.Base(folderName) != folderName {
		return fmt.Errorf("invalid command folder name")
	}
	for _, c := range folderName {
		if !isValidNameChar(c) {
			return fmt.Errorf("command folder name contains invalid character %q", c)
		}
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
