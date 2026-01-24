package skills

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseSkillFile parses a SKILL.md file content into frontmatter and body
func ParseSkillFile(content string) (*SkillFrontmatter, string, error) {
	// Check for YAML frontmatter delimiters
	if !strings.HasPrefix(content, "---") {
		return nil, "", fmt.Errorf("SKILL.md must start with YAML frontmatter (---)")
	}

	// Find the closing delimiter
	rest := content[3:] // Skip opening ---
	endIndex := strings.Index(rest, "\n---")
	if endIndex == -1 {
		return nil, "", fmt.Errorf("SKILL.md frontmatter missing closing delimiter (---)")
	}

	// Extract frontmatter YAML and body
	frontmatterYAML := strings.TrimSpace(rest[:endIndex])
	body := strings.TrimSpace(rest[endIndex+4:]) // Skip \n---

	// Parse YAML frontmatter
	var frontmatter SkillFrontmatter
	decoder := yaml.NewDecoder(bytes.NewBufferString(frontmatterYAML))
	if err := decoder.Decode(&frontmatter); err != nil {
		return nil, "", fmt.Errorf("failed to parse frontmatter YAML: %w", err)
	}

	return &frontmatter, body, nil
}

// SerializeSkillFile serializes frontmatter and body back to SKILL.md format
func SerializeSkillFile(frontmatter *SkillFrontmatter, body string) (string, error) {
	// Marshal frontmatter to YAML
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(frontmatter); err != nil {
		return "", fmt.Errorf("failed to serialize frontmatter: %w", err)
	}
	encoder.Close()

	// Build the full file content
	var result strings.Builder
	result.WriteString("---\n")
	result.WriteString(strings.TrimSpace(buf.String()))
	result.WriteString("\n---\n\n")
	result.WriteString(body)

	return result.String(), nil
}

// ParseSkillFromContent parses a complete Skill from SKILL.md content
func ParseSkillFromContent(content, folderName, filePath string) (*Skill, error) {
	frontmatter, body, err := ParseSkillFile(content)
	if err != nil {
		return nil, err
	}

	return &Skill{
		Frontmatter: *frontmatter,
		Content:     body,
		FolderName:  folderName,
		FilePath:    filePath,
	}, nil
}
