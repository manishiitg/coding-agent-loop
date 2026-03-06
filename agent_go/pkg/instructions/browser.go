package instructions

// GetBrowserUploadInstructions returns system prompt instructions for browser file upload.
// Appended to the agent's system prompt when browser access or Playwright is active.
// Tells the LLM to use workspace-relative paths (e.g. "Downloads/file.pdf") — these are
// automatically resolved to absolute host paths by the toolArgTransformer before reaching
// the Playwright MCP server. For agent_browser (headless/CDP), relative paths work natively
// since the CLI runs inside the Docker container with workspace as its working directory.
func GetBrowserUploadInstructions() string {
	return `

## Browser File Upload

When a website has a file upload input (e.g. file picker, drag-and-drop zone), use these tools to upload workspace files:

### Using agent_browser (Headless/CDP mode)
1. Snapshot to find the file input: agent_browser(command="snapshot", args=["-i"])
2. Upload using the ref: agent_browser(command="upload", args=["@ref", "Downloads/report.pdf"])

### Using browser_file_upload (Playwright mode)
1. First use browser_snapshot to find the file input element
2. Upload: browser_file_upload(paths=["Downloads/report.pdf"], selector="input[type=file]")

### Path Rules
- Always use **workspace-relative paths** (e.g. "Downloads/report.pdf", "Chats/output.csv")
- Paths are automatically resolved to absolute paths — do NOT construct absolute paths yourself
- Files in "Downloads/" are user-uploaded files; files in "Chats/" are created during the conversation
- If you need to create a file first, save it to "Chats/" using execute_shell_command, then upload it
`
}

// GetCamofoxInstructions returns system prompt instructions specific to the Camofox stealth browser.
// Only appended when the camofox MCP server is in the enabled servers list.
func GetCamofoxInstructions() string {
	return `

## Camofox Stealth Browser

You have access to the Camofox stealth browser — an anti-detect Firefox fork that bypasses bot detection.
Use the camofox MCP tools (snapshot, click, type_text, navigate, etc.) to interact with websites.
Always prefer snapshot over screenshot — it returns an accessibility tree which is much more token-efficient.

### File Upload (Camofox)
Camofox does not have a direct file upload tool. To upload a file to a website:
1. Use snapshot(tabId) to find the file input element ref
2. Use camofox_evaluate_js to set the file via JavaScript DataTransfer API or trigger the input programmatically
Note: For headed mode, the user can also manually select a file when the file picker opens.

### Session Persistence

Camofox has built-in session/cookie persistence using named profiles:

**Saving a session (after login):**
1. save_profile(tabId="tab-id", profileId="my-site-login") — saves cookies to a named profile

**Restoring a session (in a new tab/conversation):**
1. create_tab(url="https://example.com") — create a new tab
2. load_profile(tabId="tab-id", profileId="my-site-login") — restore saved cookies
3. refresh(tabId="tab-id") — reload page with restored cookies

**Managing profiles:**
- list_profiles() — see all saved profiles
- delete_profile(profileId="old-profile") — remove a profile

**Importing cookies directly:**
- import_cookies(tabId="tab-id", cookies="[{...}]", userId="user1") — import raw cookie JSON

Always save a profile after successful login so the session can be reused later without re-authentication.

### Downloads

Camofox manages downloads internally (not saved to workspace filesystem):

**Retrieving downloads:**
1. list_downloads(tabId="tab-id") — see all downloaded files
2. get_download(downloadId="id", includeContent=true) — get file content as base64

**Batch downloading resources from a page:**
- batch_download(tabId="tab-id", selector="table.files", types="documents") — extract and download all matching resources

**To save a download to workspace:**
1. Get the download content: get_download(downloadId="id", includeContent=true)
2. Save to workspace using execute_shell_command with base64 decode
`
}
