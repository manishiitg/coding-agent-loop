# Planner REST API - Document & File Management

A comprehensive REST API for managing markdown documents and text-based files with advanced features including structure analysis, search capabilities, version management, file uploads, and GitHub integration. **Now featuring a filepath-based system for transparent document management with complete frontend integration, centralized path handling, and MCP agent integration for AI-powered workspace management.**

## 📋 **API Endpoints Summary**

| Method | Endpoint | Description | Path Handling |
|--------|----------|-------------|---------------|
| `GET` | `/health` | Health check | - |
| `POST` | `/api/documents` | Create document | ✅ Input/Output sanitized |
| `GET` | `/api/documents` | List all documents | ✅ Output sanitized |
| `GET` | `/api/documents/*filepath` | Get document by path | ✅ Input/Output sanitized |
| `PUT` | `/api/documents/*filepath` | Update document | ✅ Input/Output sanitized |
| `DELETE` | `/api/documents/*filepath` | Delete document | ✅ Input sanitized |
| `POST` | `/api/documents/*filepath/move` | Move document to new location | ✅ Input/Output sanitized |
| `PATCH` | `/api/documents/*filepath/patch` | Patch document content | ✅ Input/Output sanitized |
| `GET` | `/api/documents/*filepath/nested` | Get nested content | ✅ Input sanitized |
| `GET` | `/api/versions/*filepath` | Get file version history | ✅ Input sanitized |
| `POST` | `/api/restore/*filepath` | Restore file version | ✅ Input sanitized |
| `POST` | `/api/upload` | Upload files (any non-executable) | ✅ Input/Output sanitized |
| `DELETE` | `/api/folders/*folderpath` | Delete folder | ✅ Input sanitized |
| `GET` | `/api/search` | Search documents | - |
| `POST` | `/api/sync/github` | Sync with GitHub | - |

**Path Handling Legend:**
- ✅ **Input/Output sanitized**: Both request and response paths are sanitized
- ✅ **Input sanitized**: Request paths are sanitized (for URL parameters)
- ✅ **Output sanitized**: Response paths are sanitized
- **-**: No path handling required

## 🎨 **Frontend Integration**

The Planner API is fully integrated with a modern React frontend that provides a complete file management interface:

### **Frontend Features**
- **File Browser**: Hierarchical folder structure with collapsible folders
- **File Upload**: Drag-and-drop file upload with validation and folder-specific upload icons
- **File Management**: Create, read, update, delete files and folders
- **File Revisions**: Complete version history with diff viewing and restoration capabilities
- **Search**: Real-time search across all documents
- **File Highlighting**: Visual highlighting of files during operations
- **Send to Chat**: Add files to chat context for AI processing
- **Git Sync Status**: Real-time Git synchronization status with manual sync capabilities
- **Responsive Design**: Works on desktop and mobile devices

### **Frontend Architecture**
- **React + TypeScript**: Modern frontend framework with type safety
- **Tailwind CSS**: Utility-first CSS framework for styling
- **Axios**: HTTP client for API communication
- **Custom Hooks**: Reusable logic for file operations and state management
- **Component Library**: Reusable UI components with proper accessibility

### **Frontend API Integration**
The frontend communicates with the Planner API through a comprehensive service layer:

```typescript
// File Management API
getPlannerFiles(folder?: string, limit?: number)
getPlannerFileContent(filepath: string)
deletePlannerFile(filepath: string, commitMessage?: string)
deletePlannerFolder(folderPath: string, commitMessage?: string)
uploadPlannerFile(file: File, folderPath: string, commitMessage?: string)

// File Revisions API
getFileVersions(filepath: string, limit?: number)

// Git Sync API
getGitSyncStatus()
syncWithGitHub(commitMessage?: string)
```

## 🤖 **MCP Agent Integration** ✅ **NEW**

The Planner API is now fully integrated with the MCP Agent system, providing AI agents with direct access to workspace files and documents through custom tools.

### **Workspace Tools for AI Agents**

AI agents can now interact with the workspace using the `list_workspace_files` tool:

#### **Available Tools**

**`list_workspace_files`**: Browse files and folders in the workspace
- **Parameters**:
  - `folder` (optional): Filter by specific folder path (e.g., 'docs', 'examples')
  - `max_depth` (optional): Maximum directory depth to traverse (default: -1 for unlimited)
- **Response**: Formatted list of files and folders with metadata

**`read_workspace_file`**: Read the content of a specific file
- **Parameters**:
  - `filepath` (required): Full file path (e.g., 'docs/example.md', 'README.md')
- **Response**: File content with metadata (size, last modified, folder)

**`update_workspace_file`**: Create a new file or replace the entire content of an existing file (upsert behavior)
- **Parameters**:
  - `filepath` (required): Full file path of the file to create or update
  - `content` (required): Content to write to the file (will create new file or replace entire existing file)
  - `commit_message` (optional): Git commit message for version control
- **Response**: Confirmation with file metadata (indicates whether file was created or updated)

**`patch_workspace_file`**: Patch content in an existing file with comprehensive operations
- **Parameters**:
  - `filepath` (required): Full file path of the file to patch
  - `target_selector` (required): Target to operate on (heading text, table index, etc.)
  - `content` (required): Content to add or replace
  - `operation` (optional): Operation to perform: 'append', 'prepend', 'replace', 'insert_after', 'insert_before' (defaults to 'append')
  - `target_type` (optional): Type of target: 'heading', 'table', 'list', 'paragraph', 'code_block' (defaults to 'heading')
  - `commit_message` (optional): Git commit message for version control
- **Response**: Confirmation with updated file metadata

**`get_workspace_file_nested`**: Navigate and explore document content structure
- **Purpose**: Discover headings, access nested content, and find targets for patch operations
- **Parameters**:
  - `filepath` (required): Full file path of the file to analyze
  - `path` (optional): Nested path using arrow notation (e.g., 'Introduction -> Getting Started', 'Phase 1 -> Research'). Returns level 1 headings by default.
- **Response**: Detailed nested content structure and metadata (level 1 headings when no path specified)
- **Usage**: Perfect for patch tool targeting and content exploration

