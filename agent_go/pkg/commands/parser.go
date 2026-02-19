package commands

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseCommandFile parses a COMMAND.md file content into frontmatter and body
func ParseCommandFile(content string) (*CommandFrontmatter, string, error) {
	if !strings.HasPrefix(content, "---") {
		return nil, "", fmt.Errorf("COMMAND.md must start with YAML frontmatter (---)")
	}

	rest := content[3:]
	endIndex := strings.Index(rest, "\n---")
	if endIndex == -1 {
		return nil, "", fmt.Errorf("COMMAND.md frontmatter missing closing delimiter (---)")
	}

	frontmatterYAML := strings.TrimSpace(rest[:endIndex])
	body := strings.TrimSpace(rest[endIndex+4:])

	var frontmatter CommandFrontmatter
	decoder := yaml.NewDecoder(bytes.NewBufferString(frontmatterYAML))
	if err := decoder.Decode(&frontmatter); err != nil {
		return nil, "", fmt.Errorf("failed to parse frontmatter YAML: %w", err)
	}

	return &frontmatter, body, nil
}

// SerializeCommandFile serializes frontmatter and body back to COMMAND.md format
func SerializeCommandFile(frontmatter *CommandFrontmatter, body string) (string, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(frontmatter); err != nil {
		return "", fmt.Errorf("failed to serialize frontmatter: %w", err)
	}
	encoder.Close()

	var result strings.Builder
	result.WriteString("---\n")
	result.WriteString(strings.TrimSpace(buf.String()))
	result.WriteString("\n---\n\n")
	result.WriteString(body)

	return result.String(), nil
}

// ParseCommandFromContent parses a complete Command from COMMAND.md content
func ParseCommandFromContent(content, folderName, filePath string) (*Command, error) {
	frontmatter, body, err := ParseCommandFile(content)
	if err != nil {
		return nil, err
	}

	return &Command{
		Frontmatter: *frontmatter,
		Content:     body,
		FolderName:  folderName,
		FilePath:    filePath,
	}, nil
}
