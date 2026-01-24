package skills

import (
	"fmt"
	"strings"
)

// ValidationError represents a validation error with details
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateFrontmatter validates the skill frontmatter has required fields
func ValidateFrontmatter(fm *SkillFrontmatter) error {
	if fm == nil {
		return &ValidationError{Field: "frontmatter", Message: "frontmatter is nil"}
	}

	// Required fields
	if strings.TrimSpace(fm.Name) == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}

	if strings.TrimSpace(fm.Description) == "" {
		return &ValidationError{Field: "description", Message: "description is required"}
	}

	// Validate name format (alphanumeric, hyphens, underscores)
	for _, c := range fm.Name {
		if !isValidNameChar(c) {
			return &ValidationError{
				Field:   "name",
				Message: fmt.Sprintf("name contains invalid character '%c' (only alphanumeric, hyphens, and underscores allowed)", c),
			}
		}
	}

	// Validate allowed-tools if present
	for i, tool := range fm.AllowedTools {
		if strings.TrimSpace(tool) == "" {
			return &ValidationError{
				Field:   "allowed-tools",
				Message: fmt.Sprintf("allowed-tools[%d] is empty", i),
			}
		}
	}

	return nil
}

// ValidateSkillContent validates the complete SKILL.md content
func ValidateSkillContent(content string) (*SkillFrontmatter, string, error) {
	// Parse the content
	frontmatter, body, err := ParseSkillFile(content)
	if err != nil {
		return nil, "", err
	}

	// Validate frontmatter
	if err := ValidateFrontmatter(frontmatter); err != nil {
		return nil, "", err
	}

	return frontmatter, body, nil
}

// ValidateSkill validates a complete Skill struct
func ValidateSkill(skill *Skill) error {
	if skill == nil {
		return fmt.Errorf("skill is nil")
	}

	if err := ValidateFrontmatter(&skill.Frontmatter); err != nil {
		return err
	}

	if strings.TrimSpace(skill.FolderName) == "" {
		return &ValidationError{Field: "folder_name", Message: "folder_name is required"}
	}

	return nil
}

// isValidNameChar checks if a character is valid for skill names
func isValidNameChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_'
}