**`search_workspace_files`**: Search files by content, filename, or headings
- **Parameters**:
  - `query` (required): Search query to find in files (supports regex patterns)
  - `search_type` (optional): Type of search - 'content', 'title', 'headings', 'all' (default: 'all')
  - `limit` (optional): Maximum results to return (default: 20, max: 100)
- **Response**: Rich search results with file paths, scores, and content previews
- **Regex Support**: Uses ripgrep for fast regex pattern matching

**`delete_workspace_file`**: Delete a specific file from the workspace permanently
- **Parameters**:
  - `filepath` (required): Full file path of the file to delete (e.g., 'docs/example.md', 'configs/settings.json')
  - `commit_message` (optional): Git commit message for version control
- **Response**: Confirmation with deletion status and warning about permanent removal
- **Safety**: Requires confirmation and includes warning about permanent deletion

**`move_workspace_file`**: Move a file from one location to another in the workspace
- **Parameters**:
  - `source_filepath` (required): Current file path of the file to move (e.g., 'docs/old-file.md', 'configs/settings.json')
  - `destination_filepath` (required): New file path where the file should be moved (e.g., 'archive/old-file.md', 'settings/config.json')
  - `commit_message` (optional): Git commit message for version control
- **Response**: Confirmation with move operation details and new file location
- **Use Cases**: Move files between folders, rename files, reorganize workspace structure

**`sync_workspace_to_github`**: Sync all workspace files to GitHub repository
- **Parameters**:
  - `force` (optional): Force sync even if there are conflicts (default: false)
  - `resolve_conflicts` (optional): Automatically resolve conflicts if possible (default: false)
- **Response**: Sync status with repository information, commit details, and operation results

**`get_workspace_github_status`**: Get current GitHub sync status
- **Parameters**:
  - `show_pending` (optional): Show pending changes (default: true)
  - `show_conflicts` (optional): Show conflicts if any (default: true)
- **Response**: Detailed sync status including connection status, pending changes, file statuses, and conflicts

#### **File Operations for Frontend Integration**

The following tools are considered file operations that trigger frontend updates:
- **`update_workspace_file`** - File creation and updates (upsert behavior)
- **`patch_workspace_file`** - File content modifications
- **`read_workspace_file`** - File content reading
- **`delete_workspace_file`** - File deletion (permanent removal)
- **`move_workspace_file`** - File movement and renaming
- **`get_workspace_file_nested`** - Document structure navigation and exploration

#### **Usage Examples**
```
User: "List files in the workspace"
Agent: Uses list_workspace_files tool to show all files and folders

User: "Show me files in the docs folder"  
Agent: Uses list_workspace_files with folder="docs" parameter

User: "Read the README file"
Agent: Uses read_workspace_file with filepath="README.md"

User: "Create a new documentation file"
Agent: Uses update_workspace_file with filepath="docs/new-guide.md" and content (will create new file)

User: "Update the existing config file"
Agent: Uses update_workspace_file with filepath="configs/settings.json" and new content (will update existing file)

User: "Create or update a file"
Agent: Uses update_workspace_file with filepath="docs/guide.md" and content (will create if new, update if exists)

User: "Add more content to the existing guide"
Agent: Uses get_workspace_file_nested first, then patch_workspace_file with target_selector (operation defaults to "append")

User: "What headings are available in the guide?"
Agent: Uses get_workspace_file_nested with filepath="docs/guide.md"

User: "Show me the content under 'Getting Started' section"
Agent: Uses get_workspace_file_nested with filepath="docs/guide.md" and path="Getting Started"

User: "Explore the 'Phase 1 -> Research' section in the project plan"
Agent: Uses get_workspace_file_nested with filepath="planning/project.md" and path="Phase 1 -> Research"

User: "Analyze the structure of the documentation"
Agent: Uses get_workspace_file_nested with filepath="docs/guide.md"

User: "Search for files containing 'docker'"
Agent: Uses search_workspace_files with query="docker" and search_type="all"

User: "Find files with 'test' in the filename"
Agent: Uses search_workspace_files with query="test" and search_type="title"

User: "Search for files matching pattern 'test.*file'"
Agent: Uses search_workspace_files with query="test.*file" and search_type="content"

User: "Find dates in YYYY-MM-DD format"
Agent: Uses search_workspace_files with query="\\d{4}-\\d{2}-\\d{2}" and search_type="content"

User: "Delete the old config file"
Agent: Uses delete_workspace_file with filepath="configs/old-config.json"

User: "Remove the temporary test file"
Agent: Uses delete_workspace_file with filepath="temp/test.md" and commit_message="Remove temporary test file"

User: "Move the config file to the settings folder"
Agent: Uses move_workspace_file with source_filepath="config.json" and destination_filepath="settings/config.json"

User: "Rename the old document to new-document.md"
Agent: Uses move_workspace_file with source_filepath="docs/old-document.md" and destination_filepath="docs/new-document.md"

User: "What's in the docs/example.md file?"
Agent: Uses read_workspace_file with filepath="docs/example.md"

User: "Sync all changes to GitHub"
Agent: Uses sync_workspace_to_github to commit and push all changes

User: "Check GitHub sync status"
Agent: Uses get_workspace_github_status to see pending changes and conflicts

User: "Force sync to GitHub with conflict resolution"
Agent: Uses sync_workspace_to_github with force=true and resolve_conflicts=true
```

#### **Response Format**

**`list_workspace_files`** returns:
- **📁 Workspace Files** header
- **📂 Folders** section with directory listings
- **📄 Files** section with file listings including timestamps
- **Total file count** and pagination info

**`read_workspace_file`** returns:
- **📄 File Content** header with filepath
- **Metadata**: Folder, last modified date, file size
- **Content**: File content in code block format

**`update_workspace_file`** returns:
- **✅ File created/updated** confirmation with filepath (indicates whether file was created or updated)
- **Metadata**: Folder, last modified date, file size
- **Commit Message**: If provided
- **Operation**: Creation or update confirmation

**`patch_workspace_file`** returns:
- **✅ File patched** confirmation with filepath
- **Metadata**: Folder, last modified date, file size
- **Commit Message**: If provided
- **Operation**: Patch confirmation with operation details

