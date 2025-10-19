package server

import "mcp-agent/agent_go/pkg/utils"

// AgentInstructions contains custom instructions for both React and Simple agents
type AgentInstructions struct {
	ResponseFormatting string
}

// GetAgentInstructions returns the custom instructions for agents
func GetAgentInstructions() string {
	return `

**File Operations Protocol:**
When working with files, follow this CRITICAL 5-step workflow:
1. **READ FIRST**: 🚨 MANDATORY - Always use read_workspace_file to see exact current content
2. **CHOOSE METHOD**: 
   - **PREFERRED**: Use diff_patch_workspace_file for all file updates (more efficient, smaller payloads, better version control)
   - **ONLY for**: Use update_workspace_file for complete file rewrites or new files
3. **DIFF FORMAT**: If using diff_patch_workspace_file, generate perfect unified diff format like 'diff -U0'
4. **CONTEXT MATCHING**: 🚨 CRITICAL - Context lines (starting with space) must match file content EXACTLY
5. **VERIFY**: Test your approach before applying changes

**Diff Patch Requirements:**
- ✅ Use read_workspace_file first to get exact file content
- ✅ Copy context lines EXACTLY from the file (including spaces/tabs)
- ✅ Ensure diff ends with a newline character
- ✅ Use proper unified diff format with ---/+++ headers
- ✅ Generate diffs like 'diff -U0' would produce
- ✅ Verify line numbers in hunk headers match actual file

**🚨 CRITICAL CONTEXT LINE FORMAT:**
- Context lines MUST start with SPACE ( ), NOT minus (-)!
- Correct: ' # Header' (space + content)
- Wrong:   '- # Header' (minus + content)
- Context lines show unchanged content, removals show deleted content


` + utils.GetCommonFileInstructions() + `

`

}

// GetReactAgentInstructions returns additional instructions specifically for React agents
func GetReactAgentInstructions() string {
	return `
**Memory Management:**
React agents have access to memory management tools for persistent knowledge storage:
- Use ` + "`add_memory`" + ` to store important information, insights, or learnings for future reference
- Use ` + "`search_memory`" + ` to retrieve relevant past information when answering questions
- Use ` + "`delete_memory`" + ` to remove outdated, incorrect, or no longer relevant memories
- Store key findings, user preferences, project details, and important decisions
- Search memory before providing answers to leverage accumulated knowledge
- Clean up outdated information to maintain memory quality and accuracy
- Memory helps maintain context across conversations and builds institutional knowledge
- Always better to first search existing memory, then add new memory, and delete outdated memory when needed

**Workspace Files Management Long Term Memory:**
All agents have access to workspace tools for file and document management:
- Use this as a file storage to save information for long term
- Use this to store large information which is best to store in files 
- Include optional commit messages for version control
- Sync the files to git via tools on a regular basis

**Critical File Operations Best Practices:**
- **🚨 ALWAYS use read_workspace_file first** before making any changes to understand current content
- **PREFERRED**: Use diff_patch_workspace_file for all file updates (more efficient, smaller payloads, better version control)
- **ONLY for**: Use update_workspace_file for complete file rewrites or new files
- **For exploration**: Use get_workspace_file_nested to understand document structure
- **For searching**: Use search_workspace_files to find relevant content across workspace
- **🚨 Context matching is CRITICAL**: When using diff_patch_workspace_file, context lines must match file exactly
- **🚨 Test your diffs**: Ensure unified diff format is perfect before applying patches

**Diff Patch Success Checklist:**
- ✅ File exists and was read with read_workspace_file
- ✅ Context lines copied EXACTLY from file content (including whitespace)
- ✅ Hunk headers show correct line numbers
- ✅ Diff ends with newline character
- ✅ Proper unified diff format (---/+++ headers)
- ✅ No truncated or malformed lines
- ✅ Test with simple single-line addition first

` + utils.GetCommonFileInstructions() + `

`
}
