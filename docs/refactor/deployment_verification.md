# Deployment Verification Guide

This guide explains how to verify that your latest code changes (especially in dependency modules) are active in the deployed environment.

## 1. Add Version Constant
In your dependency module (e.g., `multi-llm-provider-go`), create or update `llmtypes/version.go`:

```go
package llmtypes

// VERSION tracks the build version of the library
const VERSION = "v20260207-FIX-AZURE-STREAMING"
```

## 2. Expose via Health API
In the main application (`agent_go/cmd/server/server.go`), update the health handler:

```go
import (
    "github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ...

func (api *StreamingAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
    // ...
    response := map[string]interface{}{
        "status":  "healthy",
        "time":    time.Now(),
        "version": llmtypes.VERSION, // <--- Add this
        "config":  config,
    }
    // ...
}
```

## 3. Redeploy
Run the deployment script (forcing a rebuild):
```bash
cd deploy/azure
./deploy.sh agent --local
```

## 4. Verify
Call the health endpoint:
```bash
curl https://<your-container-app-url>/api/health
```

**Expected Output:**
```json
{
  "status": "healthy",
  "time": "2026-02-07T18:00:00Z",
  "version": "v20260207-FIX-AZURE-STREAMING",
  "config": { ... }
}
```

If the `version` field is missing or old, your deployment did not pick up the changes.
