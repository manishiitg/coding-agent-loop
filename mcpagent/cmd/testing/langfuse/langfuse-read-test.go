package langfuse

import (
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
	readTraceID       string
	readSessionID     string
	readObservationID string
	readLimit         int
)

var langfuseReadTestCmd = &cobra.Command{
	Use:   "langfuse-read",
	Short: "Read traces/observations/sessions from Langfuse (read-only)",
	Long: `Read-only command to retrieve traces, observations, and sessions from Langfuse.

This command uses direct API calls (no binary required) to read data from Langfuse.

Examples:
  # List recent traces
  mcpagent-test langfuse-read --traces --limit 10

  # Get a specific trace
  mcpagent-test langfuse-read --trace-id <trace-id>

  # Get observations for a trace
  mcpagent-test langfuse-read --trace-id <trace-id> --observations

  # List observations
  mcpagent-test langfuse-read --observations --limit 10

  # Get a specific observation
  mcpagent-test langfuse-read --observation-id <observation-id>

  # List sessions
  mcpagent-test langfuse-read --sessions --limit 10`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Langfuse Read Test (Read-Only) ===")

		// Load .env file if it exists (same as langfuse-read binary)
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

		// Initialize Langfuse API client
		logger.Info("Initializing Langfuse API client...")
		apiClient, err := newLangfuseAPIClient()
		if err != nil {
			return fmt.Errorf("failed to initialize Langfuse API client: %w", err)
		}
		logger.Info("✅ Langfuse API client initialized")

		// Determine what to read based on flags
		if readTraceID != "" {
			if readObservationID != "" {
				// Get specific observation
				logger.Info("Retrieving observation...", loggerv2.String("observation_id", readObservationID))
				observations, err := apiClient.getObservations(1, readTraceID, "")
				if err != nil {
					return fmt.Errorf("failed to retrieve observation: %w", err)
				}

				// Find the specific observation
				var foundObs map[string]interface{}
				for _, obs := range observations {
					if obsID, ok := obs["id"].(string); ok && obsID == readObservationID {
						foundObs = obs
						break
					}
				}

				if foundObs == nil {
					return fmt.Errorf("observation %s not found", readObservationID)
				}

				// Print as JSON
				jsonData, err := json.MarshalIndent(foundObs, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal observation: %w", err)
				}
				fmt.Println(string(jsonData))
				logger.Info("✅ Observation retrieved successfully")
				return nil
			}

			// Get specific trace
			logger.Info("Retrieving trace...", loggerv2.String("trace_id", readTraceID))
			traceData, err := apiClient.getTrace(readTraceID)
			if err != nil {
				return fmt.Errorf("failed to retrieve trace: %w", err)
			}

			// Print as JSON
			jsonData, err := json.MarshalIndent(traceData, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal trace: %w", err)
			}
			fmt.Println(string(jsonData))
			logger.Info("✅ Trace retrieved successfully")

			// If observations flag is set, also get observations
			if cmd.Flag("observations").Changed {
				logger.Info("Retrieving observations for trace...")
				observations, err := apiClient.getObservations(readLimit, readTraceID, "")
				if err != nil {
					return fmt.Errorf("failed to retrieve observations: %w", err)
				}

				obsJSON, err := json.MarshalIndent(observations, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal observations: %w", err)
				}
				fmt.Println("\n--- Observations ---")
				fmt.Println(string(obsJSON))
				logger.Info("✅ Observations retrieved successfully", loggerv2.Int("count", len(observations)))
			}

			return nil
		}

		if readObservationID != "" {
			// Get specific observation (without trace ID)
			logger.Info("Retrieving observation...", loggerv2.String("observation_id", readObservationID))
			// Note: Langfuse API requires trace ID to get observation, so we'll search in recent observations
			observations, err := apiClient.getObservations(readLimit, "", "")
			if err != nil {
				return fmt.Errorf("failed to retrieve observations: %w", err)
			}

			var foundObs map[string]interface{}
			for _, obs := range observations {
				if obsID, ok := obs["id"].(string); ok && obsID == readObservationID {
					foundObs = obs
					break
				}
			}

			if foundObs == nil {
				return fmt.Errorf("observation %s not found in recent observations", readObservationID)
			}

			jsonData, err := json.MarshalIndent(foundObs, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal observation: %w", err)
			}
			fmt.Println(string(jsonData))
			logger.Info("✅ Observation retrieved successfully")
			return nil
		}

		// List operations
		if cmd.Flag("traces").Changed {
			logger.Info("Listing recent traces...", loggerv2.Int("limit", readLimit))
			traces, err := apiClient.getTraces(readLimit)
			if err != nil {
				return fmt.Errorf("failed to retrieve traces: %w", err)
			}

			jsonData, err := json.MarshalIndent(traces, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal traces: %w", err)
			}
			fmt.Println(string(jsonData))
			logger.Info("✅ Traces retrieved successfully", loggerv2.Int("count", len(traces)))
			return nil
		}

		if cmd.Flag("observations").Changed {
			logger.Info("Listing observations...", loggerv2.Int("limit", readLimit))
			observations, err := apiClient.getObservations(readLimit, readTraceID, readSessionID)
			if err != nil {
				return fmt.Errorf("failed to retrieve observations: %w", err)
			}

			jsonData, err := json.MarshalIndent(observations, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal observations: %w", err)
			}
			fmt.Println(string(jsonData))
			logger.Info("✅ Observations retrieved successfully", loggerv2.Int("count", len(observations)))
			return nil
		}

		if cmd.Flag("sessions").Changed {
			logger.Info("Listing sessions...", loggerv2.Int("limit", readLimit))
			sessions, err := apiClient.getSessions(readLimit)
			if err != nil {
				return fmt.Errorf("failed to retrieve sessions: %w", err)
			}

			jsonData, err := json.MarshalIndent(sessions, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal sessions: %w", err)
			}
			fmt.Println(string(jsonData))
			logger.Info("✅ Sessions retrieved successfully", loggerv2.Int("count", len(sessions)))
			return nil
		}

		// If no flags specified, show help
		return cmd.Help()
	},
}