**`get_workspace_file_nested`** returns:
- **🌳 Nested Content Structure** header with filepath
- **Content Structure**: Formatted nested content
- **Metadata**: Additional structural information

**`search_workspace_files`** returns:
- **🔍 Search Results** header with query and search type
- **Search Info**: Method used (ripgrep), total results found
- **Results List**: Numbered list with file details including:
  - **File Path**: Clean path without internal prefixes
  - **Folder**: Directory location
  - **Score**: Relevance score (higher = more relevant)
  - **Line Number**: Where the match was found
  - **Last Modified**: File modification timestamp
  - **Match**: Exact text that matched the query
  - **Preview**: Content preview around the match
- **Tip**: Guidance on using read_workspace_file for full content

**`delete_workspace_file`** returns:
- **🗑️ File Deleted** header with filepath
- **Commit Message**: If provided for version control
- **Status**: Confirmation of permanent deletion
- **Warning**: Clear warning that the action cannot be undone

**`move_workspace_file`** returns:
- **📁 File Moved** header with source → destination paths
- **Commit Message**: If provided for version control
- **Status**: Confirmation of successful move operation
- **Operation**: Success confirmation with new file location

**`sync_workspace_to_github`** returns:
- **🔄 GitHub Sync Completed** header
- **Status**: Sync operation status (synced, etc.)
- **Message**: Success or error message
- **Repository**: GitHub repository name
- **Branch**: Target branch name
- **Force Sync**: Whether force sync was enabled
- **Auto-resolve Conflicts**: Whether conflict resolution was enabled
- **Commit Hash**: Git commit hash of the sync
- **Commit Message**: Commit message used for the sync
- **Operation**: Confirmation of successful sync

**`get_workspace_github_status`** returns:
- **📊 GitHub Sync Status** header
- **Status**: Connection status (🟢 Connected / 🔴 Not connected)
- **Repository**: GitHub repository name
- **Branch**: Current branch name
- **Last Sync**: Timestamp of last successful sync
- **Pending Changes**: Number of uncommitted changes
- **Pending Files**: List of files with pending changes
- **File Statuses**: Detailed Git status for each file (staged/unstaged)
- **Conflicts**: List of any merge conflicts (if show_conflicts=true)
- **Tip**: Guidance on using sync_workspace_to_github to sync changes

#### **Integration Benefits**
- **AI-Powered File Management**: Agents can understand workspace structure
- **Context-Aware Operations**: Agents can reference specific files and folders
- **Seamless Workflow**: No need to manually browse files - agents can discover content
- **Intelligent Filtering**: Agents can filter by folders and limit results as needed

#### **Configuration**
The workspace tools are automatically available to both Simple and ReAct agents. Configuration is handled via environment variables:

```bash
# Planner API URL (default: http://localhost:8081)
export PLANNER_API_URL="http://localhost:8081"
```

#### **Technical Implementation**
- **Custom Tools**: Implemented as custom tools in the MCP agent system
- **HTTP Integration**: Direct API calls to planner endpoints
- **Error Handling**: Graceful fallback when planner API is unavailable
- **Response Parsing**: Automatic parsing of API responses into user-friendly format

### **File Revisions Feature** ✅ **NEW**
The frontend now includes a comprehensive file revisions system with GitHub-like functionality:

#### **Revisions UI Components**
- **Revisions Button**: Located next to file path in file viewer header
- **Revisions Modal**: Two-panel interface for version browsing and details
- **Version List**: Left panel showing all file versions with commit information
- **Version Details**: Right panel displaying selected version details
- **Diff Viewer**: Full-screen modal with syntax-highlighted diff display

#### **Revisions Features**
- **Version History**: Complete commit history with hash, message, author, and date
- **Content Preview**: File content preview for each version
- **Diff Display**: Syntax-highlighted Git diff with color-coded changes:
  - 🟢 **Green**: Added lines (starts with `+`)
  - 🔴 **Red**: Removed lines (starts with `-`)
  - 🔵 **Blue**: Hunk headers (starts with `@@`)
  - 🟡 **Yellow**: File headers (`diff --git`, `index`, `---`, `+++`)
- **Latest Indicator**: Highlights the most recent version
- **Restore Capability**: Placeholder for version restoration (TODO)
- **Responsive Design**: Works on desktop and mobile devices
- **Dark Mode**: Proper styling for both light and dark themes

#### **How to Use File Revisions**
1. **Click on any file** in the workspace to view its contents
2. **Click the "Revisions" button** next to the file path in the header
3. **Browse versions** in the left panel with commit information
4. **Select a version** to see details in the right panel
5. **Click "View Diff"** to see syntax-highlighted changes
6. **Close modals** to return to file viewing

### **File Upload Frontend**
- **Upload Button**: Prominent upload button in the workspace header (defaults to root folder)
- **Folder Upload Icons**: Upload icons on each folder for direct folder uploads
- **File Validation**: Client-side validation for file types and size
- **Simplified Dialog**: Clean upload dialog without manual path input
- **Commit Messages**: Optional commit messages for version control
- **Progress Feedback**: Visual feedback during upload process
- **Error Handling**: Comprehensive error handling and user feedback

### **Frontend File Types**
The frontend supports all text-based file types defined by the API:
- **Documents**: `.txt`, `.md`, `.json`, `.csv`, `.yaml`, `.xml`, `.html`
- **Code**: `.js`, `.ts`, `.py`, `.go`, `.java`, `.cpp`, `.c`, `.php`, `.rb`
- **Config**: `.conf`, `.ini`, `.toml`, `.env`, `.gitignore`, `Dockerfile`
- **And 40+ more text-based file types**

### **Path Handling Architecture**
The system uses a **centralized path handling approach** for maximum security and consistency:

- **Backend Path Management**: All path conversion and validation happens in the Go backend
- **Frontend Clean Paths**: Frontend works exclusively with relative paths (e.g., `"file.md"`, `"folder/file.md"`)
- **API Path Conversion**: Backend automatically converts relative paths to full paths internally
- **Security First**: All path validation and directory traversal protection centralized in backend
- **No UI Path Logic**: Frontend has no path manipulation logic, ensuring clean separation of concerns

