package subagents

import (
	"fmt"
	"path"
	"strings"
)

// DiscoverSubAgents discovers all sub-agent templates in the workspace
func DiscoverSubAgents(workspaceAPIURL string) ([]SubAgent, error) {
	client := NewWorkspaceAPIClient(workspaceAPIURL)

	entries, err := client.ListFiles(SubAgentsBasePath)
	if err != nil {
		return []SubAgent{}, nil
	}

	var subagents []SubAgent

	processFolder := func(entry DocumentEntry, prefix string) {
		var folderName string
		if prefix != "" {
			folderName = strings.TrimPrefix(entry.Filepath, SubAgentsBasePath+"/")
		} else {
			folderName = path.Base(entry.Filepath)
		}

		filePath := path.Join(entry.Filepath, SubAgentFileName)
		content, err := client.ReadFile(filePath)
		if err != nil {
			return
		}

		sa, err := ParseSubAgentFromContent(content, folderName, filePath)
		if err != nil {
			return
		}

		subagents = append(subagents, *sa)
	}

	for _, entry := range entries {
		if entry.Type != "folder" {
			continue
		}

		folderName := path.Base(entry.Filepath)

		if folderName == "custom" {
			customEntries, err := client.ListFiles(entry.Filepath)
			if err == nil {
				for _, customEntry := range customEntries {
					if customEntry.Type == "folder" {
						processFolder(customEntry, "custom")
					}
				}
			}
			continue
		}

		processFolder(entry, "")
	}

	return subagents, nil
}

// GetSubAgent retrieves a specific sub-agent template by folder name
func GetSubAgent(workspaceAPIURL, folderName string) (*SubAgent, error) {
	client := NewWorkspaceAPIClient(workspaceAPIURL)

	filePath := path.Join(SubAgentsBasePath, folderName, SubAgentFileName)
	content, err := client.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("sub-agent template not found: %w", err)
	}

	sa, err := ParseSubAgentFromContent(content, folderName, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sub-agent template: %w", err)
	}

	return sa, nil
}

// UpdateSubAgent updates a sub-agent template's SUBAGENT.md content
func UpdateSubAgent(workspaceAPIURL, folderName, content string) (*SubAgent, error) {
	frontmatter, body, err := ParseSubAgentFile(content)
	if err != nil {
		return nil, fmt.Errorf("invalid sub-agent content: %w", err)
	}

	client := NewWorkspaceAPIClient(workspaceAPIURL)

	filePath := path.Join(SubAgentsBasePath, folderName, SubAgentFileName)
	if err := client.WriteFile(filePath, content); err != nil {
		return nil, fmt.Errorf("failed to write sub-agent template: %w", err)
	}

	return &SubAgent{
		Frontmatter: *frontmatter,
		Content:     body,
		FolderName:  folderName,
		FilePath:    filePath,
	}, nil
}

// DeleteSubAgent deletes a sub-agent template folder
func DeleteSubAgent(workspaceAPIURL, folderName string) error {
	client := NewWorkspaceAPIClient(workspaceAPIURL)

	folderPath := path.Join(SubAgentsBasePath, folderName)
	if err := client.DeleteFolder(folderPath); err != nil {
		return fmt.Errorf("failed to delete sub-agent template: %w", err)
	}

	return nil
}