func init() {
	langfuseReadTestCmd.Flags().StringVar(&readTraceID, "trace-id", "", "Trace ID to retrieve")
	langfuseReadTestCmd.Flags().StringVar(&readSessionID, "session-id", "", "Session ID to filter by")
	langfuseReadTestCmd.Flags().StringVar(&readObservationID, "observation-id", "", "Observation ID to retrieve")
	langfuseReadTestCmd.Flags().IntVar(&readLimit, "limit", 10, "Limit for list operations")
	langfuseReadTestCmd.Flags().Bool("traces", false, "List recent traces")
	langfuseReadTestCmd.Flags().Bool("observations", false, "List observations")
	langfuseReadTestCmd.Flags().Bool("sessions", false, "List sessions")
}

// GetLangfuseReadTestCmd returns the Langfuse read-only test command
func GetLangfuseReadTestCmd() *cobra.Command {
	return langfuseReadTestCmd
}

// langfuseAPIClient is a client for making direct API calls to Langfuse
type langfuseAPIClient struct {
	client    *http.Client
	host      string
	publicKey string
	secretKey string
}

// newLangfuseAPIClient creates a new Langfuse API client from environment variables
func newLangfuseAPIClient() (*langfuseAPIClient, error) {
	// Get host
	h := os.Getenv("LANGFUSE_BASE_URL")
	if h == "" {
		h = "https://cloud.langfuse.com"
	}

	// Get credentials
	pk := os.Getenv("LANGFUSE_PUBLIC_KEY")
	sk := os.Getenv("LANGFUSE_SECRET_KEY")

	if pk == "" || sk == "" {
		return nil, fmt.Errorf("langfuse credentials missing. Set LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY environment variables")
	}

	return &langfuseAPIClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		host:      h,
		publicKey: pk,
		secretKey: sk,
	}, nil
}

// makeRequest makes an HTTP request to the Langfuse API
func (c *langfuseAPIClient) makeRequest(method, endpoint string) ([]byte, error) {
	reqURL := c.host + endpoint
	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(c.publicKey, c.secretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errorResp map[string]interface{}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			if msg, ok := errorResp["message"].(string); ok {
				return nil, fmt.Errorf("API request failed (status %d): %s", resp.StatusCode, msg)
			}
		}
		errorBody := string(body)
		if len(errorBody) > 200 {
			errorBody = errorBody[:200] + "..."
		}
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, errorBody)
	}

	return body, nil
}

// Trace represents a Langfuse trace
type traceResponse struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Input     interface{}            `json:"input,omitempty"`
	Output    interface{}            `json:"output,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp string                 `json:"timestamp,omitempty"`
	UserID    string                 `json:"userId,omitempty"`
	SessionID string                 `json:"sessionId,omitempty"`
}

// Span represents a Langfuse observation/span
type spanResponse struct {
	ID                  string                 `json:"id"`
	TraceID             string                 `json:"traceId"`
	ParentObservationID string                 `json:"parentObservationId,omitempty"`
	Name                string                 `json:"name"`
	Type                string                 `json:"type"`
	Model               string                 `json:"model,omitempty"`
	Input               interface{}            `json:"input,omitempty"`
	Output              interface{}            `json:"output,omitempty"`
	StartTime           string                 `json:"startTime,omitempty"`
	EndTime             string                 `json:"endTime,omitempty"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
	Usage               map[string]interface{} `json:"usage,omitempty"`
	PromptTokens        *int                   `json:"promptTokens,omitempty"`
	CompletionTokens    *int                   `json:"completionTokens,omitempty"`
	TotalTokens         *int                   `json:"totalTokens,omitempty"`
}

// spansResponse represents the response from get spans endpoint
type spansResponse struct {
	Data []spanResponse `json:"data"`
}

