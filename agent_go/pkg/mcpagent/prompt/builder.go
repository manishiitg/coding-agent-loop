package prompt

import (
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/utils"

	"github.com/mark3labs/mcp-go/mcp"
)

// BuildSystemPromptWithoutTools builds the system prompt without including tool descriptions
// This is useful when tools are passed via llmtypes.WithTools() to avoid prompt length issues
func BuildSystemPromptWithoutTools(prompts map[string][]mcp.Prompt, resources map[string][]mcp.Resource, mode interface{}, discoverResource bool, discoverPrompt bool, logger utils.ExtendedLogger) string {
	// Build prompts section with previews (only if discoverPrompt is true)
	var promptsSection string
	if discoverPrompt {
		promptsSection = buildPromptsSectionWithPreviews(prompts, logger)
	} else {
		promptsSection = "" // Empty prompts section when discovery is disabled
	}

	// Build resources section (only if discoverResource is true)
	var resourcesSection string
	if discoverResource {
		resourcesSection = buildResourcesSection(resources)
	} else {
		resourcesSection = "" // Empty resources section when discovery is disabled
	}

	// Build virtual tools section
	virtualToolsSection := buildVirtualToolsSection()

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Choose template based on mode
	var prompt string
	modeStr := fmt.Sprintf("%v", mode)
	if modeStr == "ReAct" {
		prompt = ReActSystemPromptTemplate
	} else {
		prompt = SystemPromptTemplate
	}

	// Replace placeholders (tools are passed via llmtypes.WithTools())
	// prompt = strings.ReplaceAll(prompt, "{{TOOLS_SECTION}}", "Tools are available via llmtypes.WithTools() - see available tools in the tools array")
	prompt = strings.ReplaceAll(prompt, PromptsSectionPlaceholder, promptsSection)
	prompt = strings.ReplaceAll(prompt, ResourcesSectionPlaceholder, resourcesSection)
	prompt = strings.ReplaceAll(prompt, VirtualToolsSectionPlaceholder, virtualToolsSection)
	prompt = strings.ReplaceAll(prompt, CurrentDatePlaceholder, currentDate)
	prompt = strings.ReplaceAll(prompt, CurrentTimePlaceholder, currentTime)

	return prompt
}

// IsReActCompletion checks if the response contains ReAct completion patterns
func IsReActCompletion(response string) bool {
	responseLower := strings.ToLower(response)
	for _, pattern := range ReActCompletionPatterns {
		if strings.Contains(responseLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// ExtractFinalAnswer extracts the final answer from a ReAct response
func ExtractFinalAnswer(response string) string {
	responseLower := strings.ToLower(response)

	// Look for completion patterns
	for _, pattern := range ReActCompletionPatterns {
		patternLower := strings.ToLower(pattern)
		if strings.Contains(responseLower, patternLower) {
			// Find the position of the pattern
			pos := strings.Index(strings.ToLower(response), patternLower)
			if pos != -1 {
				// Extract everything after the pattern
				finalAnswer := response[pos+len(pattern):]
				return strings.TrimSpace(finalAnswer)
			}
		}
	}

	// If no pattern found, return the original response
	return response
}

// buildPromptsSectionWithPreviews builds the prompts section with previews
func buildPromptsSectionWithPreviews(prompts map[string][]mcp.Prompt, logger utils.ExtendedLogger) string {
	// Count total prompts across all servers
	totalPrompts := 0
	for _, serverPrompts := range prompts {
		totalPrompts += len(serverPrompts)
	}

	if totalPrompts == 0 {
		logger.Info("🔍 No prompts found for preview generation - skipping prompts section")
		return ""
	}

	logger.Info("🔍 Building prompts section with previews", map[string]interface{}{
		"server_count":  len(prompts),
		"total_prompts": totalPrompts,
	})

	var promptsList []string
	for serverName, serverPrompts := range prompts {
		if len(serverPrompts) == 0 {
			// Skip servers with no prompts
			continue
		}

		logger.Info("📝 Processing server prompts", map[string]interface{}{
			"server_name":  serverName,
			"prompt_count": len(serverPrompts),
		})

		promptsList = append(promptsList, fmt.Sprintf("%s:", serverName))
		for _, prompt_item := range serverPrompts {
			name := prompt_item.Name
			description := prompt_item.Description

			logger.Debug("📄 Processing prompt", map[string]interface{}{
				"server_name":        serverName,
				"prompt_name":        name,
				"description_length": len(description),
			})

			// Extract preview (first 10 lines) from the description
			preview := extractPromptPreview(description)

			// Format as preview with name and first few lines
			promptsList = append(promptsList, fmt.Sprintf("  - %s: %s", name, preview))
		}
	}

	// Double-check: if no prompts were actually added, return empty
	if len(promptsList) == 0 {
		logger.Info("🔍 No actual prompts found after processing - skipping prompts section")
		return ""
	}

	promptsText := strings.Join(promptsList, "\n")
	logger.Info("✅ Prompts section built", map[string]interface{}{
		"total_length": len(promptsText),
		"prompt_lines": len(promptsList),
	})

	return strings.ReplaceAll(PromptsSectionTemplate, PromptsListPlaceholder, promptsText)
}

// extractPromptPreview extracts the first 10 lines from prompt content
func extractPromptPreview(description string) string {
	// If description contains "Content:", extract the content part (legacy format)
	if strings.Contains(description, "\n\nContent:\n") {
		parts := strings.Split(description, "\n\nContent:\n")
		if len(parts) > 1 {
			content := parts[1]

			// Split into lines and take first 10 lines
			lines := strings.Split(content, "\n")
			previewLines := lines
			if len(lines) > 10 {
				previewLines = lines[:10]
			}

			preview := strings.Join(previewLines, "\n")
			if len(lines) > 10 {
				preview += "\n... (use 'get_prompt' tool for full content)"
			}

			return preview
		}
	}

	// If description contains full content (new format), extract preview
	if len(description) > 100 && !strings.Contains(description, "Prompt loaded from") {
		// Split into lines and take first 10 lines
		lines := strings.Split(description, "\n")
		previewLines := lines
		if len(lines) > 10 {
			previewLines = lines[:10]
		}

		preview := strings.Join(previewLines, "\n")
		if len(lines) > 10 {
			preview += "\n... (use 'get_prompt' tool for full content)"
		}

		return preview
	}

	// If no content section or short description, return the description as is
	return description
}

// buildResourcesSection builds the resources section
func buildResourcesSection(resources map[string][]mcp.Resource) string {
	if len(resources) == 0 {
		return ""
	}

	var resourcesList []string
	for serverName, serverResources := range resources {
		resourcesList = append(resourcesList, fmt.Sprintf("%s:", serverName))
		for _, resource := range serverResources {
			name := resource.Name
			uri := resource.URI
			description := resource.Description
			resourcesList = append(resourcesList, fmt.Sprintf("  - %s (%s): %s", name, uri, description))
		}
	}

	resourcesText := strings.Join(resourcesList, "\n")
	return strings.ReplaceAll(ResourcesSectionTemplate, ResourcesListPlaceholder, resourcesText)
}

// buildVirtualToolsSection builds the virtual tools section
func buildVirtualToolsSection() string {
	return VirtualToolsSectionTemplate
}
