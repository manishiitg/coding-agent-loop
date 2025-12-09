package server

import "mcp-agent-builder-go/agent_go/pkg/utils"

// AgentInstructions contains custom instructions for both React and Simple agents
type AgentInstructions struct {
	ResponseFormatting string
}

// GetAgentInstructions returns the custom instructions for agents
func GetAgentInstructions() string {
	// 	return `

	// **File Operations Protocol:**
	// When working with files, follow this CRITICAL 5-step workflow:
	// 1. **READ FIRST**: 🚨 MANDATORY - Always use read_workspace_file to see exact current content
	// 2. **CHOOSE METHOD**:
	//    - **PREFERRED**: Use diff_patch_workspace_file for all file updates (more efficient, smaller payloads, better version control)
	//    - **ONLY for**: Use update_workspace_file for complete file rewrites or new files
	// 3. **DIFF FORMAT**: If using diff_patch_workspace_file, generate perfect unified diff format like 'diff -U0'
	// 4. **CONTEXT MATCHING**: 🚨 CRITICAL - Context lines (starting with space) must match file content EXACTLY
	// 5. **VERIFY**: Test your approach before applying changes

	// **Diff Patch Requirements:**
	// - ✅ Use read_workspace_file first to get exact file content
	// - ✅ Copy context lines EXACTLY from the file (including spaces/tabs)
	// - ✅ Ensure diff ends with a newline character
	// - ✅ Use proper unified diff format with ---/+++ headers
	// - ✅ Generate diffs like 'diff -U0' would produce
	// - ✅ Verify line numbers in hunk headers match actual file

	// **🚨 CRITICAL CONTEXT LINE FORMAT:**
	// - Context lines MUST start with SPACE ( ), NOT minus (-)!
	// - Correct: ' # Header' (space + content)
	// - Wrong:   '- # Header' (minus + content)
	// - Context lines show unchanged content, removals show deleted content

	// ` + utils.GetCommonFileInstructions() + `

	// `
	return utils.GetCommonFileInstructions()

}
