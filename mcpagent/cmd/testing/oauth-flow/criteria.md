# OAuth Flow E2E Test - Log Analysis Criteria

This test validates the complete OAuth authentication flow with Notion MCP. **This test doesn't use traditional asserts** - instead, logs are analyzed (manually or by LLM) to verify test success.

## Running the Test

```bash
# Build the test binary
cd /Users/mipl/ai-work/mcpagent
go build -o mcpagent-test ./cmd/testing

# Run the OAuth flow test
./mcpagent-test test oauth-flow

# With debug logging
./mcpagent-test test oauth-flow --log-level debug

# With custom log file
./mcpagent-test test oauth-flow --log-file logs/oauth-test.log
```

## What This Test Does

The test validates the complete OAuth 2.1 authentication flow:

1. **Create Config** - Temporary Notion MCP config with OAuth auto-discovery
2. **OAuth Login** - Interactive browser-based authentication
3. **Verify Token** - Check token file created with correct permissions
4. **Test Connection** - Use cached token to connect to Notion MCP
5. **Test Logout** - Remove token and verify cleanup

## Prerequisites

- ✅ Notion account with workspace access
- ✅ Browser installed and can open automatically
- ✅ Network connection to mcp.notion.com
- ✅ Port 8080 available for OAuth callback

## Log Analysis Checklist

### ✅ Test Initialization

- [ ] "=== OAuth Flow E2E Test ===" appears in logs
- [ ] "This test requires manual browser interaction" appears
- [ ] No panic or crash during startup

**What to look for in logs:**
```
=== OAuth Flow E2E Test ===
This test requires manual browser interaction
```

### ✅ Step 1: Config Creation

- [ ] "--- Step 1: Create Notion MCP Config with OAuth ---" appears
- [ ] "✅ Created OAuth config" appears with paths
- [ ] Token file path shown in logs
- [ ] No errors during config creation

**What to look for in logs:**
```
--- Step 1: Create Notion MCP Config with OAuth ---
✅ Created OAuth config path=/tmp/notion-oauth-test.json token_file=/tmp/notion-token.json
```

### ✅ Step 2: OAuth Login Flow

- [ ] "--- Step 2: Test OAuth Login (Browser Flow) ---" appears
- [ ] "⚠️  Your browser will open - please authenticate with Notion" appears
- [ ] "Auto-discovering OAuth endpoints from Notion..." appears
- [ ] "✅ Discovered OAuth endpoints" with auth_url and token_url appears
- [ ] "🔐 Opening browser for authentication..." appears
- [ ] Browser actually opens to Notion auth page
- [ ] "✅ Successfully authenticated" appears with expiry time
- [ ] No timeout errors

**What to look for in logs:**
```
--- Step 2: Test OAuth Login (Browser Flow) ---
⚠️  Your browser will open - please authenticate with Notion
Auto-discovering OAuth endpoints from Notion...
✅ Discovered OAuth endpoints auth_url=https://auth.notion.com/oauth/authorize token_url=https://auth.notion.com/oauth/token
🔐 Opening browser for authentication...
Please complete the authentication in your browser
✅ Successfully authenticated expires=2026-01-06T21:30:00Z
```

### ✅ Step 3: Token File Verification

- [ ] "--- Step 3: Verify Token File Created ---" appears
- [ ] "Checking token file..." appears with path
- [ ] "✅ Token file permissions correct mode=0600" appears
- [ ] "✅ Token file structure valid" with size and field count appears
- [ ] No JSON parsing errors
- [ ] All required fields present (access_token, token_type, expiry)

**What to look for in logs:**
```
--- Step 3: Verify Token File Created ---
Checking token file... path=/tmp/notion-token.json
✅ Token file permissions correct mode=0600
✅ Token file structure valid size_bytes=1234 field_count=5
```

**Warning to check for:**
```
⚠️  Token file permissions incorrect got=0644 want=0600
```

### ✅ Step 4: Connection with Cached Token

- [ ] "--- Step 4: Test Connection with Cached Token ---" appears
- [ ] "Attempting to connect using cached token..." appears
- [ ] "Connecting to Notion MCP..." appears
- [ ] "✅ Connected successfully using cached token" appears
- [ ] "✅ Listed tools" with tool count appears (or warning if fails)
- [ ] No "OAuth token unavailable" errors

