package langsmith

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

var (
	readRunID     string
	readTraceID   string
	readProject   string
	readProjectID string // UUID of the project
	readRunType   string
	readLimit     int
)

var langsmithReadTestCmd = &cobra.Command{
	Use:   "langsmith-read",
	Short: "Read runs from LangSmith (read-only)",
	Long: `Read-only command to retrieve runs from LangSmith.

This command uses direct API calls to read data from LangSmith.

Examples:
  # List recent runs
  mcpagent-test langsmith-read --runs --limit 10

  # Get a specific run
  mcpagent-test langsmith-read --run-id <run-id>

  # Get all runs in a trace
  mcpagent-test langsmith-read --trace-id <trace-id> --limit 20

  # Filter by run type
  mcpagent-test langsmith-read --runs --run-type llm

  # Filter by project
  mcpagent-test langsmith-read --runs --project myproject`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== LangSmith Read Test (Read-Only) ===")

		// Load .env file if it exists
		envPaths := []string{
			".env",
			"../.env",
			"../../.env",
		}
		for _, path := range envPaths {
			if _, err := os.Stat(path); err == nil {
				if err := godotenv.Load(path); err == nil {
					logger.Info("Loaded .env file", loggerv2.String("path", path))
					break
				}
			}
		}

		// Initialize LangSmith API client
		logger.Info("Initializing LangSmith API client...")
		apiClient, err := newLangsmithAPIClient()
		if err != nil {
			return fmt.Errorf("failed to initialize LangSmith API client: %w", err)
		}
		logger.Info("✅ LangSmith API client initialized",
			loggerv2.String("project", apiClient.project))

		// Determine what to read based on flags
		if readRunID != "" {
			// Get specific run
			logger.Info("Retrieving run...", loggerv2.String("run_id", readRunID))
			runData, err := apiClient.getRun(readRunID)
			if err != nil {
				return fmt.Errorf("failed to retrieve run: %w", err)
			}

			// Print as JSON
			jsonData, err := json.MarshalIndent(runData, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal run: %w", err)
			}
			fmt.Println(string(jsonData))
			logger.Info("✅ Run retrieved successfully")
			return nil
		}

		if readTraceID != "" {
			// Get all runs in a trace
			logger.Info("Retrieving runs for trace...", loggerv2.String("trace_id", readTraceID))
			runs, err := apiClient.getRunsByTrace(readTraceID, readLimit)
			if err != nil {
				return fmt.Errorf("failed to retrieve runs: %w", err)
			}

			jsonData, err := json.MarshalIndent(runs, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal runs: %w", err)
			}
			fmt.Println(string(jsonData))
			logger.Info("✅ Runs retrieved successfully", loggerv2.Int("count", len(runs)))
			return nil
		}

		// List operations
		if cmd.Flag("runs").Changed {
			logger.Info("Listing recent runs...", loggerv2.Int("limit", readLimit))
			runs, err := apiClient.getRuns(readLimit, readProject, readProjectID, readRunType)
			if err != nil {
				return fmt.Errorf("failed to retrieve runs: %w", err)
			}

			jsonData, err := json.MarshalIndent(runs, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal runs: %w", err)
			}
			fmt.Println(string(jsonData))
			logger.Info("✅ Runs retrieved successfully", loggerv2.Int("count", len(runs)))
			return nil
		}

		// If no flags specified, show help
		return cmd.Help()
	},
}

func init() {
	langsmithReadTestCmd.Flags().StringVar(&readRunID, "run-id", "", "Run ID to retrieve")
	langsmithReadTestCmd.Flags().StringVar(&readTraceID, "trace-id", "", "Trace ID to filter by (gets all runs in trace)")
	langsmithReadTestCmd.Flags().StringVar(&readProject, "project", "", "Project name to filter by")
	langsmithReadTestCmd.Flags().StringVar(&readProjectID, "project-id", "", "Project UUID to filter by (use if name lookup fails)")
	langsmithReadTestCmd.Flags().StringVar(&readRunType, "run-type", "", "Run type to filter by (llm, chain, tool)")
	langsmithReadTestCmd.Flags().IntVar(&readLimit, "limit", 10, "Limit for list operations")
	langsmithReadTestCmd.Flags().Bool("runs", false, "List recent runs")
}