## Quick Start

### 🐳 **Docker (Recommended)**

1. **Setup environment:**
```bash
cd planner
cp env.example .env
# Edit .env with your GitHub token and repository
```

2. **Run with Docker:**
```bash
# Start the API
docker-compose up -d

# Or run the test script
./docker-test.sh
```

3. **Access the system:**
- **API**: http://localhost:8081
- **Frontend**: http://localhost:5175 (if running frontend)
- **Health Check**: http://localhost:8081/health
- **MCP Agent Integration**: Available via `list_workspace_files` tool

### 🎨 **Full Stack Setup (API + Frontend)**

1. **Start the Planner API:**
```bash
cd planner
docker-compose up -d
```

2. **Start the Frontend:**
```bash
cd frontend
npm install
npm run dev
```

3. **Access the complete system:**
- **Frontend Interface**: http://localhost:5175
- **API Backend**: http://localhost:8081
- **File Management**: Full UI for uploading, browsing, and managing files

### 🖥️ **Local Development**

1. **Build the application:**
```bash
cd planner
go mod tidy
go build -o planner .
```

2. **Run the server:**
```bash
# Basic usage
./planner server --port 8080 --docs-dir ./workspace-docs

# With GitHub integration
./planner server --port 8080 --docs-dir ./workspace-docs --github-repo city-mall/mcp-agent-docs --github-token ghp_your_token_here

```

### 3. Test the API
```bash
# Health check
curl http://localhost:8081/health

# Create a document
curl -X POST http://localhost:8081/api/documents \
  -H "Content-Type: application/json" \
  -d '{
    "filepath": "examples/my-first-document.md",
    "content": "# Hello World\n\nThis is my first markdown document!",
    "commit_message": "Add first example document"
  }'

# List documents
curl http://localhost:8081/api/documents

# Get a specific document by filepath
curl http://localhost:8081/api/documents/my-first-document.md

# Update a document by filepath
curl -X PUT http://localhost:8081/api/documents/examples/my-first-document.md \
  -H "Content-Type: application/json" \
  -d '{
    "content": "# Hello World\n\nThis is my updated markdown document!",
    "commit_message": "Update first example document"
  }'

# Delete a document by filepath (requires confirmation)
curl -X DELETE "http://localhost:8081/api/documents/examples/my-first-document.md?confirm=true&commit_message=Remove%20first%20example%20document"

# Move a document to a new location
curl -X POST http://localhost:8081/api/documents/examples/my-first-document.md/move \
  -H "Content-Type: application/json" \
  -d '{
    "destination_path": "archive/my-first-document.md",
    "commit_message": "Move document to archive folder"
  }'

# Search documents
curl "http://localhost:8081/api/search?query=hello&search_type=all"



# Patch document content
curl -X PATCH http://localhost:8081/api/documents/examples/my-first-document.md/patch \
  -H "Content-Type: application/json" \
  -d '{
    "target_type": "heading",
    "target_selector": "Hello World",
    "operation": "insert_after",
    "content": "## New Section\n\nThis is a new section!"
  }'

# Get file version history
curl "http://localhost:8081/api/versions/examples/my-first-document.md?limit=5"

# Restore file to a specific version
curl -X POST http://localhost:8081/api/restore/examples/my-first-document.md \
  -H "Content-Type: application/json" \
  -d '{
    "commit_hash": "abc123def456",
    "commit_message": "Restore to previous version"
  }'

# Upload a file with commit message
curl -X POST http://localhost:8081/api/upload \
  -F "file=@/path/to/your/file.pdf" \
  -F "folder_path=uploads" \
  -F "commit_message=Upload new PDF file"

# Delete a folder with commit message
curl -X DELETE "http://localhost:8081/api/folders/examples?confirm=true&commit_message=Remove%20examples%20folder"
```

## Configuration

### Command Line Flags
- `--port`: HTTP server port (default: 8080)
- `--docs-dir`: Documents directory (default: ./workspace-docs)
- `--github-token`: GitHub personal access token
- `--github-repo`: GitHub repository (username/repo-name)
### Environment Variables
```bash
export PLANNER_PORT=8080
export PLANNER_DOCS_DIR=./workspace-docs
export PLANNER_GITHUB_TOKEN=ghp_your_token_here
export PLANNER_GITHUB_REPO=username/workspace-docs
```

**Note**: GitHub branch is always set to "main" for now.

### GitHub Repository Setup

#### **Prerequisites:**
1. **Create GitHub Repository**: The repository must exist on GitHub before first sync
2. **Personal Access Token**: Generate a token with `repo` permissions
3. **Repository Format**: Use `username/repo-name` format

#### **First-Time Setup:**
When you first sync, the API will:
1. **Initialize Git Repo**: Create `.git` directory in docs folder
2. **Set Git Config**: Configure user name and email
3. **Add Remote**: Connect to your GitHub repository
4. **Create README**: Add initial README.md if directory is empty
5. **Initial Commit**: Make first commit with setup files
6. **Push to GitHub**: Push with upstream tracking

#### **Repository Requirements:**
- **Must exist on GitHub** before first sync
- **Must be accessible** with your token
- **Can be empty** - API will create initial content
- **Branch**: Defaults to `main` (configurable)

#### **Example Setup:**
```bash
# 1. Create repo on GitHub (manually)
# 2. Set environment variables
export GITHUB_TOKEN=ghp_your_token_here
export GITHUB_REPO=city-mall/mcp-agent-docs

# 3. Start API and sync
./planner server --github-repo city-mall/mcp-agent-docs --github-token ghp_your_token_here
# Then call: POST /api/sync/github
```

## API Endpoints

### Document Management (Filepath-Based)

#### POST /api/documents
Create a new markdown document
- **Request Body:**
  - `filepath`* - Full file path (e.g., `document.md`, `folder/document.md`)
  - `content`* - Markdown content
  - `commit_message` - Git commit message (optional)

#### GET /api/documents
List all markdown documents
- **Query Parameters:**
  - `folder` - Filter by folder (optional)
  - `max_depth` - Maximum directory depth to traverse (default: -1 for unlimited)

