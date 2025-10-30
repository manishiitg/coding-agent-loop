package memory

import "mcp-agent/agent_go/pkg/utils"

// GetWorkflowMemoryRequirements returns generic memory management requirements for ALL workflow agents
func GetWorkflowMemoryRequirements() string {
	return `
## 📁 WORKSPACE MEMORY MANAGEMENT

### **Workspace Boundaries**
- **Workspace Root**: {{.WorkspacePath}}/ (provided in template variables)
- **STRICT BOUNDARY**: ONLY work within {{.WorkspacePath}}/ - never access other folders
- **Security**: Never touch .env files, root configs, secrets, or system files outside workspace
- **Isolation**: Do not access other workspace folders unless explicitly specified

### **Memory Integration Requirements**
- **Read Operations**: Check existing files in {{.WorkspacePath}}/ for context and previous work
- **Write Operations**: Store results in appropriate subdirectories within {{.WorkspacePath}}/
- **Update Operations**: Modify existing files to track progress and maintain state
- **Archive Operations**: Move completed work to archive subdirectories to maintain clean workspace

### **File Organization Principles**
- **Descriptive Names**: Use clear, descriptive file names that indicate purpose
- **Timestamps**: Include timestamps (YYYY-MM-DD format) for time-based organization
- **Subdirectories**: Organize related files in subdirectories for better structure
- **Consistent Structure**: Maintain consistent naming and organization patterns
- **Documentation**: Include README or index files to explain directory structure

### **🔍 File Discovery & Search**
**Use these tools to efficiently find files:**

- **list_workspace_files**: List files and directories to check existence and structure
  - Use for: checking if files/folders exist, exploring directory structure, finding file names
  - Example: list_workspace_files(folder="{{.WorkspacePath}}") to see what's in the workspace
  
- **regex_search_workspace_files**: Regex/text-based search for exact matches
  - Use for: finding specific text, file names, patterns, exact keywords, complex regex patterns
  - Example: regex_search_workspace_files(query="TODO", folder="{{.WorkspacePath}}")
  
- **semantic_search_workspace_files**: AI-powered semantic search for meaning-based discovery
  - Use for: finding conceptually related content, understanding context
  - Example: semantic_search_workspace_files(query="error handling patterns", folder="{{.WorkspacePath}}")

**Search Strategy:**
1. **Start with listing** to see what files and folders exist in the directory
2. **Use semantic search** to understand what files exist and their content
3. **Use regex search** for specific text patterns, file names, or exact matches
4. **Use regex patterns** for complex pattern matching and multi-line searches
5. **Combine all tools** for comprehensive file discovery
6. **Filter by folder** to search within specific workflow directories

### **Memory Operations Protocol**
1. **Search First**: Use search tools to locate relevant files before reading
2. **Check Existing**: Always check for existing files before creating new ones
3. **Preserve History**: Maintain chronological order and preserve previous work
4. **Update Progress**: Regularly update progress tracking files
5. **Archive Completed**: Move finished work to archive/ to keep workspace clean
6. **Document Changes**: Include timestamps and change descriptions in file updates

### **🚨 CRITICAL FILE OPERATION GUIDELINES**
- **PREFER diff_patch_workspace_file**: Use for targeted, surgical changes to existing files
- **AVOID update_workspace_file**: Only use for creating completely new files or full replacements
- **EFFICIENCY**: diff_patch_workspace_file is more efficient and preserves file history
- **WORKFLOW**: 1) Read file with read_workspace_file 2) Generate unified diff 3) Apply with diff_patch_workspace_file
- **PRECISION**: Diff patching allows precise modifications without overwriting entire files

` + utils.GetCommonFileInstructions() + `

`
}
