package subagents

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSubAgentFile parses a SUBAGENT.md file content into frontmatter and body
func ParseSubAgentFile(content string) (*SubAgentFrontmatter, string, error) {
	if !strings.HasPrefix(content, "---") {
		return nil, "", fmt.Errorf("SUBAGENT.md must start with YAML frontmatter (---)")
	}

	rest := content[3:]
	endIndex := strings.Index(rest, "\n---")
	if endIndex == -1 {
		return nil, "", fmt.Errorf("SUBAGENT.md frontmatter missing closing delimiter (---)")
	}

	frontmatterYAML := strings.TrimSpace(rest[:endIndex])
	body := strings.TrimSpace(rest[endIndex+4:])

	var frontmatter SubAgentFrontmatter
	decoder := yaml.NewDecoder(bytes.NewBufferString(frontmatterYAML))
	if err := decoder.Decode(&frontmatter); err != nil {
		return nil, "", fmt.Errorf("failed to parse frontmatter YAML: %w", err)
	}

	return &frontmatter, body, nil
}

// SerializeSubAgentFile serializes frontmatter and body back to SUBAGENT.md format
func SerializeSubAgentFile(frontmatter *SubAgentFrontmatter, body string) (string, error) {
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

// ParseSubAgentFromContent parses a complete SubAgent from SUBAGENT.md content
func ParseSubAgentFromContent(content, folderName, filePath string) (*SubAgent, error) {
	frontmatter, body, err := ParseSubAgentFile(content)
	if err != nil {
		return nil, err
	}

	return &SubAgent{
		Frontmatter: *frontmatter,
		Content:     body,
		FolderName:  folderName,
		FilePath:    filePath,
	}, nil
}