#### GET /api/documents/*filepath
Get document content by filepath
- **Path Parameters:**
  - `filepath`* - Full file path (e.g., `document.md`, `folder/document.md`)

#### PUT /api/documents/*filepath
Create a new file or update entire document content (upsert behavior)
- **Path Parameters:**
  - `filepath`* - Full file path
- **Request Body:**
  - `content`* - Content to write to the file (will create new file or replace entire existing file)
  - `commit_message` - Git commit message (optional)

#### DELETE /api/documents/*filepath
Delete a document permanently
- **Path Parameters:**
  - `filepath`* - Full file path
- **Query Parameters:**
  - `confirm` - Must be 'true' to confirm deletion
  - `commit_message` - Git commit message (optional)

#### POST /api/documents/*filepath/move
Move a document to a new location
- **Path Parameters:**
  - `filepath`* - Source file path
- **Request Body:**
  - `destination_path`* - New file path where the document should be moved
  - `commit_message` - Git commit message (optional)
- **Use Cases**: Move files between folders, rename files, reorganize workspace structure

### Search

#### GET /api/search
Search across all documents
- **Query Parameters:**
  - `query`* - Search query
  - `search_type` - Search type: `content`, `title`, `headings`, `all` (default: `all`)
  - `limit` - Maximum results (default: 50)

### Nested Content Analysis

#### GET /api/documents/*filepath/nested
Get nested content by path (returns level 1 headings by default)
- **Path Parameters:**
  - `filepath`* - Full file path
- **Query Parameters:**
  - `path` - Nested path (e.g., "Section -> Subsection"). Optional - returns level 1 headings by default.

### Version Management

#### GET /api/versions/*filepath
Get version history for a file
- **Path Parameters:**
  - `filepath`* - Full file path
- **Query Parameters:**
  - `limit` - Maximum number of versions to return (default: 10)

#### POST /api/restore/*filepath
Restore a file to a specific version
- **Path Parameters:**
  - `filepath`* - Full file path
- **Request Body:**
  - `commit_hash`* - Git commit hash to restore to
  - `commit_message` - Commit message for the restoration (optional)

### File Upload

#### POST /api/upload
Upload any file to a specified folder
- **Form Data:**
  - `file`* - File to upload (multipart/form-data)
  - `folder_path`* - Target folder path
  - `commit_message` - Commit message for the upload (optional)
- **File Size Limit:** 10MB maximum
- **File Type Restrictions:** Most file types allowed. Executables and system files (exe, dll, so, dylib, bin, msi, dmg, iso, jar, bat, cmd, com, scr) are blocked.
- **Security:** File names are sanitized and paths are validated

### Folder Management

#### DELETE /api/folders/*folderpath
Delete a folder and all its contents
- **Path Parameters:**
  - `folderpath`* - Full folder path
- **Query Parameters:**
  - `confirm`* - Must be "true" to confirm deletion
  - `commit_message` - Commit message for the deletion (optional)

### Advanced Patching

#### PATCH /api/documents/*filepath/patch
Patch document content
- **Path Parameters:**
  - `filepath`* - Full file path
- **Request Body:**
  - `target_type`* - Target type: `heading`, `table`, `list`, `paragraph`, `code_block`
  - `target_selector`* - Selector (heading text, table index, etc.)
  - `operation`* - Operation: `append`, `prepend`, `replace`, `insert_after`, `insert_before`
  - `content`* - New content


## Response Format

All API responses follow this format:
```json
{
  "success": true,
  "message": "Operation completed successfully",
  "data": {
    // Response data here
  },
  "error": "Error message if success is false"
}
```

## File Structure

```
planner/
├── README.md          # This file
├── main.go           # Application entry point
├── root.go           # Cobra root command
├── server.go         # HTTP server setup
├── handlers.go       # API request handlers
└── go.mod           # Go module definition
```

## Search with Ripgrep