// GetLangsmithReadTestCmd returns the LangSmith read-only test command
func GetLangsmithReadTestCmd() *cobra.Command {
	return langsmithReadTestCmd
}

// langsmithAPIClient is a client for making direct API calls to LangSmith
type langsmithAPIClient struct {
	client    *http.Client
	host      string
	apiKey    string
	project   string
	sessionID string // UUID for the project (fetched from sessions API)
}

// newLangsmithAPIClient creates a new LangSmith API client from environment variables
func newLangsmithAPIClient() (*langsmithAPIClient, error) {
	// Get host
	h := os.Getenv("LANGSMITH_ENDPOINT")
	if h == "" {
		h = "https://api.smith.langchain.com"
	}

	// Get credentials
	apiKey := os.Getenv("LANGSMITH_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("LangSmith credentials missing. Set LANGSMITH_API_KEY environment variable")
	}

	// Get project
	project := os.Getenv("LANGSMITH_PROJECT")
	if project == "" {
		project = "default"
	}

	c := &langsmithAPIClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		host:    h,
		apiKey:  apiKey,
		project: project,
	}

	// Lookup session ID for the project
	sessionID, err := c.getSessionIDByName(project)
	if err != nil {
		// Not fatal - some operations may work without session ID
		// Just continue without it
	} else {
		c.sessionID = sessionID
	}

	return c, nil
}

// makeRequest makes an HTTP request to the LangSmith API
func (c *langsmithAPIClient) makeRequest(method, endpoint string, body []byte) ([]byte, error) {
	reqURL := c.host + endpoint

	var req *http.Request
	var err error

	if body != nil {
		req, err = http.NewRequest(method, reqURL, bytes.NewBuffer(body))
	} else {
		req, err = http.NewRequest(method, reqURL, nil)
	}
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errorResp map[string]interface{}
		if err := json.Unmarshal(respBody, &errorResp); err == nil {
			if msg, ok := errorResp["detail"].(string); ok {
				return nil, fmt.Errorf("API request failed (status %d): %s", resp.StatusCode, msg)
			}
			if msg, ok := errorResp["message"].(string); ok {
				return nil, fmt.Errorf("API request failed (status %d): %s", resp.StatusCode, msg)
			}
		}
		errorBody := string(respBody)
		if len(errorBody) > 200 {
			errorBody = errorBody[:200] + "..."
		}
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, errorBody)
	}

	return respBody, nil
}

// getSessionIDByName looks up the session (project) ID by name
func (c *langsmithAPIClient) getSessionIDByName(name string) (string, error) {
	// LangSmith sessions are projects - use /sessions endpoint
	body, err := c.makeRequest("GET", "/sessions?name="+url.QueryEscape(name), nil)
	if err != nil {
		return "", err
	}

	// Try parsing as array
	var sessions []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &sessions); err != nil {
		return "", fmt.Errorf("failed to parse sessions response: %w", err)
	}

	// Find matching session
	for _, s := range sessions {
		if s.Name == name {
			return s.ID, nil
		}
	}

	// If not found by exact name, return first session if available
	if len(sessions) > 0 {
		return sessions[0].ID, nil
	}

	return "", fmt.Errorf("session not found: %s", name)
}

// RunResponse represents a LangSmith run
type runResponse struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	RunType      string                 `json:"run_type"`
	StartTime    string                 `json:"start_time,omitempty"`
	EndTime      string                 `json:"end_time,omitempty"`
	Inputs       map[string]interface{} `json:"inputs,omitempty"`
	Outputs      map[string]interface{} `json:"outputs,omitempty"`
	ParentRunID  string                 `json:"parent_run_id,omitempty"`
	TraceID      string                 `json:"trace_id,omitempty"`
	SessionName  string                 `json:"session_name,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Extra        map[string]interface{} `json:"extra,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
	Status       string                 `json:"status,omitempty"`
	TotalTokens  int                    `json:"total_tokens,omitempty"`
	PromptTokens int                    `json:"prompt_tokens,omitempty"`
	CompletionTokens int               `json:"completion_tokens,omitempty"`
}

// getRun retrieves a specific run by ID
func (c *langsmithAPIClient) getRun(id string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/runs/%s", url.PathEscape(id))
	body, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var run runResponse
	if err := json.Unmarshal(body, &run); err != nil {
		return nil, fmt.Errorf("failed to parse run response: %w", err)
	}

	// Convert to map for compatibility
	return convertRunToMap(run), nil
}

