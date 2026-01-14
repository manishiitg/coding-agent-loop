# Playwright Downloads Going to Wrong Folder in Batch Execution

**Status**: PARTIALLY FIXED - Under Investigation  
**Date**: January 2026  
**Severity**: High  
**Component**: Workflow Orchestrator / MCP Connection Management

## Problem Description

When running multiple workflow groups in batch execution mode, Playwright downloads are inconsistently going to the wrong folder. Downloads should go to each group's specific folder (e.g., `runs/{groupFolder}/execution/Downloads/`), but sometimes they go to the default Downloads folder (`workspace-docs/Downloads/`).

**Observed Behavior:**
- Downloads sometimes go to: `/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/Downloads/` (WRONG)
- Downloads should go to: `/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs/{workspacePath}/runs/{groupFolder}/execution/Downloads/` (CORRECT)
- The issue is **inconsistent** - sometimes correct, sometimes wrong

## Root Cause Analysis

### Primary Issue: Session ID Reuse Across Groups

The root cause is that all workflow groups were sharing the same MCP session ID, causing Playwright connections to be reused across groups with the first group's Downloads path configuration.

**Flow (BEFORE fix):**
1. Group 1 starts → Creates Playwright connection with Downloads path for Group 1
2. Group 2 starts → Reuses same session ID → Reuses Group 1's Playwright connection → Downloads go to Group 1's folder ❌

### Secondary Issue: Connection Reuse Without Override Application

Even with unique session IDs per group, if a Playwright connection already exists in the session registry, it's reused **without** applying new runtime overrides. This means:
- If the first agent in a group creates the connection before `selectedRunFolder` is set correctly
- Or if the connection is created with default config
- All subsequent agents in that group will reuse the wrong connection

**Connection Reuse Logic:**
```go
// From session_registry.go
if existing, ok := sessionConns.clients.Load(serverName); ok {
    logger.Info(fmt.Sprintf("Reusing existing connection for session=%s server=%s", sessionID, serverName))
    return existing.(ClientInterface), false, nil  // ❌ Reused without checking config
}
```

### Timing/Race Condition

The inconsistency suggests a race condition where:
1. `selectedRunFolder` might not be set when the first agent creates the connection
2. Or the override is set but the connection already exists
3. Or multiple agents try to create connections concurrently

## Fixes Applied

### 1. Unique Session ID Per Group ✅

**File**: `controller_batch_execution.go`

- Generate unique session ID for each workflow group: `session-group-{groupID}-{timestamp}`
- Close previous session before starting new group
- Set `selectedRunFolder` via `ApplyExecutionContext` before setting session ID

```go
// Apply execution context (sets selectedRunFolder)
execManager.ApplyExecutionContext(groupSetup)

// Generate unique session ID per group
groupSessionID := fmt.Sprintf("session-group-%s-%d", group.GroupID, time.Now().UnixNano())
hcpo.sessionID = groupSessionID
hcpo.BaseOrchestrator.SetMCPSessionID(groupSessionID)
```

### 2. Downloads Path Override Configuration ✅

**File**: `controller_agent_factory.go`

- Set runtime override for Playwright `--output-dir` based on `selectedRunFolder`
- Override is set on agent config BEFORE agent creation
- Added validation to ensure `selectedRunFolder` is set correctly

```go
if hcpo.selectedRunFolder != "" {
    downloadsRelativePath = filepath.Join("runs", hcpo.selectedRunFolder, "execution", "Downloads")
} else {
    // Log error - selectedRunFolder is empty
}
playwrightOverride.ArgsReplace["--output-dir"] = absDownloadsPath
config.RuntimeOverrides["playwright"] = playwrightOverride
```

### 3. Debug Logging ✅

Added extensive logging to track:
- `selectedRunFolder` value after `ApplyExecutionContext`
- `selectedRunFolder` when setting Downloads path override
- Session ID and Downloads path configuration
- Runtime override details

## Remaining Issues / Investigation Needed

### 1. Inconsistent Behavior

The issue is still **inconsistent**, suggesting:
- Race condition in connection creation
- `selectedRunFolder` might be cleared/reset somewhere
- Connection might be created before override is applied

### 2. Connection Reuse Without Override

The session registry reuses existing connections without checking if the config has changed. This is by design (for performance), but means:
- First agent in group must create connection with correct config
- If first agent creates with wrong config, all subsequent agents share wrong connection

### 3. Potential Solutions (Not Yet Implemented)

**Option A: Fail Hard on Empty selectedRunFolder**
- Return error if `selectedRunFolder` is empty when setting override
- Prevents agent creation with wrong config

**Option B: Close Connection Before First Agent**
- Close any existing Playwright connection for session ID before first agent
- Risk: Might close connection another agent just created (race condition)

**Option C: Config Comparison**
- Check if existing connection's config matches desired config
- Recreate connection if config differs
- More complex but safer

## Files Modified

1. `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_batch_execution.go`
   - Generate unique session ID per group
   - Close previous session before new group
   - Set `selectedRunFolder` before session ID

2. `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go`
   - Set Downloads path runtime override
   - Validate `selectedRunFolder` is set
   - Add debug logging

## Testing

**To Reproduce:**
1. Run workflow with multiple variable groups in batch execution
2. Check where Playwright downloads go for each group
3. Verify downloads go to: `runs/{groupFolder}/execution/Downloads/`

**Expected Behavior:**
- Each group's downloads go to its own folder
- Consistent behavior across all groups

**Current Behavior:**
- Inconsistent - sometimes correct, sometimes wrong

## Debugging Steps

1. Check logs for:
   - `🔍 [DEBUG] After ApplyExecutionContext - selectedRunFolder: '...'`
   - `🔍 [DEBUG] Setting Downloads path - selectedRunFolder: '...'`
   - `❌ [CRITICAL] selectedRunFolder is EMPTY` (if this appears, that's the issue)

2. Check session registry logs:
   - `Creating new connection for session=... server=playwright` (good - fresh connection)
   - `Reusing existing connection for session=... server=playwright` (might be issue if config is wrong)

3. Verify session IDs are unique per group:
   - `🔗 Generated unique MCP session ID for group ...`

## Related Documentation

- [Runtime MCP Overrides](../runtime_mcp_overrides.md) - How runtime overrides work
- [Session-Scoped MCP Connections](../../mcpagent/docs/session_scoped_mcp_connections.md) - Connection sharing mechanism

## Next Steps

1. **Monitor logs** to identify when `selectedRunFolder` is empty or wrong
2. **Consider Option A** (fail hard) if validation shows `selectedRunFolder` is consistently empty
3. **Consider Option C** (config comparison) if connection reuse is the issue
4. **Add connection creation logging** to track exactly when connections are created vs reused