// Session represents a Langfuse session
type sessionResponse struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name,omitempty"`
	UserID    string                 `json:"userId,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt string                 `json:"createdAt,omitempty"`
	UpdatedAt string                 `json:"updatedAt,omitempty"`
}

// sessionsResponse represents the response from get sessions endpoint
type sessionsResponse struct {
	Data []sessionResponse `json:"data"`
}

// getTrace retrieves a specific trace by ID
func (c *langfuseAPIClient) getTrace(id string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/api/public/traces/%s", url.PathEscape(id))
	body, err := c.makeRequest("GET", endpoint)
	if err != nil {
		return nil, err
	}

	var trace traceResponse
	if err := json.Unmarshal(body, &trace); err != nil {
		return nil, fmt.Errorf("failed to parse trace response: %w", err)
	}

	// Convert to map for compatibility
	result := make(map[string]interface{})
	result["id"] = trace.ID
	result["name"] = trace.Name
	result["input"] = trace.Input
	result["output"] = trace.Output
	result["metadata"] = trace.Metadata
	result["timestamp"] = trace.Timestamp
	result["userId"] = trace.UserID
	result["sessionId"] = trace.SessionID

	return result, nil
}

// getObservations retrieves observations from Langfuse with optional filters
func (c *langfuseAPIClient) getObservations(limit int, traceIDFilter, sessionIDFilter string) ([]map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/api/public/observations?limit=%d", limit)

	// Add optional filters
	if traceIDFilter != "" {
		endpoint += fmt.Sprintf("&traceId=%s", url.QueryEscape(traceIDFilter))
	}
	if sessionIDFilter != "" {
		endpoint += fmt.Sprintf("&sessionId=%s", url.QueryEscape(sessionIDFilter))
	}

	body, err := c.makeRequest("GET", endpoint)
	if err != nil {
		return nil, err
	}

	var response spansResponse
	if err := json.Unmarshal(body, &response); err == nil {
		// Convert to []map[string]interface{} for compatibility
		result := make([]map[string]interface{}, len(response.Data))
		for i, span := range response.Data {
			result[i] = map[string]interface{}{
				"id":                  span.ID,
				"traceId":             span.TraceID,
				"parentObservationId": span.ParentObservationID,
				"name":                span.Name,
				"type":                span.Type,
				"model":               span.Model,
				"input":               span.Input,
				"output":              span.Output,
				"startTime":           span.StartTime,
				"endTime":             span.EndTime,
				"metadata":            span.Metadata,
				"usage":               span.Usage,
			}
		}
		return result, nil
	}

	// Try alternative response format (direct array)
	var observations []spanResponse
	if err := json.Unmarshal(body, &observations); err == nil {
		result := make([]map[string]interface{}, len(observations))
		for i, span := range observations {
			result[i] = map[string]interface{}{
				"id":                  span.ID,
				"traceId":             span.TraceID,
				"parentObservationId": span.ParentObservationID,
				"name":                span.Name,
				"type":                span.Type,
				"model":               span.Model,
				"input":               span.Input,
				"output":              span.Output,
				"startTime":           span.StartTime,
				"endTime":             span.EndTime,
				"metadata":            span.Metadata,
				"usage":               span.Usage,
			}
		}
		return result, nil
	}

	return nil, fmt.Errorf("failed to parse observations response: %w", err)
}

// getSessions retrieves sessions from Langfuse
func (c *langfuseAPIClient) getSessions(limit int) ([]map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/api/public/sessions?limit=%d", limit)

	body, err := c.makeRequest("GET", endpoint)
	if err != nil {
		return nil, err
	}

	var response sessionsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse sessions response: %w", err)
	}

	// Convert to []map[string]interface{} for compatibility
	result := make([]map[string]interface{}, len(response.Data))
	for i, session := range response.Data {
		result[i] = map[string]interface{}{
			"id":        session.ID,
			"name":      session.Name,
			"userId":    session.UserID,
			"metadata":  session.Metadata,
			"createdAt": session.CreatedAt,
			"updatedAt": session.UpdatedAt,
		}
	}

	return result, nil
}

// getTraces retrieves recent traces from Langfuse
func (c *langfuseAPIClient) getTraces(limit int) ([]map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/api/public/traces?limit=%d", limit)
	body, err := c.makeRequest("GET", endpoint)
	if err != nil {
		return nil, err
	}

	var response struct {
		Data []traceResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse traces response: %w", err)
	}

	// Convert to []map[string]interface{} for compatibility
	result := make([]map[string]interface{}, len(response.Data))
	for i, trace := range response.Data {
		result[i] = map[string]interface{}{
			"id":        trace.ID,
			"name":      trace.Name,
			"input":     trace.Input,
			"output":    trace.Output,
			"metadata":  trace.Metadata,
			"timestamp": trace.Timestamp,
			"userId":    trace.UserID,
			"sessionId": trace.SessionID,
		}
	}

	return result, nil
}