The API uses [ripgrep](https://github.com/BurntSushi/ripgrep) for fast, powerful search when available, with automatic fallback to basic search.

### Installing Ripgrep

**macOS (Homebrew):**
```bash
brew install ripgrep
```

**Ubuntu/Debian:**
```bash
sudo apt-get install ripgrep
```

**Windows:**
```bash
# Using Chocolatey
choco install ripgrep

# Using Scoop
scoop install ripgrep
```

### Search Features

- **Fast Search**: Uses ripgrep for lightning-fast text search
- **JSON Output**: Structured results with line numbers and context
- **Multiple Types**: Search in titles, headings, content, or all
- **Relevance Scoring**: Intelligent scoring based on match location and type
- **Fallback Mode**: Automatically falls back to basic search if ripgrep unavailable

### Search Examples

```bash
# Search all content
curl "http://localhost:8081/api/search?query=markdown&search_type=all"

# Search only headings
curl "http://localhost:8081/api/search?query=getting%20started&search_type=headings"

# Search with limit
curl "http://localhost:8081/api/search?query=test&search_type=content&limit=10"
```

## 🐳 Docker Setup

### **File Storage in Docker:**
- **Container Path**: `/app/workspace-docs/`
- **Host Volume**: `workspace-docs` (persistent across container restarts)
- **Development Mount**: `./test-docs` (for local development)

### **Docker Features:**
- **Multi-stage Build**: Optimized image size
- **Ripgrep Included**: Fast search capabilities
- **Git Integration**: GitHub sync support
- **Health Checks**: Automatic container health monitoring
- **Volume Persistence**: Documents survive container restarts

### **Docker Commands:**
```bash
# Build and start
docker-compose up --build

# Run in background
docker-compose up -d

# View logs
docker-compose logs -f planner-api

# Stop services
docker-compose down

# Clean up volumes
docker-compose down -v
```

### **Pushing to Docker Hub**

The workspace-api image is published as `manishiitg/workspace-api` on Docker Hub. Examples under `agent_go/examples/workspace_tools/` use this image.

**Build and push locally** (run from repo root or `workspace/`):
```bash
# From workspace/
./docker-push.sh

# Or with a custom tag
DOCKER_IMAGE=manishiitg/workspace-api:v1.0.0 ./docker-push.sh
```
Ensure you are logged in first: `docker login -u manishiitg`.

**Push via GitHub Actions:** Add repository secrets `DOCKERHUB_USERNAME` (e.g. `manishiitg`) and `DOCKERHUB_TOKEN`, then run the "Docker push workspace-api" workflow manually, or push a tag `workspace-v*` (e.g. `workspace-v1.0.0`) to build and push.

### **Environment Variables:**
```bash
# Required
GITHUB_TOKEN=ghp_your_token_here
GITHUB_REPO=city-mall/mcp-agent-docs

# Optional
PLANNER_DEBUG=false
PLANNER_PORT=8081
```

## 🐛 **Known Issues**

### **Folder Detection Bug** ✅ **RESOLVED**
**Issue**: The `/api/documents` endpoint only returns files, not folders, even though folder detection logic has been implemented.

**Root Cause**: The folder detection logic in `getAllDocumentsRecursively` function was working correctly, but the issue was in the path handling and early return conditions that prevented proper folder processing.

**Solution Applied**:
1. **Enhanced Path Handling**: Improved the relative path calculation and folder detection logic
2. **Fixed Early Return Conditions**: Ensured that directories are properly processed before any early returns
3. **Added Debug Logging**: Temporarily added comprehensive logging to track folder processing
4. **Comprehensive Testing**: Tested with Docker Compose deployment using empty folders

**Test Results**:
- ✅ **Empty folders are now detected**: `Tasks/MembersOfParliament`, `Tasks/test-folder`, `docs/examples`
- ✅ **Folders have correct type field**: `"type": "folder"`
- ✅ **API returns both files and folders**: Complete directory structure is visible
- ✅ **Frontend can display folder structure**: Empty folders are now visible in the UI
- ✅ **Orchestrator mode works**: Tasks folder selection now functions correctly

**Status**: ✅ **RESOLVED** - Fixed and tested successfully

**Files Modified**:
- `planner/handlers/documents.go` - Enhanced `getAllDocumentsRecursively` function
- `planner/models/document.go` - `Document` struct with `Type` field (already working)

### **Max-Depth Parameter Implementation** ✅ **COMPLETED**
**Issue**: User requested to replace the `limit` parameter with a `max_depth` parameter for better control over directory traversal depth.

**Solution Applied**:
1. **Updated Request Model**: Added `MaxDepth` parameter to `ListDocumentsRequest`
2. **Removed Limit Parameter**: Completely removed the `limit` parameter as requested
3. **Updated Recursive Function**: Modified `getAllDocumentsRecursively` to support depth-based traversal
4. **Updated Handler**: Modified `ListDocuments` to use the new `max_depth` parameter

**API Usage Examples**:
```bash
# Get only top-level items
GET /api/documents?max_depth=0

# Get 2 levels deep
GET /api/documents?max_depth=2

# Get from specific folder with depth limit
GET /api/documents?folder=Tasks&max_depth=1

# Unlimited depth (current behavior)
GET /api/documents?max_depth=-1
```

**Benefits Achieved**:
- **Performance**: Stop traversing at specified depth
- **Memory**: Reduce memory usage for large directory trees
- **UI Control**: Frontend can control how deep to load initially
- **Lazy Loading**: Load deeper levels on demand
- **Cleaner API**: Removed confusing `limit` parameter

**Test Results**:
- ✅ **Max Depth 0**: Working - Shows only root level folders
- ✅ **Max Depth 1**: Working - Shows root + 1 level deep
- ✅ **Max Depth 2**: Working - Shows root + 2 levels deep
- ✅ **Unlimited Depth**: Working - Shows all levels

**Status**: ✅ **COMPLETED** - Successfully implemented and tested

**Files Modified**:
- `planner/models/document.go` - Added `MaxDepth` field to `ListDocumentsRequest`
- `planner/handlers/documents.go` - Updated `getAllDocumentsRecursively` and `ListDocuments` functions

### **Hierarchical Structure Building Issue** ✅ **RESOLVED**
**Issue**: The API returns a flat list of documents instead of a hierarchical structure, causing frontend to struggle with building correct folder hierarchies and displaying nested children.

**Root Cause**: The `buildHierarchicalStructure` function had issues with Go's pass-by-value semantics when building nested folder structures. Children were added to folderMap entries but not properly preserved in the final rootItems due to working with copies instead of references.

**Solution Applied**:
1. **Fixed Pointer Management**: Changed `rootItems` to use pointers (`[]*models.Document`) to maintain references to actual structs
2. **Direct Struct Modification**: When adding children to folders, we now modify the actual structs that are referenced in `rootItems`
3. **Proper Reference Handling**: Ensured that `folderMap` and `rootItems` point to the same struct instances
4. **Value Conversion**: Convert pointers back to values only at the final return step

**Test Results**:
- ✅ **Max-Depth Parameter**: Working correctly for depth limiting
- ✅ **Folder Detection**: Folders are properly identified and returned
- ✅ **Nested Children**: Subfolders now get their children populated correctly
- ✅ **Hierarchical Response**: API now returns proper tree structure instead of flat list
- ✅ **Frontend Integration**: Frontend can now display the complete folder hierarchy
- ✅ **Complex Structures**: Tested with multi-level nested folder structures

**Status**: ✅ **RESOLVED** - Fixed and tested successfully

**Files Modified**:
- `planner/handlers/documents.go` - Fixed `buildHierarchicalStructure` function with proper pointer management
- `planner/models/document.go` - `Document` struct with `Children` field (already implemented)

---

## 🔄 **Recent Updates** ✅ **NEW**

### **Complete Path Abstraction Implementation** (January 2025)
- **✅ Output Path Sanitization**: All API responses now return clean relative paths
- **✅ Input Path Sanitization**: Handles both relative and full paths in requests
- **✅ Utility Functions**: Added `GetRelativePath()` and `SanitizeInputPath()` utilities
- **✅ Handler Updates**: Updated all 12+ handler functions for consistent path handling
- **✅ Security Enhancement**: Internal directory structure never exposed to external consumers
- **✅ User Experience**: API consumers can use either path format seamlessly

### **Technical Implementation Details**
- **Files Modified**: `utils/path.go`, `handlers/documents.go`, `handlers/search.go`, `handlers/patch.go`
- **Functions Updated**: All CRUD operations, file uploads, folder operations, version management
- **Testing**: Comprehensive test coverage with 10+ test cases for path sanitization
- **Documentation**: Updated README with complete path abstraction documentation

## ✨ **Key Features**

### **🎨 Complete Frontend Integration**
- **Modern UI**: React + TypeScript frontend with Tailwind CSS styling
- **File Browser**: Hierarchical folder structure with collapsible folders and sorting
- **File Upload**: Drag-and-drop file upload with folder-specific upload icons and simplified dialog
- **File Management**: Complete CRUD operations through intuitive interface
- **File Revisions**: Complete version history with GitHub-like diff viewing and restoration
- **Search Interface**: Real-time search with instant results
- **File Highlighting**: Visual feedback during file operations
- **Send to Chat**: Seamless integration with AI chat systems
- **Git Sync Status**: Real-time Git synchronization status with manual sync capabilities
- **Responsive Design**: Works perfectly on desktop and mobile devices

### **📄 Document Management**
- **CRUD Operations**: Create, read, update, delete documents with filepath-based system
- **Markdown Analysis**: Automatic structure analysis (headings, tables, lists, code blocks)
- **Advanced Patching**: Comprehensive patch operations (append, prepend, replace, insert_after, insert_before) with precise targeting
- **Nested Navigation**: Deep content access by hierarchical path navigation
- **Search Integration**: Fast full-text search with ripgrep integration and fallback

### **📁 File Management**
- **File Upload**: Upload text-based files with folder path and commit message support
- **Folder Upload Icons**: Direct upload to specific folders via folder-specific upload buttons
- **File Type Validation**: Comprehensive validation allowing 50+ text-based file types
- **Binary File Rejection**: Explicitly rejects images, videos, executables, and archives
- **Folder Operations**: Create and delete folders with full content management
- **Path Security**: Directory traversal protection and filename sanitization
- **Frontend Integration**: Complete UI for all file operations
- **Centralized Path Handling**: All path logic managed in backend for security and consistency

### **🔄 Version Control**
- **Version History**: Get file version history with commit details and diffs
- **Version Restoration**: Restore files to specific versions with commit tracking
- **File Revisions UI**: Complete frontend interface for browsing version history
- **Diff Viewer**: Syntax-highlighted diff display with color-coded changes
- **Git Integration**: Optional commit messages and automatic git operations
- **GitHub Sync**: Automatic repository synchronization with GitHub

### **🔒 Security & Concurrency**
- **File Locking**: In-memory mutex-based locking for concurrent operation safety
- **Path Validation**: Comprehensive path validation preventing directory traversal attacks
- **File Type Security**: MIME type and extension validation for upload security
- **Timeout Handling**: 30-second timeout for lock acquisition with automatic cleanup

### **🚀 Performance & Reliability**
- **Docker Ready**: Production-ready containerization with multi-stage builds
- **Fast Search**: Ripgrep integration for high-performance text search
- **Concurrent Safe**: Thread-safe operations with proper locking mechanisms
- **Error Handling**: Comprehensive error responses with detailed error messages

## 🆕 **Enhanced API Features**

### **Filepath-Based System**
The API now uses **filepath-based document management** instead of internal IDs:

- **Transparent Paths**: Use actual file paths (e.g., `document.md`, `folder/document.md`)
- **No Hidden IDs**: No internal ID-to-filename mapping
- **Direct Access**: Access documents using their actual file system paths
- **Folder Support**: Full support for nested folder structures
- **Security**: Path validation prevents directory traversal attacks

### **Complete Path Abstraction** ✅ **NEW**
The API now provides complete abstraction of internal directory structure:

#### **Output Path Abstraction**
- **Never Exposed**: Internal paths like `/app/workspace-docs` are never returned in API responses
- **Consistent Interface**: All API responses use relative paths (e.g., `"docs/example.md"`)
- **Utility Function**: `utils.GetRelativePath()` converts internal paths to relative paths for responses
- **Security**: External consumers never see internal directory structure or server configuration
- **Maintainability**: Internal directory changes don't affect API consumers

#### **Input Path Sanitization**
- **Flexible Input**: Users can pass either relative paths (`"docs/example.md"`) or full paths (`"/app/workspace-docs/docs/example.md"`)
- **Automatic Sanitization**: `utils.SanitizeInputPath()` strips internal directory prefixes from input
- **Consistent Processing**: All handlers sanitize input paths before processing
- **Security**: Prevents users from accidentally exposing internal paths in requests
- **User-Friendly**: API consumers don't need to worry about internal directory structure

#### **Updated Handler Functions**
**Output Sanitization (Response Paths):**
- `CreateDocument` - Returns relative paths instead of full internal paths
- `GetDocument` - Returns relative paths instead of full internal paths
- `UpdateDocument` - Returns relative paths instead of full internal paths
- `FileUpload` - Returns relative paths instead of full internal paths

**Input Sanitization (Request Paths):**
- All URL parameter handlers (`GetDocument`, `UpdateDocument`, `DeleteDocument`, etc.)
- Request body handlers (`CreateDocument`, `UploadFile`, `CreateFolder`)
- Form data handlers (file uploads, folder operations)

### **Centralized Path Handling** ✅ **NEW**
The system now features **centralized path management** for maximum security and consistency:

- **Backend-Only Path Logic**: All path conversion and validation happens in the Go backend
- **Frontend Clean Interface**: Frontend works exclusively with relative paths
- **Automatic Path Conversion**: Backend converts relative paths to full paths internally
- **Security Centralization**: All path validation and directory traversal protection in one place
- **Clean Separation**: No path manipulation logic in the frontend UI layer
- **Consistent API**: All endpoints handle paths uniformly through the backend
- **Internal Path Abstraction**: Internal directory structure (like `/app/workspace-docs`) is never exposed in API responses

### **Enhanced Frontend Integration** ✅ **NEW**
The frontend now features **improved file management** with better user experience:

- **Folder Upload Icons**: Upload icons on each folder for direct folder uploads
- **Simplified Upload Dialog**: Clean upload interface without manual path input
- **File Revisions**: Complete version history interface with diff viewing capabilities
- **Git Sync Status**: Real-time Git synchronization status with manual sync capabilities
- **Default Root Upload**: Main upload button defaults to root folder
- **Visual Git Status**: Color-coded Git status indicators in the workspace header

### **Git Integration & Commit Messages**
Optional commit messages for automatic git operations:

- **Create with Commit**: `"commit_message": "Add new document"`
- **Update with Commit**: `"commit_message": "Update document content"`
- **Delete with Commit**: `?commit_message=Remove%20document`
- **Automatic Git Operations**: Commits and pushes when message provided
- **No Git Required**: Operations work without git repository

### **Concurrent Operation Safety**
In-memory file locking prevents conflicts:

- **File Locking**: Prevents simultaneous modifications of the same file
- **No Lock Files**: Clean file system, no `.lock` file pollution
- **Thread-Safe**: Mutex-based locking for concurrent access
- **Automatic Cleanup**: Stale locks are automatically removed
- **Conflict Prevention**: Returns 409 status when file is locked

### **File Type Validation**
The file upload API includes **comprehensive file type validation**:

- **Text-Based Files Only**: Only allows text-based files (txt, md, json, csv, yaml, xml, html, css, js, py, go, etc.)
- **Binary File Rejection**: Explicitly rejects binary files (images, videos, executables, archives)
- **Extension Validation**: Checks file extensions against allowed list
- **MIME Type Validation**: Validates Content-Type headers for additional security
- **Comprehensive Coverage**: Supports 50+ text-based file types and rejects 30+ binary types

**Allowed File Types:**
- **Documents**: txt, md, markdown, rst, adoc, asciidoc, org, wiki
- **Data**: json, csv, yaml, yml, xml, html, htm
- **Code**: py, go, java, cpp, c, h, hpp, php, rb, js, ts, css, sql, sh, bash, zsh, fish
- **Config**: conf, config, ini, toml, env, gitignore, dockerfile, makefile, cmake
- **Build**: gradle, maven, pom, sbt, scala, kt, swift, rs, dart, r, m, pl, lua
- **Editors**: vim, emacs, tex, latex
- **Graphics**: svg (text-based vector graphics)

**Rejected File Types:**
- **Images**: jpg, jpeg, png, gif, bmp, tiff, webp, ico
- **Videos**: mp4, avi, mov, wmv, flv, webm
- **Audio**: mp3, wav, flac, aac, ogg
- **Documents**: pdf, doc, docx, xls, xlsx, ppt, pptx
- **Archives**: zip, rar, 7z, tar, gz, bz2, xz
- **Executables**: exe, dll, so, dylib, bin, app, deb, rpm, msi, dmg, iso

### **System Architecture**
The Planner system consists of three main components with **centralized path handling**:

1. **Planner API (Backend)**: Go-based REST API with comprehensive file management and centralized path handling
2. **Frontend Interface**: React + TypeScript UI for user interaction with clean relative path interface
3. **File Storage**: Persistent file storage with Git integration

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Frontend UI   │◄──►│   Planner API   │◄──►│  File Storage   │
│   (React/TS)    │    │   (Go/REST)     │    │   (Git/Docker)  │
│   Relative Paths│    │ Path Conversion │    │   Full Paths    │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### **Complete Path Handling Flow** ✅ **UPDATED**
1. **Input**: User sends request with either relative or full path
2. **Input Sanitization**: `SanitizeInputPath()` strips internal directory prefixes from input
3. **Validation**: Standard path validation on sanitized relative path
4. **Processing**: Internal full path construction and file operations
5. **Output Sanitization**: `GetRelativePath()` converts internal paths to relative paths for response
6. **Response**: Returns clean relative paths to frontend/API consumers

### **Path Abstraction Examples**

**Input Handling:**
```bash
# These inputs all result in the same processing:
POST /api/documents
{"filepath": "docs/example.md"}                    # ✅ Relative path
{"filepath": "/app/workspace-docs/docs/example.md"}  # ✅ Full path (sanitized)
```

**Output Handling:**
```json
// All responses return clean relative paths:
{
  "filepath": "docs/example.md",  // Never "/app/workspace-docs/docs/example.md"
  "content": "...",
  "last_modified": "2025-01-27T10:30:00Z"
}
```

### **Internal Path Abstraction** ✅ **NEW**
The API ensures complete abstraction of internal directory structure:

- **Never Exposed**: Internal paths like `/app/workspace-docs` are never returned in API responses
- **Consistent Interface**: All API responses use relative paths (e.g., `"docs/example.md"`)
- **Utility Function**: `utils.GetRelativePath()` converts internal paths to relative paths for responses
- **Security**: External consumers never see internal directory structure or server configuration
- **Maintainability**: Internal directory changes don't affect API consumers

### **Input Path Sanitization** ✅ **NEW**
The API automatically sanitizes input paths to handle both relative and full paths:

- **Flexible Input**: Users can pass either relative paths (`"docs/example.md"`) or full paths (`"/app/workspace-docs/docs/example.md"`)
- **Automatic Sanitization**: `utils.SanitizeInputPath()` strips internal directory prefixes from input
- **Consistent Processing**: All handlers sanitize input paths before processing
- **Security**: Prevents users from accidentally exposing internal paths in requests
- **User-Friendly**: API consumers don't need to worry about internal directory structure

### **Benefits:**
- **Complete Solution**: Full-stack file management with modern UI
- **Simplicity**: Direct file path usage throughout
- **Flexibility**: Support any folder structure
- **Transparency**: File path is the single source of truth
- **Consistency**: Same path used in API and file system
- **Version Control**: Optional git integration for change tracking
- **Concurrency**: Safe for multiple simultaneous operations
- **User Experience**: Intuitive interface for all file operations
- **Developer Experience**: Clean API with comprehensive TypeScript types
- **Security**: Centralized path validation and directory traversal protection
- **Maintainability**: Clean separation of concerns between frontend and backend
- **Scalability**: Backend handles all complex path logic, frontend stays simple
- **Path Abstraction**: Complete internal directory structure abstraction
- **Input Flexibility**: Accepts both relative and full paths seamlessly
- **Output Consistency**: Always returns clean relative paths