// getRuns retrieves runs with optional filters using POST /runs/query
func (c *langsmithAPIClient) getRuns(limit int, project, projectID, runType string) ([]map[string]interface{}, error) {
	// LangSmith uses POST /runs/query for listing runs
	// Requires session (project UUID) for filtering
	queryReq := map[string]interface{}{
		"limit":   limit,
		"is_root": true, // Only get root runs (traces)
	}

	// Use session ID (UUID) for filtering - required by API
	// Priority: explicit project-id flag > session ID from init > lookup
	sessionID := projectID
	if sessionID == "" {
		sessionID = c.sessionID
	}
	if sessionID == "" && project != "" {
		// Try to lookup session ID for the specified project
		if sid, err := c.getSessionIDByName(project); err == nil {
			sessionID = sid
		}
	}

	if sessionID != "" {
		queryReq["session"] = []string{sessionID}
	} else {
		return nil, fmt.Errorf("project UUID not found. Use --project-id to specify the UUID directly (find it in LangSmith UI settings)")
	}

	if runType != "" {
		queryReq["run_type"] = runType
	}

	reqBody, err := json.Marshal(queryReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query request: %w", err)
	}

	body, err := c.makeRequest("POST", "/runs/query", reqBody)
	if err != nil {
		return nil, err
	}

	// Try parsing as object with runs array
	var wrapped struct {
		Runs []runResponse `json:"runs"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Runs) > 0 {
		result := make([]map[string]interface{}, len(wrapped.Runs))
		for i, run := range wrapped.Runs {
			result[i] = convertRunToMap(run)
		}
		return result, nil
	}

	// Try parsing as direct array
	var runs []runResponse
	if err := json.Unmarshal(body, &runs); err != nil {
		return nil, fmt.Errorf("failed to parse runs response: %w", err)
	}

	// Convert to maps
	result := make([]map[string]interface{}, len(runs))
	for i, run := range runs {
		result[i] = convertRunToMap(run)
	}

	return result, nil
}

// getRunsByTrace retrieves all runs in a trace
func (c *langsmithAPIClient) getRunsByTrace(traceID string, limit int) ([]map[string]interface{}, error) {
	// Use POST /runs/query with trace filter
	queryReq := map[string]interface{}{
		"trace":   traceID,
		"limit":   limit,
	}

	reqBody, err := json.Marshal(queryReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query request: %w", err)
	}

	body, err := c.makeRequest("POST", "/runs/query", reqBody)
	if err != nil {
		return nil, err
	}

	// Try parsing as object with runs array
	var wrapped struct {
		Runs []runResponse `json:"runs"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Runs) > 0 {
		result := make([]map[string]interface{}, len(wrapped.Runs))
		for i, run := range wrapped.Runs {
			result[i] = convertRunToMap(run)
		}
		return result, nil
	}

	// Try parsing as direct array
	var runs []runResponse
	if err := json.Unmarshal(body, &runs); err != nil {
		return nil, fmt.Errorf("failed to parse runs response: %w", err)
	}

	// Convert to maps
	result := make([]map[string]interface{}, len(runs))
	for i, run := range runs {
		result[i] = convertRunToMap(run)
	}

	return result, nil
}

// convertRunToMap converts a runResponse to a map
func convertRunToMap(run runResponse) map[string]interface{} {
	result := map[string]interface{}{
		"id":           run.ID,
		"name":         run.Name,
		"run_type":     run.RunType,
		"start_time":   run.StartTime,
		"end_time":     run.EndTime,
		"inputs":       run.Inputs,
		"outputs":      run.Outputs,
		"parent_run_id": run.ParentRunID,
		"trace_id":     run.TraceID,
		"session_name": run.SessionName,
		"error":        run.Error,
		"extra":        run.Extra,
		"tags":         run.Tags,
		"status":       run.Status,
	}

	// Add token info if present
	if run.TotalTokens > 0 {
		result["total_tokens"] = run.TotalTokens
	}
	if run.PromptTokens > 0 {
		result["prompt_tokens"] = run.PromptTokens
	}
	if run.CompletionTokens > 0 {
		result["completion_tokens"] = run.CompletionTokens
	}

	return result
}
