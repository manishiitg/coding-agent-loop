package workspace

import (
	"context"
	"encoding/json"
	"fmt"
)

// QueryWorkflowDBParams holds a read-only SQL query against a workflow's SQLite DB.
type QueryWorkflowDBParams struct {
	// DBPath is the workspace-relative path to the SQLite file, e.g.
	// "Workflow/<name>/db/db.sqlite".
	DBPath string `json:"db_path"`
	// SQL is the read-only query to run.
	SQL string `json:"sql"`
}

// queryAPIResponse mirrors the workspace /api/query envelope.
type queryAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    struct {
		Columns []string                 `json:"columns"`
		Rows    []map[string]interface{} `json:"rows"`
	} `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// QueryWorkflowDB runs a read-only SQL query against a workflow's db/db.sqlite via
// the workspace POST /api/query endpoint and returns the result rows as objects
// keyed by column name. The connection is opened read-only server-side.
func (c *Client) QueryWorkflowDB(ctx context.Context, params QueryWorkflowDBParams) ([]map[string]interface{}, error) {
	// Read operation against the db file — validate the path against the read guard.
	if err := c.ValidatePath(params.DBPath, false); err != nil {
		return nil, err
	}

	respBody, err := c.request(ctx, "POST", "/api/query", params)
	if err != nil {
		return nil, err
	}

	var apiResp queryAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse query response: %w", err)
	}
	if !apiResp.Success {
		if apiResp.Error != "" {
			return nil, fmt.Errorf("query failed: %s", apiResp.Error)
		}
		return nil, fmt.Errorf("query failed: %s", apiResp.Message)
	}
	return apiResp.Data.Rows, nil
}