**What to look for in logs:**
```
--- Step 4: Test Connection with Cached Token ---
Attempting to connect using cached token...
Connecting to Notion MCP...
✅ Connected successfully using cached token
✅ Listed tools tool_count=15
```

**Acceptable warning:**
```
⚠️  Failed to list tools error=...
```
(Connection worked, tool listing might fail for other reasons)

### ✅ Step 5: Token Refresh

- [ ] "--- Step 5: Test Token Refresh (Skipped - requires token expiry) ---" appears
- [ ] "ℹ️  To test token refresh manually..." message appears

**What to look for in logs:**
```
--- Step 5: Test Token Refresh (Skipped - requires token expiry) ---
ℹ️  To test token refresh manually, edit token file and set expiry to past time
```

### ✅ Step 6: Logout

- [ ] "--- Step 6: Test OAuth Logout ---" appears
- [ ] "Removing token file..." appears with path
- [ ] "✅ Token removed successfully" appears
- [ ] "✅ Logout complete - token file deleted" appears
- [ ] No errors during cleanup

**What to look for in logs:**
```
--- Step 6: Test OAuth Logout ---
Removing token file... path=/tmp/notion-token.json
✅ Token removed successfully
✅ Logout complete - token file deleted
```

### ✅ Test Completion

- [ ] "✅ OAuth flow test passed!" appears
- [ ] "📋 For detailed verification, see criteria.md..." appears
- [ ] "🧹 Cleaned up temporary files" appears
- [ ] No goroutine leaks or hanging processes

**What to look for in logs:**
```
✅ OAuth flow test passed!

📋 For detailed verification, see criteria.md in cmd/testing/oauth-flow/
🧹 Cleaned up temporary files
```

## Expected Test Outcome

A successful test run should:

1. Create temporary OAuth config
2. Open browser to Notion auth page automatically
3. Wait for user to complete authentication
4. Receive OAuth callback with authorization code
5. Exchange code for access token and refresh token
6. Save token to file with 0600 permissions
7. Successfully connect using cached token
8. List available tools from Notion MCP
9. Clean up token file on logout
10. Complete without panics or unexpected errors

## Troubleshooting

### "failed to reach Notion MCP: connection refused"

**Cause**: Network connectivity issue or Notion service down  
**Solution**: Check internet connection, try accessing https://mcp.notion.com/mcp in browser  
**Impact**: Test fails - cannot proceed without network

### "failed to discover endpoints: no WWW-Authenticate header"

**Cause**: Notion MCP didn't return expected 401 response  
**Solution**: Check Notion API status, verify URL is correct  
**Impact**: Auto-discovery fails - may need manual endpoint configuration

### "OAuth flow failed: timeout waiting for callback"

**Cause**: User didn't complete authentication within timeout (5 minutes)  
**Solution**: Re-run test and complete auth faster, or increase timeout  
**Impact**: Test fails - token not obtained

### "OAuth flow failed: user denied access"

**Cause**: User clicked "Deny" on Notion consent screen  
**Solution**: Re-run test and click "Allow" to grant permissions  
**Impact**: Test fails - expected behavior when user denies

### "bind: address already in use (port 8080)"

**Cause**: Another process using port 8080 for OAuth callback  
**Solution**: Kill process: `lsof -ti:8080 | xargs kill`  
**Impact**: Test fails - callback server can't start

### "Browser didn't open automatically"

**Cause**: System doesn't have default browser configured  
**Solution**: Manually copy URL from logs and open in browser  
**Impact**: Test can still succeed with manual URL opening

### "token file permissions incorrect: got 0644"

**Cause**: OS umask settings or security context  
**Solution**: Review token_store.go file creation code  
**Impact**: Security risk but test can continue (shows warning)

### "connection failed: OAuth token unavailable"

**Cause**: Token file not created or corrupted  
**Solution**: Check Step 3 logs for token file verification errors  
**Impact**: Test fails - indicates OAuth flow didn't complete

## Related Files

- `mcpagent/oauth/manager.go` - OAuth manager implementation
- `mcpagent/oauth/pkce.go` - PKCE challenge generation
- `mcpagent/oauth/token_store.go` - Token persistence
- `mcpagent/oauth/discovery.go` - Endpoint auto-discovery
- `mcpagent/oauth/callback_server.go` - OAuth callback handler
- `mcpagent/mcpclient/client.go` - MCP client with OAuth integration
- `docs/OAUTH_INTEGRATION.md` - OAuth integration documentation
